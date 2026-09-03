#!/bin/sh
# =============================================================================
# NetPulse Agent — one-liner installer (OpenWrt, vía SSH desde esta máquina)
#
#   Instala el agente nativo netpulse-agent en un router/AP OpenWrt: copia el
#   binario (arm64/armv7/mipsle/mips), escribe /etc/netpulse-agent.env (chmod
#   600) e instala el servicio procd (respawn) habilitado y arrancado.
#
# Uso (el token lo genera POST /api/agents y se muestra UNA sola vez):
#   sh install-agent.sh --host 192.168.8.3 \
#       --server http://192.168.8.1:3000 --slug patio --token <64-hex>
#   sh install-agent.sh --host 192.168.8.3 --server ... --slug ... --token ... \
#       --binary ./netpulse-agent          # binario local (sin descarga)
#   sh install-agent.sh --host 192.168.8.3 --uninstall
#
# Opciones:
#   --host X        IP/host del router (obligatorio)
#   --server X      URL del servidor NetPulse (obligatoria salvo --uninstall)
#   --slug X        slug del equipo en NetPulse (obligatorio salvo --uninstall)
#   --token X       token del equipo (obligatorio salvo --uninstall y --pairing-token)
#   --pairing-token X  token de pairing (bootstrap: el agente contacta al servidor
#                   y obtiene el token real automáticamente; Fase 9 R3)
#   --server-fp X   SHA-256 SPKI del servidor en hex (obligatorio si --server es https://)
#   --ssh-user X    usuario SSH del router (default: root)
#   --binary X      binario local a copiar (default: descarga de la release)
#   --version X.Y.Z versión a descargar (default: latest)
#   --tmp           instala el binario en /tmp (RAM) en vez de /usr/sbin:
#                   para equipos con flash justa. OJO: /tmp se pierde al
#                   reiniciar — tras un reboot hay que reejecutar este script
#                   (variante documentada del SPEC; el .ipk llega en el incr. 2)
#   --uninstall     detiene y elimina servicio, binario y config del router
#   --update-netgrip  si se detecta NetGrip en el router: actualizar el panel
#                   NetGrip a su última release (apk/ipk de GitHub) en vez de
#                   limitarse a entregarle la config (--force permite bajar de
#                   versión)
#
# Requisitos: ssh/scp al router (OpenWrt con dropbear), curl o wget local.
# =============================================================================
set -eu

GH_REPO="gnacho/netpulse"
BIN_NAME="netpulse-agent"
ENV_FILE="/etc/netpulse-agent.env"
INIT_NAME="netpulse-agent"
INIT_DST="/etc/init.d/$INIT_NAME"

HOST=""; SERVER=""; SLUG=""; TOKEN=""; PAIRING_TOKEN=""; SERVER_FP=""; SSH_USER="root"; BINARY=""; NETPULSE_VERSION=""; USE_TMP=0; UNINSTALL=0; UPDATE_NETGRIP=0; NETGRIP_FORCE=0

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    C_G=$(printf '\033[32m'); C_R=$(printf '\033[31m'); C_Y=$(printf '\033[33m'); C_B=$(printf '\033[1m'); C_0=$(printf '\033[0m')
else C_G=""; C_R=""; C_Y=""; C_B=""; C_0=""; fi
info()  { printf '%s--%s %s\n' "$C_B" "$C_0" "$*"; }
ok()    { printf '%s✓%s %s\n' "$C_G" "$C_0" "$*"; }
warn()  { printf '%s!%s %s\n' "$C_Y" "$C_0" "$*" >&2; }
fatal() { _c=$1; shift; printf '%s✗ %s%s\n' "$C_R" "$*" "$C_0" >&2; exit "$_c"; }

usage() { sed -n '2,33p' "$0"; exit 0; }

for arg in "$@"; do
    case "$arg" in
        --host=*)     HOST="${arg#*=}" ;;
        --server=*)   SERVER="${arg#*=}" ;;
        --slug=*)     SLUG="${arg#*=}" ;;
        --token=*)    TOKEN="${arg#*=}" ;;
        --pairing-token=*) PAIRING_TOKEN="${arg#*=}" ;;
        --server-fp=*) SERVER_FP="${arg#*=}" ;;
        --ssh-user=*) SSH_USER="${arg#*=}" ;;
        --binary=*)   BINARY="${arg#*=}" ;;
        --version=*)  NETPULSE_VERSION="${arg#*=}" ;;
        --tmp)        USE_TMP=1 ;;
        --uninstall)  UNINSTALL=1 ;;
        --update-netgrip) UPDATE_NETGRIP=1 ;;
        --force)      NETGRIP_FORCE=1 ;;
        -h|--help)    usage ;;
        *) fatal 10 "opción desconocida: $arg (prueba --help)" ;;
    esac
