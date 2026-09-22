#!/bin/bash
# NetPulse — poda de snapshots del updater (issue #809).
#
# Cada apply del updater (deploy/update.sh) deja una copia completa de la BD
# en $DATA_DIR/.update-backup-<timestamp>/ antes de tocar nada. Sin límite,
# esos snapshots crecen sin control: en una instancia viva se observaron 18
# directorios / ~2,2 GB en ~2 semanas (una copia por update diario, más los
# reintentos). Este script conserva los KEEP más recientes y borra el resto.
#
# Solo debe llamarse DESPUÉS de un update correcto: durante un rollback el
# snapshot de la ejecución en curso es la red de seguridad.
#
# Uso: DATA_DIR=/ruta [KEEP=3] bash prune-update-backups.sh
set -e

DATA_DIR="${DATA_DIR:-/opt/netpulse/server/data}"
KEEP="${KEEP:-3}"

case "$KEEP" in
  ''|*[!0-9]*) echo "KEEP inválido: $KEEP" >&2; exit 1 ;;
esac
if [ "$KEEP" -lt 1 ]; then
  echo "KEEP debe ser >= 1: $KEEP" >&2
  exit 1
fi

# -1: un nombre por línea; -d: directorios; -t: por mtime (más nuevo primero).
# Los nombres ya llevan timestamp (orden coincidente), pero -t es robusto ante
# relojes movidos. Si no hay coincidencias, ls falla y se ignora.
kept=0
removed=0
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  kept=$((kept + 1))
  if [ "$kept" -gt "$KEEP" ]; then
    rm -rf -- "$dir"
    removed=$((removed + 1))
    echo "snapshot eliminado: $(basename -- "$dir")"
  fi
done < <(ls -1dt "$DATA_DIR"/.update-backup-* 2>/dev/null || true)

if [ "$kept" -gt "$KEEP" ]; then
  kept="$KEEP"
fi
echo "snapshots del updater: $kept conservados, $removed eliminados"
