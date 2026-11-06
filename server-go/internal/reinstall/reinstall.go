// Package reinstall: construcción del script de instalación del agente
// netpulse-agent en un router OpenWrt (#246, #457, #463). Lo comparten el
// handler manual (POST /api/agents/{slug}/reinstall) y el supervisor del
// rearmer (escalado restart → reinstall), de ahí su propio paquete: httpapi
// importa rearmer y ninguno de los dos puede importar al otro.
package reinstall

import (
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/agentbin"
)

// Trust is how an agent reaches the server over HTTPS. FORK.
//
// The zero value leaves the script behaving as before. With a CA, the script
// writes the root to the router - over the SSH session it runs in, whose host
// key the server has verified - and downloads verify the server against it,
// so the agent token in the request header is not handed to whoever answers.
// The binary's sha256 was always checked; this protects the token.
type Trust struct {
	ServerURL string // replaces the URL given to Script, e.g. https://host:443
	ServerFP  string // written as NETPULSE_SERVER_FP: what the agent pins
	CAPEM     []byte // the root to verify downloads with
}

// caPath is where the router keeps the root.
const caPath = "/etc/netpulse-ca.pem"

// Script construye el POSIX sh que se ejecuta en el router: instala el
// agente completo (binario verificado por sha256, config .env, init procd
// con self-heal) de forma idempotente. Ya NO instala el watchdog con cron
// (#851): si el agente crashea en bucle, un reinicio a ciegas no lo arregla
// y ensucia el log; la detección efectiva es el dead-man del servidor. La
// caída puntual la cubre el respawn de procd; el binario tras sysupgrade lo
// restaura el self-heal del init.
// digests mapa arch→sha256 del binario embebido; un arch sin digest queda
// sin verificación (build dev) en lugar de bloquear.
// serverFP es el SPKI pin del server (#851): si serverURL es https y el
// server lo pudo derivar (ServerFP), va en el .env para que el agente no
// entre en bucle fatal de "HTTPS requiere NETPULSE_SERVER_FP"; vacío = el
// comportamiento previo (env sin FP).
//
// FORK: trust, when given, is how the agent should reach the server now -
// the HTTPS address with the private CA - and wins over serverURL and
// serverFP; its root is written to the router to verify downloads with.
// TargetsFor: resuelve los objetivos de sonda para un slug desde la tabla de
// routers (#1204): la unidad gateway hace ping a internet; el resto, al
// gateway (su IP de la tabla). Sin gateway declarado no se escribe nada.
type RouterRow struct {
	ID        string
	Host      string
	IsGateway bool
	// AgentInterval (#1476): cadencia de push del agente en segundos. 0 =
	// default del agente.
	AgentInterval int
}

func TargetsFor(routers []RouterRow, slug string) WanTargets {
	gwIP := ""
	for _, rc := range routers {
		if rc.IsGateway && rc.ID != slug {
			gwIP = rc.Host
		}
	}
	for _, rc := range routers {
		if rc.ID != slug {
			continue
		}
		if rc.IsGateway {
			return WanTargets{WanTarget: "1.1.1.1", Interval: rc.AgentInterval}
		}
		if gwIP != "" {
			return WanTargets{GwTarget: gwIP, Interval: rc.AgentInterval}
		}
	}
	return WanTargets{}
}

func Script(slug, token, serverURL, serverFP string, digests map[string]string, trust ...Trust) string {
	return ScriptWithTargets(slug, token, serverURL, serverFP, digests, WanTargets{}, trust...)
}

// WanTargets (#1204): la sonda de latencia del agente solo corre con
// NETPULSE_WAN_TARGET (gateway) o NETPULSE_GW_TARGET (APs) en su env, y el
// instalador histórico las dejaba comentadas: ninguna instalación las tenía
// y las tarjetas de latencia se quedaban sin datos para siempre. El server
// sabe qué unidad es el gateway: resuelve los objetivos y el script escribe
// las líneas ACTIVAS. Unidades ya instaladas: un reinstall desde la UI las
// estrena.
type WanTargets struct {
	WanTarget string // gateway: ping a internet (p. ej. "1.1.1.1"); vacío = comentada
	GwTarget  string // APs: IP del gateway para el ping; vacío = comentada
	// Interval (#1476): cadencia de push del agente en segundos. 0 deja el
	// default del agente (30 s) y borra la clave de la config UCI.
	Interval int
}