done

[ -n "$HOST" ] || fatal 11 "falta --host (IP del router)"
SSH="$SSH_USER@$HOST"
command -v ssh >/dev/null 2>&1 || fatal 12 "necesito ssh"
command -v scp >/dev/null 2>&1 || fatal 12 "necesito scp"

# --------------------------------------------------------------- uninstall --
if [ "$UNINSTALL" -eq 1 ]; then
    info "desinstalando $BIN_NAME de $SSH"
    ssh "$SSH" "
        $INIT_DST stop 2>/dev/null || true
        $INIT_DST disable 2>/dev/null || true
        rm -f $INIT_DST $ENV_FILE /usr/sbin/$BIN_NAME /tmp/$BIN_NAME
        rm -f /usr/sbin/netpulse-watchdog /tmp/netpulse-agent.heartbeat
        # quitar la línea del watchdog del crontab (si existe)
        if [ -f /etc/crontabs/root ]; then
            sed -i '/netpulse-watchdog/d' /etc/crontabs/root
        fi
    "
    ok "$BIN_NAME desinstalado de $HOST"
    exit 0
fi

[ -n "$SERVER" ] || fatal 11 "falta --server (URL de NetPulse)"
[ -n "$SLUG" ]   || fatal 11 "falta --slug"
# Token: obligatorio --token o --pairing-token (no ambos)
if [ -z "$TOKEN" ] && [ -z "$PAIRING_TOKEN" ]; then
    fatal 11 "falta --token (o --pairing-token para bootstrap)"
fi
SERVER="${SERVER%/}"

ssh -o ConnectTimeout=8 -o BatchMode=yes "$SSH" true \
    || fatal 13 "no hay SSH a $SSH (¿dropbear + clave autorizada?)"

# ----------------------------------------------------- netgrip handoff --
# Si el router ya corre NetGrip, su agente EMBEBIDO cubre el sondeo: no se
# instala el standalone. Se le entrega la config (sin pisar la existente) y
# opcionalmente se actualiza el propio NetGrip (--update-netgrip).
if ssh "$SSH" '[ -x /usr/sbin/netgrip ] || [ -f /etc/init.d/netgrip ]'; then
    info "NetGrip detectado en $HOST: el agente embebido ya cubre este router"
    # Token line igual que el flujo normal
    if [ -n "$PAIRING_TOKEN" ]; then
        HANDOFF_TOKEN="NETPULSE_PAIRING_TOKEN=$PAIRING_TOKEN"
    else
        HANDOFF_TOKEN="NETPULSE_TOKEN=$TOKEN"
    fi
    HANDOFF_FP=""
    [ -n "$SERVER_FP" ] && HANDOFF_FP="NETPULSE_SERVER_FP=$SERVER_FP"
    ssh "$SSH" "NG_ENV=/etc/netgrip/netpulse.env sh -s" <<HANDOFF
set -eu
if [ ! -f "\$NG_ENV" ]; then
    mkdir -p /etc/netgrip
    {
        echo "# managed by netgrip; netpulse embedded agent config"
        echo "NETPULSE_SERVER=$SERVER"
        echo "NETPULSE_SLUG=$SLUG"
        echo "$HANDOFF_TOKEN"
        ${HANDOFF_FP:+echo "$HANDOFF_FP"}
        echo "NETPULSE_ENABLED=1"
    } > "\$NG_ENV"
    chmod 600 "\$NG_ENV"
    echo "config-escrita"
else
    echo "config-existente-conservada"
