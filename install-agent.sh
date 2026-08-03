#!/bin/sh
# =============================================================================
# NetPulse Agent — one-liner installer (OpenWrt, vía SSH desde esta máquina)
#
#   Instala el agente nativo netpulse-agent en un router/AP OpenWrt: copia el
#   binario (arm64/armv7), escribe /etc/netpulse-agent.env (chmod 600) e
#   instala el servicio procd (respawn) habilitado y arrancado.
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
#   --token X       token del equipo (obligatorio salvo --uninstall)
#   --ssh-user X    usuario SSH del router (default: root)
#   --binary X      binario local a copiar (default: descarga de la release)
#   --version X.Y.Z versión a descargar (default: latest)
#   --tmp           instala el binario en /tmp (RAM) en vez de /usr/sbin:
#                   para equipos con flash justa. OJO: /tmp se pierde al
#                   reiniciar — tras un reboot hay que reejecutar este script
#                   (variante documentada del SPEC; el .ipk llega en el incr. 2)
#   --uninstall     detiene y elimina servicio, binario y config del router
#
# Requisitos: ssh/scp al router (OpenWrt con dropbear), curl o wget local.
# =============================================================================
set -eu

GH_REPO="gnacho/netpulse"
BIN_NAME="netpulse-agent"
ENV_FILE="/etc/netpulse-agent.env"
INIT_NAME="netpulse-agent"
INIT_DST="/etc/init.d/$INIT_NAME"

HOST=""; SERVER=""; SLUG=""; TOKEN=""; SSH_USER="root"; BINARY=""; VERSION=""; USE_TMP=0; UNINSTALL=0

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
        --ssh-user=*) SSH_USER="${arg#*=}" ;;
        --binary=*)   BINARY="${arg#*=}" ;;
        --version=*)  VERSION="${arg#*=}" ;;
        --tmp)        USE_TMP=1 ;;
        --uninstall)  UNINSTALL=1 ;;
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
    "
    ok "$BIN_NAME desinstalado de $HOST"
    exit 0
fi

[ -n "$SERVER" ] || fatal 11 "falta --server (URL de NetPulse)"
[ -n "$SLUG" ]   || fatal 11 "falta --slug"
[ -n "$TOKEN" ]  || fatal 11 "falta --token (se muestra una vez al crearlo: POST /api/agents)"
SERVER="${SERVER%/}"

ssh -o ConnectTimeout=8 -o BatchMode=yes "$SSH" true \
    || fatal 13 "no hay SSH a $SSH (¿dropbear + clave autorizada?)"

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
        *) fatal 20 "arquitectura no soportada: $ARCH (release: arm64, armv7)" ;;
    esac
    if command -v curl >/dev/null 2>&1; then FETCH="curl -fsSL --retry 3 --connect-timeout 10"
    elif command -v wget >/dev/null 2>&1; then FETCH="wget -q -O-"
    else fatal 21 "necesito curl o wget"; fi
    fetch_to() { $FETCH "$1" > "$2"; }
    command -v sha256sum >/dev/null 2>&1 || fatal 21 "falta sha256sum"
    if [ -z "$VERSION" ]; then
        info "resolviendo última release"
        VERSION=$($FETCH "https://api.github.com/repos/$GH_REPO/releases/latest" \
            | grep '"tag_name"' | head -1 | cut -d'"' -f4) \
            || fatal 31 "no pude resolver la última release; usa --version=X.Y.Z"
    fi
    VERSION_NORM=$(echo "$VERSION" | sed 's/^v//')
    ASSET="${BIN_NAME}_${VERSION_NORM}_linux_${GOARCH}.tar.gz"
    BASE_URL="https://github.com/$GH_REPO/releases/download/$VERSION"
    info "descargando $ASSET"
    fetch_to "$BASE_URL/$ASSET" "$TMP/$ASSET" || fatal 32 "descarga falló: $BASE_URL/$ASSET"
    fetch_to "$BASE_URL/checksums.txt" "$TMP/checksums.txt" || fatal 32 "checksums.txt no está en $VERSION"
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
scp -q "$TMP/$BIN_NAME" "$SSH:$BIN_DST"
ssh "$SSH" "chmod 0755 $BIN_DST"
ok "binario en $BIN_DST"

# Config (chmod 600: el token solo lo lee root)
info "escribiendo $ENV_FILE (chmod 600)"
ssh "$SSH" "cat > $ENV_FILE && chmod 600 $ENV_FILE" <<EOF
# netpulse-agent — config (generado por install-agent.sh)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=$TOKEN
# NETPULSE_INTERVAL=15
# NETPULSE_WAN_TARGET=1.1.1.1      # solo si este equipo es el gateway
# NETPULSE_GW_TARGET=192.168.8.1   # ping al gateway (APs)
# NETPULSE_INSECURE_TLS=1          # solo si el servidor usa cert autofirmado
EOF
ok "config escrita"

# Servicio procd (respawn)
info "instalando servicio procd"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$SCRIPT_DIR/agent/deploy/$INIT_NAME.init" ]; then
    scp -q "$SCRIPT_DIR/agent/deploy/$INIT_NAME.init" "$SSH:$INIT_DST"
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