// caSetupBlock escribe (o borra) la raíz de la CA en el router. Without a
// root, one left from an earlier install (a CA since replaced) would make
// every https download fail against it.
func caSetupBlock(tr Trust) string {
	if len(tr.CAPEM) == 0 {
		return "\nrm -f " + caPath + "\n"
	}
	return `
# FORK: the server's root, to verify downloads with (written over this SSH
# session, so it is the server's own)
cat > ` + caPath + ` <<'CAEOF'
` + strings.TrimSpace(string(tr.CAPEM)) + `
CAEOF
chmod 644 ` + caPath + `
`
}

// envRewriteBlock regenera /etc/netpulse-agent.env con las vars gestionadas
// actuales, conservando las NETPULSE_* del usuario (#851). Usa las vars de
// entorno del script anfitrión: SERVER, SLUG, TOKEN y SERVER_FP.
//
// #1558: el env sale con NETPULSE_SCAN_INTERVAL=0 activo. El scan periódico
// de vecinos saca la radio del canal y puede tirar clientes sensibles a
// beacons (#1557); la vista de canales pide el scan bajo demanda (#1561). Un
// valor que ya esté en el env gana, para no pisar un override manual (#851).
func envRewriteBlock(targets WanTargets) string {
	return `# #1853: escribir la config no puede abortar el reinstall. El env se
# compone en un temporal y solo se pone en su sitio si salio entero, asi que un
# /etc lleno o de solo lectura conserva el anterior en vez de dejarlo a medias.
warn() { echo "netpulse-agent: $*" >&2; }
# #1894: aviso normal por stdout. El journal y la caja de la UI enseñan lo que
# el script cuenta; sin esto una instalación correcta no dejaba ni una línea.
say() { echo "netpulse-agent: $*"; }
USER_VARS=""
SCAN_VALUE="0"
if [ -f "$ENV_FILE" ]; then
	prev_scan=$(grep -E '^NETPULSE_SCAN_INTERVAL=' "$ENV_FILE" | tail -n 1) || prev_scan=""
	if [ -n "$prev_scan" ]; then SCAN_VALUE=${prev_scan#NETPULSE_SCAN_INTERVAL=}; fi
	USER_VARS=$(grep -E '^NETPULSE_[A-Z0-9_]+=' "$ENV_FILE" | grep -vE '^NETPULSE_(SERVER|SLUG|TOKEN|SERVER_FP|PAIRING_TOKEN|WAN_TARGET|GW_TARGET|SCAN_INTERVAL)=') || true
fi
ENV_TMP="$ENV_FILE.new.$$"
write_env() {
	cat > "$ENV_TMP" <<EOF
# netpulse-agent — config (generado por reinstall)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=$TOKEN
# NETPULSE_SERVER_FP=<sha256>    # pin SPKI del server (https); lo rellena el instalador
# NETPULSE_INTERVAL=30           # segundos entre pushes
# NETPULSE_SCAN_INTERVAL: "0" = sin scans periódicos (bajo demanda desde
# "Análisis de canales"); "30m" u otro valor los reactiva.
NETPULSE_SCAN_INTERVAL=$SCAN_VALUE
` + func() string {
		wanLine := "# NETPULSE_WAN_TARGET=1.1.1.1    # solo si este equipo es el gateway"
		if targets.WanTarget != "" {
			wanLine = "NETPULSE_WAN_TARGET=" + targets.WanTarget
		}
		gwLine := "# NETPULSE_GW_TARGET=192.168.8.1 # ping al gateway (APs)"
		if targets.GwTarget != "" {
			gwLine = "NETPULSE_GW_TARGET=" + targets.GwTarget
		}
		return wanLine + "\n" + gwLine
	}() + `
# NETPULSE_HEARTBEAT_FILE=/tmp/netpulse-agent.heartbeat
EOF
# #851: con server https el agente exige el SPKI pin; vacío = http plano o
# el server no pudo derivarlo (se comporta como antes).
if [ -n "$SERVER_FP" ]; then
	echo "NETPULSE_SERVER_FP=$SERVER_FP" >> "$ENV_TMP" || return 1
fi
if [ -n "$USER_VARS" ]; then
	printf '%s\n' "$USER_VARS" >> "$ENV_TMP" || return 1
fi
chmod 600 "$ENV_TMP" || return 1
mv -f "$ENV_TMP" "$ENV_FILE" || return 1
}
if ! write_env; then
	rm -f "$ENV_TMP"
	warn "no pude escribir $ENV_FILE; se queda el anterior"
fi
`
}

