#!/bin/bash
# switch-scraper.sh — pusher externo de NetPulse para switches gestionados
# sin SSH (Keephome KP-9000 y clones con web UI mac.cgi/port.cgi).
#
# Extrae la tabla MAC y el estado de bocas de la web del switch y los
# empuja a POST /api/ingest/agent como pusher EXTERNO (kind=external,
# #285/#288): sin agente instalable, con cadencia declarada (interval)
# para que el servidor escale su ventana de frescura.
#
# Configuración: fichero env (default /opt/netpulse/switch-scraper.env,
# permisos 600; ver switch-scraper.env.example). El timer systemd lanza
# este script cada 5 min; su OnUnitActiveSec debe casar con INTERVAL.
#
# Instalación (host de monitorización):
#   install -m 0755 switch-scraper.sh /opt/netpulse/switch-scraper.sh
#   install -m 0600 switch-scraper.env /opt/netpulse/switch-scraper.env  # editado
#   # timer: netpulse-switch-scraper.{service,timer} (OnUnitActiveSec=5min)
set -euo pipefail

ENV_FILE="${SWITCH_SCRAPER_ENV:-/opt/netpulse/switch-scraper.env}"
if [ ! -r "$ENV_FILE" ]; then
    echo "[switch-scraper] ERROR: no se puede leer $ENV_FILE" >&2
    exit 2
fi
# shellcheck source=/dev/null
. "$ENV_FILE"

: "${SWITCH_HOST:?SWITCH_HOST no definido en $ENV_FILE}"
: "${SWITCH_USER:?SWITCH_USER no definido en $ENV_FILE}"
: "${SWITCH_PASS:?SWITCH_PASS no definido en $ENV_FILE}"
: "${AGENT_SLUG:?AGENT_SLUG no definido en $ENV_FILE}"
: "${NETPULSE_URL:=http://127.0.0.1:3000}"
: "${INTERVAL:=300}"
TOKEN_FILE="${TOKEN_FILE:-/opt/netpulse/${AGENT_SLUG}.token}"

TOKEN="$(cat "$TOKEN_FILE")"
if [ -z "$TOKEN" ]; then
    echo "[$AGENT_SLUG] ERROR: token vacío en $TOKEN_FILE" >&2
    exit 2
fi

PASS_MD5="$(echo -n "${SWITCH_USER}${SWITCH_PASS}" | md5sum | awk '{print $1}')"

# --- Scrapear MAC table ---
HTML_MAC="$(curl -sf -m 10 -b "admin=$PASS_MD5" -H "Referer: http://${SWITCH_HOST}/" \
    "http://${SWITCH_HOST}/mac.cgi?page=fwd_tbl" 2>/dev/null)" || {
    echo "[$AGENT_SLUG] ERROR: no se pudo obtener la MAC table del switch" >&2
    exit 2
}

# Parsear: cada fila <tr>...</tr> tiene 4 <td>: No., MAC, VLAN, Type, Port
# Extraer bloques de fila y sacar MAC (campo 2) + Port (campo 5)
MACS_JSON="{}"
if command -v python3 >/dev/null 2>&1; then
    MACS_JSON=$(python3 -c "
import re, sys, json
html = sys.stdin.read()
rows = re.findall(r'<tr>\s*(.*?)\s*</tr>', html, re.S)
macs = {}
for row in rows:
    cells = re.findall(r'<td[^>]*>(.*?)</td>', row)
    if len(cells) >= 5:
        mac = cells[1].strip()
        port = cells[4].strip()
        if re.match(r'^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$', mac) and port.isdigit():
            macs[mac.upper()] = port
print(json.dumps(macs))
" <<< "$HTML_MAC")
fi

# --- Scrapear port statistics ---
HTML_PORTS="$(curl -sf -m 10 -b "admin=$PASS_MD5" -H "Referer: http://${SWITCH_HOST}/" \
    "http://${SWITCH_HOST}/port.cgi?page=stats" 2>/dev/null)" || {
    echo "[$AGENT_SLUG] ERROR: no se pudo obtener port statistics del switch" >&2
    exit 2
}

PORTS_JSON="[]"
if command -v python3 >/dev/null 2>&1; then
    PORTS_JSON=$(python3 -c "
import re, sys, json
html = sys.stdin.read()
ports = []
# Patrones: <td>Port N</td> <td>Enable</td> <td>Link Up/Down</td>
for m in re.finditer(r'<td>Port\s*(\d+)</td>\s*<td>(\w+)</td>\s*<td>(Link\s+\w+)</td>', html):
    ports.append({'id': f'lan{m.group(1)}', 'label': f'Port {m.group(1)}', 'up': m.group(3) == 'Link Up'})
print(json.dumps(ports))
" <<< "$HTML_PORTS")
fi

# --- Construir payload y push (pusher externo: kind + interval, #288) ---
TS="$(date +%s)"
PAYLOAD="{\"router\":\"${AGENT_SLUG}\",\"ts\":${TS},\"version\":\"scraper-2.0\",\"kind\":\"external\",\"interval\":${INTERVAL},\"data\":{\"fdb\":{\"macs\":${MACS_JSON},\"ports\":${PORTS_JSON}}}}"

SIG=$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac "$TOKEN" 2>/dev/null | awk '{print $NF}')

HTTP_CODE=$(curl -s -o /tmp/${AGENT_SLUG}-response -w '%{http_code}' -m 10 -X POST "${NETPULSE_URL}/api/ingest/agent" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "X-Agent-Signature: ${SIG}" \
    -d "$PAYLOAD" 2>/dev/null)

if [ "$HTTP_CODE" = "202" ]; then
    MAC_COUNT=$(echo "$MACS_JSON" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "?")
    echo "[$AGENT_SLUG] OK: ${MAC_COUNT} MACs, push 202" >&2
else
    RESP=$(cat /tmp/${AGENT_SLUG}-response 2>/dev/null)
    echo "[$AGENT_SLUG] ERROR: push devolvió ${HTTP_CODE}: ${RESP}" >&2
    exit 2
fi