fi
HANDOFF

    if [ "$UPDATE_NETGRIP" -eq 1 ]; then
        # Versión instalada (registry apk/opkg; vacío si es binario manual)
        CUR=$(ssh "$SSH" "(apk info netgrip 2>/dev/null || opkg status netgrip 2>/dev/null | sed -n 's/^Version: //p') | head -1")
        if command -v curl >/dev/null 2>&1; then FETCH="curl -fsSL --retry 3 --connect-timeout 10"
        elif command -v wget >/dev/null 2>&1; then FETCH="wget -q -O-"
        else fatal 21 "necesito curl o wget"; fi
        NG_TAG=$($FETCH "https://api.github.com/repos/gnacho/netgrip/releases/latest" \
            | grep '"tag_name"' | head -1 | cut -d'"' -f4) || fatal 41 "no pude resolver la última release de NetGrip"
        NG_VER=$(echo "$NG_TAG" | sed 's/^v//')
        ARCH=$(ssh "$SSH" uname -m)
        case "$ARCH" in
            aarch64|arm64) NG_ARCH=arm64 ;;
            *) fatal 20 "NetGrip solo publica builds arm64 (router: $ARCH)" ;;
        esac
        if ssh "$SSH" command -v apk >/dev/null 2>&1; then
            NG_ASSET="netgrip-${NG_VER}-r1-${NG_ARCH}.apk"; NG_INST="apk add --allow-untrusted /tmp/netgrip-update.pkg"
        else
            NG_ASSET="netgrip_${NG_VER}-1_aarch64_cortex-a53.ipk"; NG_INST="opkg install /tmp/netgrip-update.pkg"
        fi
        NG_URL="https://github.com/gnacho/netgrip/releases/download/${NG_TAG}/${NG_ASSET}"
        info "actualizando NetGrip a $NG_TAG (instalado: ${CUR:-desconocido})"
        if [ -n "$CUR" ] && [ "$NETGRIP_FORCE" -ne 1 ]; then
            CUR_V=$(echo "$CUR" | sed 's/^netgrip-//; s/ description:.*//; s/-r[0-9]*$//')
            if [ "$CUR_V" = "$NG_VER" ]; then
                ok "NetGrip ya está en $NG_VER; nada que hacer (usa --force para reinstalar)"
                ssh "$SSH" "/etc/init.d/netgrip restart >/dev/null 2>&1 || true"
                ok "agente embebido rearmado (slug $SLUG)"
                exit 0
            fi
            OLDEST=$(printf '%s\n%s\n' "$CUR_V" "$NG_VER" | sort -V | head -1)
            if [ "$OLDEST" = "$NG_VER" ]; then
                warn "la release $NG_TAG es MÁS VIEJA que la instalada ($CUR_V): router con build no liberada"
                fatal 43 "downgrade rechazado (usa --force si de verdad lo quieres)"
            fi
        fi
        $FETCH "$NG_URL" -o "$TMP/netgrip-update.pkg" || fatal 42 "descarga falló: $NG_URL"
        scp -Oq "$TMP/netgrip-update.pkg" "$SSH:/tmp/netgrip-update.pkg"
        ssh "$SSH" "$NG_INST && rm -f /tmp/netgrip-update.pkg"
        ok "NetGrip actualizado a $NG_TAG"
    fi

    ssh "$SSH" "/etc/init.d/netgrip restart >/dev/null 2>&1 || true"
    ok "agente embebido de NetGrip rearmado (server $SERVER, slug $SLUG)"
    info "el agente standalone NO se instala en routers con NetGrip"
    exit 0
fi

# ------------------------------------------------------------------ binario --
BIN_DST="/usr/sbin/$BIN_NAME"
[ "$USE_TMP" -eq 1 ] && BIN_DST="/tmp/$BIN_NAME"

TMP=$(mktemp -d) || fatal 34 "mktemp falló"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT INT TERM

if [ -n "$BINARY" ]; then
    [ -f "$BINARY" ] || fatal 30 "binario local no encontrado: $BINARY"
    cp "$BINARY" "$TMP/$BIN_NAME"
    info "usando binario local: $BINARY"