// uciConfigBlock (#1476) sincroniza la configuración nativa del paquete con
// el env que acaba de escribir envRewriteBlock. /etc/config/netpulse-agent es
// un conffile, así que sobrevive a sysupgrade, y el init procd del paquete lee
// UCI (#1772): sin este bloque un reinstall dejaba el env al día y la config
// nativa con el token viejo. Solo actúa si el fichero existe, es decir, si el
// router tiene el paquete instalado; una instalación por ficheros sueltos
// sigue funcionando con el env. No toca `interval`: no lo gestiona el
// reinstall y pisarlo borraría un ajuste del usuario. Los campos opcionales
// vacíos se borran para no dejar un target falso.
func uciConfigBlock(targets WanTargets) string {
	return `UCI_CFG=/etc/config/netpulse-agent
if [ -f "$UCI_CFG" ] && command -v uci >/dev/null 2>&1; then
	uci -q set netpulse-agent.main.server="$SERVER" || true
	uci -q set netpulse-agent.main.slug="$SLUG" || true
	uci -q set netpulse-agent.main.token="$TOKEN" || true
	if [ -n "$SERVER_FP" ]; then
		uci -q set netpulse-agent.main.server_fp="$SERVER_FP" || true
	else
		uci -q delete netpulse-agent.main.server_fp || true
	fi
	uci -q set netpulse-agent.main.scan_interval="$SCAN_VALUE" || true
` + func() string {
		out := ""
		if targets.WanTarget != "" {
			out += "\tuci -q set netpulse-agent.main.wan_target=\"" + targets.WanTarget + "\" || true\n"
		} else {
			out += "\tuci -q delete netpulse-agent.main.wan_target || true\n"
		}
		if targets.GwTarget != "" {
			out += "\tuci -q set netpulse-agent.main.gw_target=\"" + targets.GwTarget + "\" || true\n"
		} else {
			out += "\tuci -q delete netpulse-agent.main.gw_target || true\n"
		}
		// #1476: el intervalo es un ajuste por router. 0 vuelve al default
		// del agente, así que se borra la clave en vez de escribir un cero.
		if targets.Interval > 0 {
			out += "\tuci -q set netpulse-agent.main.interval=\"" + strconv.Itoa(targets.Interval) + "\" || true\n"
		} else {
			out += "\tuci -q delete netpulse-agent.main.interval || true\n"
		}
		return out
	}() + `	uci -q commit netpulse-agent || true
fi
`
}

// preserveOnSysupgrade (#1476) añade los ficheros del agente a
// /etc/sysupgrade.conf. OpenWrt solo conserva en una actualización de imagen los
// conffiles declarados por los paquetes instalados, lo listado en ese fichero y
// /lib/upgrade/keep.d/*; el init y el env los escribe este script como ficheros
// sueltos, así que sin esta lista desaparecen y el self-heal nunca llega a
// correr, porque lo que se pierde es el punto de entrada. Idempotente (no
// duplica líneas) y el mismo gesto que firmware.EnsureStackPreserved (owut).
const preserveOnSysupgrade = `
# Preservar el agente en sysupgrade (#1476): mismo gesto que el flujo owut.
P=/etc/sysupgrade.conf
touch "$P" 2>/dev/null || true
ensure_sysupgrade() { grep -qxF "$1" "$P" 2>/dev/null || echo "$1" >> "$P" 2>/dev/null || true; }
ensure_sysupgrade /usr/sbin/netpulse-agent
ensure_sysupgrade /etc/netpulse-agent.env
ensure_sysupgrade /etc/init.d/netpulse-agent
for f in /etc/rc.d/*netpulse-agent*; do [ -e "$f" ] && ensure_sysupgrade "$f"; done
# Conffile y defaults del paquete: en una instalación por paquete OpenWrt los
# conserva por sí solo, pero declararlos aquí cubre el caso de un router que
# recibió el paquete y luego un reinstall por SSH.
for f in /etc/config/netpulse-agent /etc/uci-defaults/90-netpulse-agent; do
	[ -e "$f" ] && ensure_sysupgrade "$f"
done
true
`

