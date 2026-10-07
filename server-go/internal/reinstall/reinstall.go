// Package reinstall: construcción del script de instalación del agente
// netpulse-agent en un router OpenWrt (#246, #457, #463). Lo comparten el
// handler manual (POST /api/agents/{slug}/reinstall) y el supervisor del
// rearmer (escalado restart → reinstall), de ahí su propio paquete: httpapi
// importa rearmer y ninguno de los dos puede importar al otro.
package reinstall

import (
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
			return WanTargets{WanTarget: "1.1.1.1"}
		}
		if gwIP != "" {
			return WanTargets{GwTarget: gwIP}
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
func envRewriteBlock(targets WanTargets) string {
	return `USER_VARS=""
if [ -f "$ENV_FILE" ]; then
	USER_VARS=$(grep -E '^NETPULSE_[A-Z0-9_]+=' "$ENV_FILE" | grep -vE '^NETPULSE_(SERVER|SLUG|TOKEN|SERVER_FP|PAIRING_TOKEN|WAN_TARGET|GW_TARGET)=') || true
fi
cat > "$ENV_FILE" <<EOF
# netpulse-agent — config (generado por reinstall)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=$TOKEN
# NETPULSE_SERVER_FP=<sha256>    # pin SPKI del server (https); lo rellena el instalador
# NETPULSE_INTERVAL=30           # segundos entre pushes
# NETPULSE_SCAN_INTERVAL=30m     # min entre scans de vecinos; "0" = sin scans periódicos
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
	echo "NETPULSE_SERVER_FP=$SERVER_FP" >> "$ENV_FILE"
fi
if [ -n "$USER_VARS" ]; then
	printf '%s\n' "$USER_VARS" >> "$ENV_FILE"
fi
chmod 600 "$ENV_FILE"
`
}

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
` + caSetupBlock(tr) + envRewriteBlock(targets) + `
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
			*) echo "no pude detectar el endianness de $ARCH"; exit 20 ;;
		esac ;;
	*) echo "arch no soportado: $ARCH"; exit 20 ;;
esac

# Parar servicio previo si existe (el proceso vivo mantiene el binario abierto)
[ -f "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true

# Descargar el binario del propio server (auth por token)
if command -v curl >/dev/null 2>&1; then
	curl -fsSL $CURL_TLS --connect-timeout 10 -m 600 -H "Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH" -o /tmp/netpulse-agent.new
else
	wget -q $WGET_TLS -T 60 -O /tmp/netpulse-agent.new --header="Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH"
fi

# Verificación sha256 contra el digest embebido (#463); vacío = sin verificar
if [ -n "$SHA256" ]; then
	GOT=$(sha256sum /tmp/netpulse-agent.new | awk '{print $1}')
	[ "$GOT" = "$SHA256" ] || { echo "sha256 no coincide (esperado $SHA256, obtenido $GOT)"; exit 21; }
fi
chmod 0755 /tmp/netpulse-agent.new
mv -f /tmp/netpulse-agent.new "$BIN"

# Config (chmod 600). El rewrite toca SOLO las vars gestionadas (SERVER,
# SLUG, TOKEN, SERVER_FP, WAN_TARGET, GW_TARGET): las NETPULSE_* del usuario
# (p. ej. NETPULSE_SCAN_INTERVAL=0) se conservan tal cual (#851).
` + envRewriteBlock(targets) + `
# Init procd con self-heal (#457): un sysupgrade solo conserva /etc, así que
# si el binario falta al arrancar se descarga del server con este env.
cat > "$INIT" <<'INITEOF'
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
	local url tmp
	url="${NETPULSE_SERVER%/}/api/agents/${NETPULSE_SLUG}/binary?arch=${ARCH}"
	logger -t netpulse-agent "self-heal: binario ausente, descargando de $NETPULSE_SERVER"
	tmp=/tmp/netpulse-agent.$$
	local ctls="" wtls=""
	if [ -f /etc/netpulse-ca.pem ]; then ctls="--cacert /etc/netpulse-ca.pem"; wtls="--ca-certificate=/etc/netpulse-ca.pem"; fi
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL $ctls -m 120 -H "Authorization: Bearer $NETPULSE_TOKEN" -o "$tmp" "$url" || return 1
	else
		wget -q $wtls -T 60 -O "$tmp" --header="Authorization: Bearer $NETPULSE_TOKEN" "$url" || return 1
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
	
	start_service() {
	cleanup_stale_watchdog_cron
	selfheal_binary || logger -t netpulse-agent "self-heal: no se pudo restaurar el binario"
	procd_open_instance netpulse-agent
	procd_set_param command "$BIN"
	procd_set_param respawn "${respawn_threshold:-3600}" "${respawn_timeout:-5}" "${respawn_retry:-5}"
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_set_param file /etc/netpulse-agent.env
	procd_close_instance
}
INITEOF
chmod 0755 "$INIT"

# #851: el watchdog cron YA NO se instala (un reinicio a ciegas de un crash
# loop no arregla nada y ensucia el log; la detección es el dead-man del
# servidor). Si el router viene de una instalación que sí lo tenía, limpiarlo.
rm -f /usr/sbin/netpulse-watchdog
if [ -f /etc/crontabs/root ]; then
	sed -i '/netpulse-watchdog/d' /etc/crontabs/root
fi

"$INIT" enable
"$INIT" restart
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
[ -f "$ENV_FILE" ] || { echo "netpulse-agent.env no existe; token no actualizado"; exit 30; }
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
[ -n "$SLUG" ] || { echo "netpulse-agent.env sin NETPULSE_SLUG"; exit 31; }
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