else
    ARCH=$(ssh "$SSH" uname -m) || fatal 31 "no pude detectar la arquitectura de $HOST"
    case "$ARCH" in
        aarch64|arm64) GOARCH=arm64 ;;
        armv7l|armv7)  GOARCH=armv7 ;;
        mips)
            # uname -m dice "mips" para AMBOS endianness. El byte EI_DATA
            # (5º del ELF del sistema) decide: 0x01 = little (mipsle,
            # MT7621/ramips), 0x02 = big (mips, ath79). head|tail|tr en vez
            # de "od -t": busybox od no garantiza -t/-j/-N (#488).
            END=$(ssh "$SSH" 'head -c 6 /bin/sh | tail -c 1 | tr "\001\002" "12"')
            case "$END" in
                1) GOARCH=mipsle ;;
                2) GOARCH=mips ;;
                *) fatal 20 "no pude detectar el endianness MIPS de $HOST" ;;
            esac ;;
        *) fatal 20 "arquitectura no soportada: $ARCH (release: arm64, armv7, mipsle, mips)" ;;
    esac
    if command -v curl >/dev/null 2>&1; then FETCH="curl -fsSL --retry 3 --connect-timeout 10"
    elif command -v wget >/dev/null 2>&1; then FETCH="wget -q -O-"
    else fatal 21 "necesito curl o wget"; fi
    fetch_to() { $FETCH "$1" > "$2"; }
    command -v sha256sum >/dev/null 2>&1 || fatal 21 "falta sha256sum"
    if [ -z "$NETPULSE_VERSION" ]; then
        info "resolviendo última release"
        NETPULSE_VERSION=$($FETCH "https://api.github.com/repos/$GH_REPO/releases/latest" \
            | grep '"tag_name"' | head -1 | cut -d'"' -f4) \
            || fatal 31 "no pude resolver la última release; usa --version=X.Y.Z"
    fi
    VERSION_NORM=$(echo "$NETPULSE_VERSION" | sed 's/^v//')
    case "$NETPULSE_VERSION" in
        v*) NETPULSE_TAG="$NETPULSE_VERSION" ;;
        *)  NETPULSE_TAG="v$NETPULSE_VERSION" ;;
    esac
    ASSET="${BIN_NAME}_${VERSION_NORM}_linux_${GOARCH}.tar.gz"
    BASE_URL="https://github.com/$GH_REPO/releases/download/$NETPULSE_TAG"
    info "descargando $ASSET"
    fetch_to "$BASE_URL/$ASSET" "$TMP/$ASSET" || fatal 32 "descarga falló: $BASE_URL/$ASSET"
    fetch_to "$BASE_URL/checksums.txt" "$TMP/checksums.txt" || fatal 32 "checksums.txt no está en $NETPULSE_VERSION"
    SUM_FILE=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
    SUM_REF=$(grep "  $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')
    [ -n "$SUM_REF" ] || fatal 33 "$ASSET no está en checksums.txt"
    [ "$SUM_FILE" = "$SUM_REF" ] || fatal 33 "sha256 NO COINCIDE para $ASSET"
    ok "sha256 verificado"
    tar -xzf "$TMP/$ASSET" -C "$TMP"
fi
[ -s "$TMP/$BIN_NAME" ] || fatal 34 "no hay binario que instalar"

# ------------------------------------------------------------------ install --
info "copiando binario → $SSH:$BIN_DST"
# Reinstalación: si hay una versión previa en marcha, parar el servicio ANTES
# del scp — el proceso vivo tiene el binario abierto y la copia falla con
# "Text file busy". El servicio se reactiva al final (enable + restart).
ssh "$SSH" "/etc/init.d/$INIT_NAME stop >/dev/null 2>&1 || true"
# -O: protocolo SCP legacy — dropbear (OpenWrt) no tiene sftp-server y el
# scp de OpenSSH ≥9 usa SFTP por defecto → "connection closed" sin esto.
scp -Oq "$TMP/$BIN_NAME" "$SSH:$BIN_DST"
ssh "$SSH" "chmod 0755 $BIN_DST"
ok "binario en $BIN_DST"

# Config (chmod 600: el token solo lo lee root)
info "escribiendo $ENV_FILE (chmod 600)"
# Construir la línea de token: PAIRING_TOKEN (bootstrap) o TOKEN (normal)
if [ -n "$PAIRING_TOKEN" ]; then
    TOKEN_LINE="NETPULSE_PAIRING_TOKEN=$PAIRING_TOKEN"
else
    TOKEN_LINE="NETPULSE_TOKEN=$TOKEN"
fi
# Server fingerprint (si se proporciona)
FP_LINE=""
if [ -n "$SERVER_FP" ]; then
    FP_LINE="NETPULSE_SERVER_FP=$SERVER_FP"
fi
ssh "$SSH" "cat > $ENV_FILE && chmod 600 $ENV_FILE" <<EOF
# netpulse-agent — config (generado por install-agent.sh)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
$TOKEN_LINE
$FP_LINE
# NETPULSE_INTERVAL=15
# NETPULSE_WAN_TARGET=1.1.1.1      # solo si este equipo es el gateway
# NETPULSE_GW_TARGET=192.168.8.1   # ping al gateway (APs)
EOF
ok "config escrita"

# Servicio procd (respawn)
info "instalando servicio procd"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$SCRIPT_DIR/agent/deploy/$INIT_NAME.init" ]; then
    scp -Oq "$SCRIPT_DIR/agent/deploy/$INIT_NAME.init" "$SSH:$INIT_DST"
else
    # Instalación vía curl|sh (sin repo a mano): init embebido — es el MISMO
    # contenido que agent/deploy/netpulse-agent.init (mantener ambos a la par).
    ssh "$SSH" "cat > $INIT_DST" <<'INITEOF'
#!/bin/sh /etc/rc.common
# netpulse-agent — agente nativo NetPulse para OpenWrt (SPEC-AGENTE-PILOTO §2)
START=99
STOP=10
USE_PROCD=1

BIN=/usr/sbin/netpulse-agent
[ -x "$BIN" ] || BIN=/tmp/netpulse-agent

start_service() {
    procd_open_instance netpulse-agent
    procd_set_param command "$BIN"
    procd_set_param respawn "${respawn_threshold:-3600}" "${respawn_timeout:-5}" "${respawn_retry:-5}"
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param file /etc/netpulse-agent.env
    procd_close_instance
}
INITEOF
fi
ssh "$SSH" "chmod 0755 $INIT_DST && $INIT_DST enable && $INIT_DST restart"
ok "servicio $INIT_NAME habilitado y arrancado"

# ------------------------------------------------------------ watchdog cron --
# Fase 5 (Plan A): cron cada 2 min relanza el agente si procd se rindió o
# si está "vivo pero roto" (heartbeat viejo). Idempotente: reemplaza la
# línea previa del crontab.
info "instalando watchdog (cron, cada 2 min)"
WATCHDOG_DST="/usr/sbin/netpulse-watchdog"
if [ -f "$SCRIPT_DIR/agent/deploy/netpulse-watchdog.sh" ]; then
    scp -Oq "$SCRIPT_DIR/agent/deploy/netpulse-watchdog.sh" "$SSH:$WATCHDOG_DST"
else
    ssh "$SSH" "cat > $WATCHDOG_DST" <<'WATCHDOGEOF'
#!/bin/sh
INIT=/etc/init.d/netpulse-agent
HB=/tmp/netpulse-agent.heartbeat
MAX_AGE=300
[ -f /etc/netpulse-agent.env ] && . /etc/netpulse-agent.env 2>/dev/null
[ -n "${NETPULSE_HEARTBEAT_FILE:-}" ] && HB="$NETPULSE_HEARTBEAT_FILE"
log() { logger -t netpulse-watchdog "$*"; }
if [ ! -x /usr/sbin/netpulse-agent ] && [ ! -x /tmp/netpulse-agent ]; then
    exit 0
fi
# Nota: BusyBox pgrep -x compara contra la línea de comando completa,
# no solo el nombre base; /usr/sbin/netpulse-agent no coincide con
# netpulse-agent. Usamos pidof, que busca por comm/pidof y funciona
# tanto en BusyBox como en procps-ng.
if ! pidof netpulse-agent >/dev/null 2>&1; then
    log "agente no está en marcha — reiniciando servicio"
    $INIT restart >/dev/null 2>&1
    exit 0
fi
if [ -f "$HB" ]; then
    now=$(date +%s)
    hb=$(cat "$HB" 2>/dev/null)
    case "$hb" in
        '' | *[!0-9]*) exit 0 ;;
    esac
    age=$((now - hb))
    if [ "$age" -gt "$MAX_AGE" ]; then
        log "proceso vivo pero sin latido en ${age}s — reiniciando servicio"
        $INIT restart >/dev/null 2>&1
    fi
