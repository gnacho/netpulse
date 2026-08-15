#!/bin/bash
# NetPulse — script de actualización (lo lanza el updater del backend, corre
# como netpulse). Único flujo soportado: Go.
#   - Go   (/opt/netpulse/server-go/netpulse): git pull → binario precompilado
#          de CI (go-latest) → swap atómico → reinicio diferido.
# El backend Node legado fue eliminado; solo existe el flujo Go (decisión
# 5-Ago-2026).
set -e
cd /opt/netpulse

echo "STEP:fetch"
git fetch origin main
git reset --hard origin/main
SHA=$(git rev-parse HEAD)

if [ -f /opt/netpulse/server-go/netpulse ]; then
  # ---------------- Flujo Go ----------------
  echo "STEP:binary"
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) echo "arquitectura no soportada: $ARCH"; exit 1 ;;
  esac
  URL="https://github.com/gnacho/netpulse/releases/download/go-latest/netpulse-server-$SHA-linux-$ARCH.tar.gz"
  rm -rf /opt/netpulse/server-go/pkg.new
  mkdir -p /opt/netpulse/server-go/pkg.new
  # 1) asset exacto del commit; 2) si no existe (commit sin cambios en
  #    server-go/ ni app/), el asset más reciente de esta arquitectura.
  if curl -fsSL -m 300 "$URL" -o /tmp/netpulse-go.tgz && tar xzf /tmp/netpulse-go.tgz -C /opt/netpulse/server-go/pkg.new; then
    echo "binario precompilado descargado de CI ($SHA)"
  else
    ASSET=$(curl -fsSL -m 30 https://api.github.com/repos/gnacho/netpulse/releases/tags/go-latest \
      | grep -oP '"name":\s*"\Knetpulse-server-[0-9a-f]+-linux-'"$ARCH"'\.tar\.gz' | head -1)
    if [ -n "$ASSET" ] && curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/go-latest/$ASSET" -o /tmp/netpulse-go.tgz \
      && tar xzf /tmp/netpulse-go.tgz -C /opt/netpulse/server-go/pkg.new; then
      echo "binario de CI más reciente disponible ($ASSET)"
    else
      echo "ERROR: sin binario en CI para $ARCH (el CT no puede compilar Go)"
      exit 1
    fi
  fi
  # Swap atómico del binario: si algo falló antes, el anterior queda intacto
  chmod +x /opt/netpulse/server-go/pkg.new/netpulse
  mv /opt/netpulse/server-go/netpulse /opt/netpulse/server-go/netpulse.old
  mv /opt/netpulse/server-go/pkg.new/netpulse /opt/netpulse/server-go/netpulse
  rm -rf /opt/netpulse/server-go/pkg.new /opt/netpulse/server-go/netpulse.old /tmp/netpulse-go.tgz

  echo "STEP:restart"
  # Reinicio vía unidad systemd.path del CT (netpulse-go-restart.path vigila
  # este flag y ejecuta systemctl restart netpulse-go.service como root).
  # El dir puede no existir (DATA_DIR real vive en /opt/netpulse/server-go/data):
  # crearlo antes de tocar el flag (bug visto 1-Ago-2026: touch fallaba y el
  # binario nuevo quedaba instalado sin reiniciar).
  mkdir -p /opt/netpulse/server-go/data
  touch /opt/netpulse/server-go/data/.restart-me
fi

echo "STEP:done"