// EnvScript (#1294) es la variante LIGERA del script de instalación: NO
// descarga binario ni toca el init; solo reescribe la CA raíz y
// /etc/netpulse-agent.env (rotando el token) y reinicia el agente para que
// relea el env. Es lo que convierte un cambio de TLS/HTTPS en un despliegue
// de un clic: enable HTTPS → re-emit → toda la flota SSH-reachable migra sin
// reinstalar a mano. Mismos gestos de trust que ScriptWithTargets.
func EnvScript(slug, token, serverURL, serverFP string, targets WanTargets, trust ...Trust) string {
	var tr Trust
	if len(trust) > 0 {
		tr = trust[0]
	}
	if tr.ServerURL != "" {
		serverURL = tr.ServerURL
	}
	if tr.ServerFP != "" {
		serverFP = tr.ServerFP
	}
	return `#!/bin/sh
set -e
INIT=/etc/init.d/netpulse-agent
ENV_FILE=/etc/netpulse-agent.env
SERVER="` + serverURL + `"
SLUG="` + slug + `"
TOKEN="` + token + `"
SERVER_FP="` + serverFP + `"
` + caSetupBlock(tr) + envRewriteBlock(targets) + uciConfigBlock(targets) + preserveOnSysupgrade + `
# Releer el env: el agente lo lee del fichero en cada arranque (#1287), así
# que basta un restart del init procd (respawn inmediato).
if [ -f "$INIT" ]; then
	"$INIT" restart >/dev/null 2>&1 || true
fi
`
}