fi
exit 0
WATCHDOGEOF
fi
ssh "$SSH" "
    chmod 0755 $WATCHDOG_DST
    mkdir -p /etc/crontabs
    sed -i '/netpulse-watchdog/d' /etc/crontabs/root 2>/dev/null
    echo '*/2 * * * * $WATCHDOG_DST' >> /etc/crontabs/root
    /etc/init.d/cron enable
    /etc/init.d/cron restart
"
ok "watchdog en cron (*/2 * * * *)"

sleep 2
if ssh "$SSH" "logread -e $INIT_NAME" 2>/dev/null | tail -2 | grep -q .; then
    info "últimas líneas de syslog:"
    ssh "$SSH" "logread -e $INIT_NAME | tail -2" || true
fi

printf '\n%s================ %s instalado ================%s\n' "$C_G" "$BIN_NAME" "$C_0"
printf 'Router:   %s (slug: %s)\n' "$HOST" "$SLUG"
printf 'Binario:  %s%s\n' "$BIN_DST" "$( [ "$USE_TMP" -eq 1 ] && echo '  (variante /tmp: se pierde al reiniciar — reejecuta el instalador tras un reboot)' || true)"
printf 'Config:   %s (chmod 600; edítalo y haz /etc/init.d/%s restart)\n' "$ENV_FILE" "$INIT_NAME"
printf 'Servidor: %s\n' "$SERVER"
printf 'Logs:     ssh %s logread -e %s\n' "$SSH" "$INIT_NAME"
printf 'Verifica: GET %s/api/agents (last_seen + versión en unos 15 s)\n' "$SERVER"
printf '%s================================================%s\n\n' "$C_G" "$C_0"
