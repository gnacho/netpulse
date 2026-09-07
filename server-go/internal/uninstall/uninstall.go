// Package uninstall: construcción del script que desinstala el agente
// netpulse-agent de un router OpenWrt (#624). Es la operación inversa al
// paquete reinstall: en lugar de instalar binario + env + init procd
// (con self-heal y watchdog), se detiene y deshabilita el servicio, y se
// eliminan todos los artefactos que reinstall deja en el router.
//
// Vive en su propio paquete (como reinstall) porque httpapi importa
// rearmer/reinstall y ninguno de esos paquetes puede importar a httpapi.
package uninstall

// Script construye el POSIX sh que se ejecuta en el router para quitar el
// agente. Es idempotente: si el agente ya no está, no falla.
//
// Rutas canónicas (las mismas que reinstall.Script):
//   - INIT=/etc/init.d/netpulse-agent
//   - BIN=/usr/sbin/netpulse-agent
//   - ENV_FILE=/etc/netpulse-agent.env
//   - WATCHDOG=/usr/sbin/netpulse-watchdog
//   - cron */2 con netpulse-watchdog (desmontado por reinstall)
func Script() string {
	return `#!/bin/sh
# NetPulse agent uninstall (#624) — idempotente.

INIT=/etc/init.d/netpulse-agent
BIN=/usr/sbin/netpulse-agent
ENV_FILE=/etc/netpulse-agent.env
WATCHDOG=/usr/sbin/netpulse-watchdog

# 1. Detener (si el init existe) y deshabilitar.
if [ -f "$INIT" ]; then
  "$INIT" stop >/dev/null 2>&1 || true
  "$INIT" disable >/dev/null 2>&1 || true
  rm -f "$INIT"
fi

# 2. Matar cualquier proceso que quedara (self-heal puede relanzarlo).
if pidof netpulse-agent >/dev/null 2>&1; then
  killall netpulse-agent >/dev/null 2>&1 || true
fi

# 3. Borrar binario, watchdog, env y artefactos temporales.
rm -f "$BIN"
rm -f "$WATCHDOG"
rm -f "$ENV_FILE"
rm -f /tmp/netpulse-agent /tmp/netpulse-agent.new /tmp/netpulse-agent.$$ /tmp/netpulse-agent.heartbeat

# 4. Quitar la línea de cron del watchdog.
( crontab -l 2>/dev/null | grep -v netpulse-watchdog ) | crontab - 2>/dev/null || true
/etc/init.d/cron restart >/dev/null 2>&1 || true

# 5. Señal de éxito (el server solo comprueba exit 0).
exit 0
`
}