func ScriptWithTargets(slug, token, serverURL, serverFP string, digests map[string]string, targets WanTargets, trust ...Trust) string {
	var tr Trust
	if len(trust) > 0 {
		tr = trust[0]
	}
	if tr.ServerURL != "" {
		serverURL = tr.ServerURL
	}
	if tr.ServerFP != "" {
		serverFP = tr.ServerFP
	}
	caSetup := caSetupBlock(tr)
	return `#!/bin/sh
set -e
INIT=/etc/init.d/netpulse-agent
BIN=/usr/sbin/netpulse-agent
ENV_FILE=/etc/netpulse-agent.env
SERVER="` + serverURL + `"
SLUG="` + slug + `"
TOKEN="` + token + `"
SERVER_FP="` + serverFP + `"
` + caSetup + `
# FORK: verify https downloads against the server's root when there is one
CURL_TLS=""; WGET_TLS=""
if [ -f ` + caPath + ` ]; then CURL_TLS="--cacert ` + caPath + `"; WGET_TLS="--ca-certificate=` + caPath + `"; fi

# Detectar arquitectura del router y el digest esperado del binario embebido
ARCH=$(uname -m)
case "$ARCH" in
	aarch64|arm64)  GOARCH=arm64; SHA256="` + digests["arm64"] + `" ;;
	armv7l|armv7|armhf|arm) GOARCH=arm; SHA256="` + digests["arm"] + `" ;;
	x86_64|amd64)   GOARCH=amd64; SHA256="` + digests["amd64"] + `" ;;
	mips)
		# uname -m dice "mips" para AMBOS endianness: el byte EI_DATA (5º del
		# ELF) decide. head|tail|tr en vez de "od -t": busybox no lo garantiza.
		case "$(head -c 6 /bin/sh | tail -c 1 | tr '\001\002' '12')" in
			1) GOARCH=mipsle; SHA256="` + digests["mipsle"] + `" ;;
			2) GOARCH=mips;  SHA256="` + digests["mips"] + `" ;;
			*) echo "no pude detectar el endianness de $ARCH" >&2; exit 20 ;;
		esac ;;
	*) echo "arch no soportado: $ARCH" >&2; exit 20 ;;
esac
say "arquitectura $ARCH ($GOARCH)"

# #1476: si el router habla opkg o apk, instala el paquete que sirve el propio
# servidor. Deja binario, init y /etc/config/netpulse-agent, que es conffile y
# sobrevive a un sysupgrade. El .ipk lo compone el servidor al vuelo desde el
# binario embebido; el .apk lo firma el CI y solo esta si el operador lo dejo
# junto al binario. Cualquier fallo (sin gestor, descarga rota, paquete
# rechazado) cae al camino de ficheros sueltos de abajo: el paquete es una
# mejora, no un requisito.
PKG_OK=0
PKG_URL="$SERVER/api/agents/$SLUG/package?arch=$GOARCH"
pkg_fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL $CURL_TLS --connect-timeout 10 -m 600 -H "Authorization: Bearer $TOKEN" "$1" -o "$2" 2>/dev/null
	else
		wget -q $WGET_TLS -T 60 -O "$2" --header="Authorization: Bearer $TOKEN" "$1" 2>/dev/null
	fi
}
if command -v opkg >/dev/null 2>&1; then
	pkg_fetch "$PKG_URL&format=ipk" /tmp/netpulse-agent.ipk || rm -f /tmp/netpulse-agent.ipk
	if [ -s /tmp/netpulse-agent.ipk ] && opkg install --force-reinstall /tmp/netpulse-agent.ipk >/dev/null 2>&1; then
		PKG_OK=1
	else
		echo "netpulse-agent: el paquete .ipk no se pudo instalar; sigo con los ficheros sueltos" >&2
	fi
	rm -f /tmp/netpulse-agent.ipk
elif command -v apk >/dev/null 2>&1; then
	pkg_fetch "$PKG_URL&format=apk" /tmp/netpulse-agent.apk || rm -f /tmp/netpulse-agent.apk
	if [ -s /tmp/netpulse-agent.apk ] && apk add --allow-untrusted /tmp/netpulse-agent.apk >/dev/null 2>&1; then
		PKG_OK=1
	else
		echo "netpulse-agent: el paquete .apk no se pudo instalar; sigo con los ficheros sueltos" >&2
	fi
	rm -f /tmp/netpulse-agent.apk
fi
if [ "$PKG_OK" = "1" ]; then say "paquete instalado"; else say "sin paquete; instalo el binario suelto"; fi
if [ "$PKG_OK" != "1" ]; then
# Descargar el binario del propio server (auth por token)
# #1737: sin estos mensajes el fallo llega como "Process exited with status
# 1" y nada mas. wget -q no imprime su motivo, asi que la descarga fallida
# por wget era completamente muda.
if command -v curl >/dev/null 2>&1; then
	curl -fsSL $CURL_TLS --connect-timeout 10 -m 600 -H "Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH" -o /tmp/netpulse-agent.new || { echo "netpulse-agent: no pude descargar el binario de $SERVER (curl)" >&2; exit 22; }
else
	wget -q $WGET_TLS -T 60 -O /tmp/netpulse-agent.new --header="Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH" || { echo "netpulse-agent: no pude descargar el binario de $SERVER (wget)" >&2; exit 22; }
fi

# Verificación sha256 contra el digest embebido (#463); vacío = sin verificar
if [ -n "$SHA256" ]; then
	GOT=$(sha256sum /tmp/netpulse-agent.new | awk '{print $1}')
	[ "$GOT" = "$SHA256" ] || { echo "sha256 no coincide (esperado $SHA256, obtenido $GOT)" >&2; exit 21; }
fi
chmod 0755 /tmp/netpulse-agent.new
say "binario descargado y verificado"
# #1853: el servicio se para AQUI, con el binario ya descargado y verificado.
# Pararlo antes dejaba el router sin agente si la descarga fallaba. Hay que
# pararlo igualmente: /tmp es tmpfs y /usr es overlay, asi que mv escribe el
# destino y un binario en ejecucion da ETXTBSY. Si el reemplazo falla, se
# vuelve a arrancar el anterior en vez de dejarlo caido.
[ -f "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true
if ! mv -f /tmp/netpulse-agent.new "$BIN"; then
	echo "netpulse-agent: no pude reemplazar $BIN; vuelvo a arrancar el agente anterior" >&2
	[ -f "$INIT" ] && "$INIT" start >/dev/null 2>&1 || true
	exit 23
fi
fi

# Config (chmod 600). El rewrite toca SOLO las vars gestionadas (SERVER,
# SLUG, TOKEN, SERVER_FP, WAN_TARGET, GW_TARGET): las NETPULSE_* del usuario
# (p. ej. NETPULSE_SCAN_INTERVAL=0) se conservan tal cual (#851).
` + envRewriteBlock(targets) + uciConfigBlock(targets) + `
if [ "$PKG_OK" != "1" ]; then
# Init procd con self-heal (#457): un sysupgrade no conserva los ficheros
# sueltos, así que el binario, el init y el env se añaden a
# /etc/sysupgrade.conf (#1476); si aun así falta el binario al arrancar se
# descarga del server con este env.
INIT_TMP="/tmp/netpulse-agent.init.$$"
# #1853: el init se escribe en un temporal y se mueve solo si salio entero, para
# no dejar /etc/init.d a medias si el disco esta lleno.
if cat > "$INIT_TMP" <<'INITEOF'
#!/bin/sh /etc/rc.common
# netpulse-agent — agente nativo NetPulse para OpenWrt, con self-heal.
# Config en /etc/netpulse-agent.env (chmod 600); procd reinicia si cambia.
START=99
STOP=10
USE_PROCD=1

ENV_FILE=/etc/netpulse-agent.env
BIN=/usr/sbin/netpulse-agent
[ -x "$BIN" ] || BIN=/tmp/netpulse-agent

selfheal_binary() {
	[ -x "$BIN" ] && return 0
	[ -f "$ENV_FILE" ] || return 1
	. "$ENV_FILE" 2>/dev/null
	[ -n "${NETPULSE_SERVER:-}" ] && [ -n "${NETPULSE_TOKEN:-}" ] && [ -n "${NETPULSE_SLUG:-}" ] || return 1
	case "${NETPULSE_SERVER%%:*}" in
		https|HTTPS) ;;
		*)
			[ "${NETPULSE_ALLOW_INSECURE_COMMANDS:-}" = "1" ] || {
				logger -t netpulse-agent "self-heal: HTTP sin NETPULSE_ALLOW_INSECURE_COMMANDS=1, binario no descargado"
				return 1
			}
			;;
	esac
	case "$(uname -m)" in
		aarch64|arm64) local ARCH=arm64 ;;
		armv7l|armv7|armhf|arm) local ARCH=arm ;;
		x86_64|amd64) local ARCH=amd64 ;;
		mips)
			case "$(head -c 6 /bin/sh | tail -c 1 | tr '\001\002' '12')" in
				1) local ARCH=mipsle ;;
				2) local ARCH=mips ;;
				*) return 1 ;;
			esac ;;
		*) return 1 ;;
	esac
	local url tmp hdr digest sum
	url="${NETPULSE_SERVER%/}/api/agents/${NETPULSE_SLUG}/binary?arch=${ARCH}"
	logger -t netpulse-agent "self-heal: binario ausente, descargando de $NETPULSE_SERVER"
	tmp=/tmp/netpulse-agent.$$
	hdr=/tmp/netpulse-agent.hdr.$$
	local ctls="" wtls=""
	if [ -f /etc/netpulse-ca.pem ]; then ctls="--cacert /etc/netpulse-ca.pem"; wtls="--ca-certificate=/etc/netpulse-ca.pem"; fi
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL $ctls -m 120 -D "$hdr" -H "Authorization: Bearer $NETPULSE_TOKEN" -o "$tmp" "$url" || { rm -f "$tmp" "$hdr"; return 1; }
	else
		wget -S $wtls -T 60 -O "$tmp" --header="Authorization: Bearer $NETPULSE_TOKEN" "$url" 2>"$hdr" || { rm -f "$tmp" "$hdr"; return 1; }
	fi
	digest=$(grep -i '^[[:space:]]*x-checksum-sha256:' "$hdr" 2>/dev/null | tail -n 1 | tr -d '\r' | awk '{print $2}')
	rm -f "$hdr"
	case "$digest" in
		""|*[!0123456789abcdefABCDEF]*)
			logger -t netpulse-agent "self-heal: sin X-Checksum-Sha256 válido, binario descartado"
			rm -f "$tmp"
			return 1 ;;
	esac
	[ "${#digest}" -eq 64 ] || { logger -t netpulse-agent "self-heal: digest con longitud inválida, binario descartado"; rm -f "$tmp"; return 1; }
	sum=$(sha256sum "$tmp" 2>/dev/null | awk '{print $1}')
	if [ -z "$sum" ] || [ "$(printf '%s' "$sum" | tr 'A-F' 'a-f')" != "$(printf '%s' "$digest" | tr 'A-F' 'a-f')" ]; then
		logger -t netpulse-agent "self-heal: SHA-256 no coincide, binario descartado"
		rm -f "$tmp"
		return 1
	fi
	chmod 0755 "$tmp" && mv "$tmp" /usr/sbin/netpulse-agent || { rm -f "$tmp"; return 1; }
	logger -t netpulse-agent "self-heal: binario restaurado"
	BIN=/usr/sbin/netpulse-agent
	return 0
}


# (#879) self-heal: tras un sysupgrade /etc sobrevive pero el binario del
# watchdog no: si quedó la entrada de cron del antiguo watchdog, retirarla
# (cron loguearía un comando inexistente cada 2 min).
cleanup_stale_watchdog_cron() {
	[ -f /usr/sbin/netpulse-watchdog ] && return 0
	crontab -l 2>/dev/null | grep -q netpulse-watchdog || return 0
	( crontab -l 2>/dev/null | grep -v netpulse-watchdog ) | crontab - 2>/dev/null || true
	logger -t netpulse-agent "watchdog cron huérfano retirado (#879)"
}

# #1857: si el binario no está y el self-heal no lo trae, no se registra la
# instancia. procd arrancaría una ruta inexistente, reintentaría y se
# rendiría, dejando el servicio como arrancado y el agente muerto sin más
# rastro que una línea en el log.
start_service() {
	cleanup_stale_watchdog_cron
	if ! selfheal_binary; then
		logger -t netpulse-agent "self-heal: no se pudo restaurar el binario; el servicio no arranca"
		return 0
	fi
	procd_open_instance netpulse-agent
	procd_set_param command "$BIN"
	procd_set_param respawn "${respawn_threshold:-3600}" "${respawn_timeout:-5}" "${respawn_retry:-5}"
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_set_param file /etc/netpulse-agent.env
	procd_close_instance
}
INITEOF
then
	chmod 0755 "$INIT_TMP" && mv -f "$INIT_TMP" "$INIT" || { rm -f "$INIT_TMP"; warn "no pude instalar $INIT"; }
else
	rm -f "$INIT_TMP"
	warn "no pude preparar $INIT"
fi
fi

# #851: el watchdog cron YA NO se instala (un reinicio a ciegas de un crash
# loop no arregla nada y ensucia el log; la detección es el dead-man del
# servidor). Si el router viene de una instalación que sí lo tenía, limpiarlo.
rm -f /usr/sbin/netpulse-watchdog || warn "no pude borrar /usr/sbin/netpulse-watchdog"
if [ -f /etc/crontabs/root ]; then
	sed -i '/netpulse-watchdog/d' /etc/crontabs/root || warn "no pude limpiar el cron del watchdog"
fi

# #1853: de aqui al final nada puede abortar en silencio: el binario nuevo ya
# esta en su sitio y el servicio sigue parado, asi que se avisa y se arranca.
"$INIT" enable >/dev/null 2>&1 || warn "no pude habilitar $INIT"
` + preserveOnSysupgrade + `
if ! "$INIT" restart >/dev/null 2>&1; then
	warn "el agente no arranco tras el reinstall; revisa $INIT"
	exit 24
fi
say "agente arrancado"
`
}

