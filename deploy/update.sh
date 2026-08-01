#!/bin/bash
# NetPulse — script de actualización (lo lanza el updater del backend, corre
# como netpulse). Flujo dual según el backend instalado:
#   - Go   (/opt/netpulse/server-go/netpulse existe): git pull → binario
#          precompilado de CI (go-latest) → swap atómico → reinicio diferido.
#   - Node (legacy): git pull → deps → dist precompilado (dist-latest) →
#          swap atómico del dist → reinicio diferido.
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
  # Reinicio vía unidad systemd.path del CT (netpulse-restart.path vigila este
  # flag y ejecuta systemctl restart netpulse-go.service como root)
  touch /opt/netpulse/server-go/data/.restart-me
else
  # ---------------- Flujo Node (legacy) ----------------
  echo "STEP:server-deps"
  if git diff --name-only 'HEAD@{1}' HEAD 2>/dev/null | grep -q 'server/package-lock.json'; then
    cd /opt/netpulse/server
    npm ci --omit=dev --no-audit --no-fund
  fi

  echo "STEP:frontend-build"
  cd /opt/netpulse
  URL="https://github.com/gnacho/netpulse/releases/download/dist-latest/app-dist-$SHA.tar.gz"
  rm -rf /opt/netpulse/app/dist.new
  mkdir -p /opt/netpulse/app/dist.new
  # Vía preferida: dist precompilado por CI (el CT no tiene RAM para compilar).
  # 1) asset exacto del commit; 2) si no existe (commit sin cambios en app/),
  #    el asset más reciente disponible (el dist no ha cambiado entonces).
  if curl -fsSL -m 300 "$URL" -o /tmp/app-dist.tgz && tar xzf /tmp/app-dist.tgz -C /opt/netpulse/app/dist.new; then
    echo "dist precompilado descargado de CI ($SHA)"
  else
    ASSET=$(curl -fsSL -m 30 https://api.github.com/repos/gnacho/netpulse/releases/tags/dist-latest \
      | grep -oP '"name":\s*"\Kapp-dist-[0-9a-f]+\.tar\.gz' | head -1)
    if [ -n "$ASSET" ] && curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/dist-latest/$ASSET" -o /tmp/app-dist.tgz \
      && tar xzf /tmp/app-dist.tgz -C /opt/netpulse/app/dist.new; then
      echo "dist de CI más reciente disponible ($ASSET)"
    else
      echo "sin dist en CI; build local (riesgo OOM en 512MB)"
      cd /opt/netpulse/app
      # npm ci SOLO si cambió el lockfile (656 paquetes: OOM seguro en el CT)
      if git diff --name-only 'HEAD@{1}' HEAD 2>/dev/null | grep -q 'app/package-lock.json'; then
        npm ci --no-audit --no-fund
      fi
      npm run build -- --outDir dist.new
    fi
  fi
  # Swap atómico: si algo falló antes, el dist anterior queda intacto
  rm -rf /opt/netpulse/app/dist.old
  [ -d /opt/netpulse/app/dist ] && mv /opt/netpulse/app/dist /opt/netpulse/app/dist.old
  mv /opt/netpulse/app/dist.new /opt/netpulse/app/dist
  rm -rf /opt/netpulse/app/dist.old /tmp/app-dist.tgz

  echo "STEP:restart"
  # Reinicio vía unidad systemd.path del CT (netpulse-restart.path vigila este
  # flag y ejecuta systemctl restart netpulse.service como root)
  touch /opt/netpulse/server/data/.restart-me
fi

echo "STEP:done"
