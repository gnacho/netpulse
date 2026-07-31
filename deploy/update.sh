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
cd /opt/netpulse/app
npm ci --no-audit --no-fund
npm run build

echo "STEP:restart"
# Reinicio diferido en proceso desacoplado (este script muere con el servicio)
setsid sh -c 'sleep 3; sudo -n systemctl restart netpulse.service' >/dev/null 2>&1 &

echo "STEP:done"
