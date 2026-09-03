// Package reinstall: construcción del script de instalación del agente
// netpulse-agent en un router OpenWrt (#246, #457, #463). Lo comparten el
// handler manual (POST /api/agents/{slug}/reinstall) y el supervisor del
// rearmer (escalado restart → reinstall), de ahí su propio paquete: httpapi
// importa rearmer y ninguno de los dos puede importar al otro.
package reinstall

import "github.com/gnacho/netpulse/server-go/internal/agentbin"

// Script construye el POSIX sh que se ejecuta en el router: instala el
// agente completo (binario verificado por sha256, config .env, init procd
// con self-heal y watchdog con su cron) de forma idempotente.
// digests mapa arch→sha256 del binario embebido; un arch sin digest queda
// sin verificación (build dev) en lugar de bloquear.
func Script(slug, token, serverURL string, digests map[string]string) string {
	return `#!/bin/sh
set -e
INIT=/etc/init.d/netpulse-agent
BIN=/usr/sbin/netpulse-agent
ENV_FILE=/etc/netpulse-agent.env
WATCHDOG=/usr/sbin/netpulse-watchdog
SERVER="` + serverURL + `"
SLUG="` + slug + `"
TOKEN="` + token + `"

# Detectar arquitectura del router y el digest esperado del binario embebido
ARCH=$(uname -m)
case "$ARCH" in
	aarch64|arm64)  GOARCH=arm64; SHA256="` + digests["arm64"] + `" ;;
	armv7l|armv7|armhf|arm) GOARCH=arm; SHA256="` + digests["arm"] + `" ;;
	x86_64|amd64)   GOARCH=amd64; SHA256="` + digests["amd64"] + `" ;;
	*) echo "arch no soportado: $ARCH"; exit 20 ;;
esac

# Parar servicio previo si existe (el proceso vivo mantiene el binario abierto)
[ -f "$INIT" ] && "$INIT" stop >/dev/null 2>&1 || true

# Descargar el binario del propio server (auth por token)
if command -v curl >/dev/null 2>&1; then
	curl -fsSL --connect-timeout 10 -m 600 -H "Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH" -o /tmp/netpulse-agent.new
else
	wget -q -T 60 -O /tmp/netpulse-agent.new --header="Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH"
fi

# Verificación sha256 contra el digest embebido (#463); vacío = sin verificar
if [ -n "$SHA256" ]; then
	GOT=$(sha256sum /tmp/netpulse-agent.new | awk '{print $1}')
	[ "$GOT" = "$SHA256" ] || { echo "sha256 no coincide (esperado $SHA256, obtenido $GOT)"; exit 21; }
fi
chmod 0755 /tmp/netpulse-agent.new
mv -f /tmp/netpulse-agent.new "$BIN"

# Config (chmod 600)
cat > "$ENV_FILE" <<EOF
# netpulse-agent — config (generado por reinstall)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=$TOKEN
EOF
chmod 600 "$ENV_FILE"

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
		*) return 1 ;;
	esac
	local url tmp
	url="${NETPULSE_SERVER%/}/api/agents/${NETPULSE_SLUG}/binary?arch=${ARCH}"
	logger -t netpulse-agent "self-heal: binario ausente, descargando de $NETPULSE_SERVER"
	tmp=/tmp/netpulse-agent.$$
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -m 120 -H "Authorization: Bearer $NETPULSE_TOKEN" -o "$tmp" "$url" || return 1
	else
		wget -q -T 60 -O "$tmp" --header="Authorization: Bearer $NETPULSE_TOKEN" "$url" || return 1
	fi
	chmod 0755 "$tmp" && mv "$tmp" /usr/sbin/netpulse-agent || { rm -f "$tmp"; return 1; }
	logger -t netpulse-agent "self-heal: binario restaurado"
	BIN=/usr/sbin/netpulse-agent
	return 0
}

start_service() {
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

# Watchdog + cron (mismo criterio que el paquete; detección por pidof,
# compatible con BusyBox)
cat > "$WATCHDOG" <<'WGEOF'
#!/bin/sh
# netpulse-watchdog — relanza el agente si el proceso murió o el heartbeat
# lleva >300s sin latir. Cron cada 2 min.
INIT=/etc/init.d/netpulse-agent
HB=/tmp/netpulse-agent.heartbeat
[ -x "$INIT" ] || exit 0
if ! pidof netpulse-agent >/dev/null 2>&1; then
	logger -t netpulse-watchdog "agente no está en marcha, reiniciando servicio"
	"$INIT" restart >/dev/null 2>&1
	exit 0
fi
if [ -f "$HB" ]; then
	now=$(date +%s)
	hb=$(cat "$HB" 2>/dev/null)
	case "$hb" in ''|*[!0-9]*) exit 0 ;; esac
	age=$((now - hb))
	if [ "$age" -gt 300 ]; then
		logger -t netpulse-watchdog "proceso vivo pero sin latido en ${age}s, reiniciando servicio"
		"$INIT" restart >/dev/null 2>&1
	fi
fi
exit 0
WGEOF
chmod 0755 "$WATCHDOG"
( crontab -l 2>/dev/null | grep -v netpulse-watchdog ; echo '*/2 * * * * /usr/sbin/netpulse-watchdog' ) | crontab -
/etc/init.d/cron restart >/dev/null 2>&1 || true

"$INIT" enable
"$INIT" restart
`
}

// Digests devuelve los sha256 de los binarios de agente embebidos para las
// tres arquitecturas (cadena vacía si el arch no tiene binario).
func Digests() map[string]string {
	return map[string]string{
		"arm64": agentbin.Digest("arm64"),
		"arm":   agentbin.Digest("arm"),
		"amd64": agentbin.Digest("amd64"),
	}
}