// TokenPushScript construye el POSIX sh que rota el token en CALIENTE: solo
// reescribe el token en /etc/netpulse-agent.env (conservando server, slug,
// FP del pin SPKI y las NETPULSE_* del usuario) y reinicia el servicio. No
// descarga binario ni toca el init: es la versión ligera de reinstall para
// cuando el token cambia pero el binario y la config ya están bien (rotate
// "en caliente").
func TokenPushScript(slug, token string) string {
	return `#!/bin/sh
set -e
ENV_FILE=/etc/netpulse-agent.env
INIT=/etc/init.d/netpulse-agent
# El env debe existir: el router ya tenía el agente instalado.
[ -f "$ENV_FILE" ] || { echo "netpulse-agent.env no existe; token no actualizado" >&2; exit 30; }
# Conservar el server y el slug del env existente para no romper la config.
SERVER=$(sed -n 's/^NETPULSE_SERVER=//p' "$ENV_FILE" | head -n1)
SLUG=$(sed -n 's/^NETPULSE_SLUG=//p' "$ENV_FILE" | head -n1)
# #851: conservar también el FP del pin SPKI y las NETPULSE_* no gestionadas
# (p. ej. NETPULSE_SCAN_INTERVAL=0); si se pierden, el agente cae en bucle
# fatal (https sin FP) o pierde la config del usuario. TOKEN y PAIRING_TOKEN
# se descartan a propósito: el token rota y un pairing token residual ya no
# aplica una vez emparejado.
FP=$(sed -n 's/^NETPULSE_SERVER_FP=//p' "$ENV_FILE" | head -n1)
USER_VARS=$(grep -E '^NETPULSE_[A-Z0-9_]+=' "$ENV_FILE" | grep -vE '^NETPULSE_(SERVER|SLUG|TOKEN|SERVER_FP|PAIRING_TOKEN)=') || true
[ -n "$SLUG" ] || { echo "netpulse-agent.env sin NETPULSE_SLUG" >&2; exit 31; }
umask 077
cat > "$ENV_FILE.tmp" <<EOF
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=` + token + `
EOF
if [ -n "$FP" ]; then
	echo "NETPULSE_SERVER_FP=$FP" >> "$ENV_FILE.tmp"
fi
if [ -n "$USER_VARS" ]; then
	printf '%s\n' "$USER_VARS" >> "$ENV_FILE.tmp"
fi
chmod 600 "$ENV_FILE.tmp"
# Swap atómico: el proceso vivo sigue leyendo el archivo íntegro.
mv -f "$ENV_FILE.tmp" "$ENV_FILE"
# #1816: el init del paquete lee UCI primero y solo cae al env si el token de
# UCI está vacío. Rotar solo el env dejaba el token viejo en
# /etc/config/netpulse-agent y el agente respondía 401 para siempre. Se
# sincronizan también server y slug: si UCI apunta a un servidor antiguo, el
# token nuevo tampoco sirve.
UCI_CFG=/etc/config/netpulse-agent
if [ -f "$UCI_CFG" ] && command -v uci >/dev/null 2>&1; then
	uci -q set netpulse-agent.main.server="$SERVER" || true
	uci -q set netpulse-agent.main.slug="$SLUG" || true
	uci -q set netpulse-agent.main.token=` + token + ` || true
	uci -q commit netpulse-agent || true
fi
# procd rearma solito por el file (procd_set_param file), pero por
# determinismo reiniciamos el servicio explícitamente.
[ -x "$INIT" ] && "$INIT" restart >/dev/null 2>&1 || true
`
}

// Digests devuelve los sha256 de los binarios de agente embebidos (cadena
// vacía si el arch no tiene binario).
func Digests() map[string]string {
	return map[string]string{
		"arm64":  agentbin.Digest("arm64"),
		"arm":    agentbin.Digest("arm"),
		"amd64":  agentbin.Digest("amd64"),
		"mipsle": agentbin.Digest("mipsle"),
		"mips":   agentbin.Digest("mips"),
	}
}
