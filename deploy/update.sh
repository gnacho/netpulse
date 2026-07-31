#!/bin/bash
# NetPulse — script de actualización (lo lanza el servicio, corre como netpulse)
# git pull → deps server si cambia el lock → build frontend → reinicio diferido.
set -e
cd /opt/netpulse

echo "STEP:fetch"
git fetch origin main
git reset --hard origin/main

echo "STEP:server-deps"
if git diff --name-only 'HEAD@{1}' HEAD 2>/dev/null | grep -q 'server/package-lock.json'; then
  cd /opt/netpulse/server
  npm ci --omit=dev --no-audit --no-fund
fi

echo "STEP:frontend-build"
cd /opt/netpulse
SHA=$(git rev-parse HEAD)
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

echo "STEP:done"
