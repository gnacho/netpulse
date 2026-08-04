#!/bin/sh
# =============================================================================
# netpulse-watchdog — Fase 5 (Plan A): auto-supervisión del agente desde el
# propio router. Se ejecuta desde cron cada 2 minutos:
#
#   1. Proceso muerto → /etc/init.d/netpulse-agent restart
#      (reinicia la instancia procd y RESETA su contador de reintentos, que
#      es el hueco que deja al agente caído para siempre tras 5 crashes).
#   2. Proceso vivo PERO heartbeat viejo (> MAX_AGE s) → restart
#      ("vivo pero roto": el proceso anda pero no consigue empujar datos
#      confirmados por el servidor).
#   3. Proceso vivo sin fichero de heartbeat → NO hacer nada (arranque
#      reciente o backoff inicial con el servidor caído; reiniciar solo
#      resetearía el backoff sin aportar nada).
#
# El heartbeat lo escribe el agente (agent/internal/heartbeat) tras cada push
# CONFIRMADO: /tmp/netpulse-agent.heartbeat (tmpfs, nada en NAND).
#
# Si el binario no existe (variante /tmp tras un reboot) no hace nada: la
# reinstalación es cosa del servidor (POST /api/agents/{slug}/rearm en el
# futuro, hoy install-agent.sh).
# =============================================================================
INIT=/etc/init.d/netpulse-agent
HB=/tmp/netpulse-agent.heartbeat
MAX_AGE=300 # 5 min sin latido = "vivo pero roto"

# El env puede mover el heartbeat (NETPULSE_HEARTBEAT_FILE)
[ -f /etc/netpulse-agent.env ] && . /etc/netpulse-agent.env 2>/dev/null
[ -n "${NETPULSE_HEARTBEAT_FILE:-}" ] && HB="$NETPULSE_HEARTBEAT_FILE"

log() { logger -t netpulse-watchdog "$*"; }

# ¿Hay binario? Sin él no hay nada que vigilar.
if [ ! -x /usr/sbin/netpulse-agent ] && [ ! -x /tmp/netpulse-agent ]; then
    exit 0
fi

if ! pgrep -x netpulse-agent >/dev/null 2>&1; then
    log "agente no está en marcha — reiniciando servicio"
    $INIT restart >/dev/null 2>&1
    exit 0
fi

if [ -f "$HB" ]; then
    now=$(date +%s)
    hb=$(cat "$HB" 2>/dev/null)
    case "$hb" in
        '' | *[!0-9]*) exit 0 ;; # fichero corrupto: no actuar a ciegas
    esac
    age=$((now - hb))
    if [ "$age" -gt "$MAX_AGE" ]; then
        log "proceso vivo pero sin latido en ${age}s — reiniciando servicio"
        $INIT restart >/dev/null 2>&1
    fi
fi
exit 0
