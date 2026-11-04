#!/bin/bash
# NetPulse — poda de binarios conservados por despliegues manuales.
#
# El updater del backend (deploy/update.sh) NO acumula: guarda el anterior como
# `netpulse.prev` y lo borra en cuanto el healthcheck pasa, y sus snapshots de
# BD los poda prune-update-backups.sh. Los que se acumulan son los que deja un
# despliegue a mano, que conserva el binario anterior con nombre propio
# (`netpulse.prev-<tag>-<fecha>`) para poder volver atrás. Nada los borra, y
# cada uno pesa ~65 MB: dos series de despliegues en un día dejaron 16 ficheros
# y ~1 GB en /opt, con el disco del CT al 85 por ciento.
#
# Este script conserva los KEEP más recientes y borra el resto. No toca:
#   - `netpulse` (el binario en uso),
#   - `netpulse.prev` (el hueco de rollback del updater),
#   - `netpulse.prev-v<version>-*` (la release estable, ancla para volver).
#
# Uso: [SERVER_DIR=/ruta] [KEEP=3] bash prune-prev-binaries.sh
set -e

SERVER_DIR="${SERVER_DIR:-/opt/netpulse/server-go}"
KEEP="${KEEP:-3}"

case "$KEEP" in
  ''|*[!0-9]*) echo "KEEP inválido: $KEEP" >&2; exit 1 ;;
esac
if [ "$KEEP" -lt 1 ]; then
  echo "KEEP debe ser >= 1: $KEEP" >&2
  exit 1
fi
if [ ! -d "$SERVER_DIR" ]; then
  echo "no existe $SERVER_DIR" >&2
  exit 1
fi

# -1: un nombre por línea; -t: por mtime (más nuevo primero). Los nombres ya
# llevan fecha, pero -t es robusto ante relojes movidos.
kept=0
removed=0
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$(basename -- "$f")" in
    netpulse.prev-v[0-9]*) continue ;;  # release estable: no se cuenta
  esac
  kept=$((kept + 1))
  if [ "$kept" -gt "$KEEP" ]; then
    rm -f -- "$f"
    removed=$((removed + 1))
    echo "binario eliminado: $(basename -- "$f")"
  fi
done < <(ls -1dt "$SERVER_DIR"/netpulse.prev-* 2>/dev/null || true)

if [ "$kept" -gt "$KEEP" ]; then
  kept="$KEEP"
fi
echo "binarios de despliegue: $kept conservados, $removed eliminados"
