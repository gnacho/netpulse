#!/bin/bash
# NetPulse — script de actualización (lo lanza el updater del backend, corre
# como netpulse). Único flujo soportado: Go.
#   - Go   (/opt/netpulse/server-go/netpulse): git pull → binario precompilado
#          de CI (go-latest) → verificación sha256 → swap atómico → reinicio
#          diferido.
# Progreso (issue #280): cada paso emite STEP:<nombre> y los hitos largos
# PROGRESS:<0-100>; el updater los reenvía al frontend vía SSE.
set -e
cd /opt/netpulse

echo "STEP:fetch"
git fetch origin main
git reset --hard origin/main
SHA=$(git rev-parse HEAD)

if [ -f /opt/netpulse/server-go/netpulse ]; then
  # ---------------- Flujo Go ----------------
  echo "STEP:download"
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) echo "arquitectura no soportada: $ARCH"; exit 1 ;;
  esac
  ASSET="netpulse-server-$SHA-linux-$ARCH.tar.gz"
  rm -rf /opt/netpulse/server-go/pkg.new
  mkdir -p /opt/netpulse/server-go/pkg.new
  # Metadatos de la prerelease go-latest: fallback de asset + digest sha256
  # para el paso verify. Si la llamada falla, el flujo sigue (fallback solo
  # con el asset exacto del commit).
  RELEASE_JSON=/tmp/netpulse-go-release.json
  curl -fsSL -m 30 https://api.github.com/repos/gnacho/netpulse/releases/tags/go-latest -o "$RELEASE_JSON" || rm -f "$RELEASE_JSON"
  # 1) asset exacto del commit; 2) si no existe (commit sin cambios en
  #    server-go/ ni app/), el asset más reciente de esta arquitectura.
  if ! curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/go-latest/$ASSET" -o /tmp/netpulse-go.tgz; then
    ASSET=$(grep -oP '"name":\s*"\Knetpulse-server-[0-9a-f]+-linux-'"$ARCH"'\.tar\.gz' "$RELEASE_JSON" 2>/dev/null | head -1)
    if [ -n "$ASSET" ] && curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/go-latest/$ASSET" -o /tmp/netpulse-go.tgz; then
      echo "binario de CI más reciente disponible ($ASSET)"
    else
      echo "ERROR: sin binario en CI para $ARCH (el CT no puede compilar Go)"
      exit 1
    fi
  fi
  echo "PROGRESS:55"

  echo "STEP:verify"
  # Digest sha256 del asset publicado por la API de GitHub. Si el campo no
  # viene (release sin digest), se avisa y continúa; si viene y NO coincide,
  # aborta (el tarball no es el publicado por CI).
  DIGEST=""
  if [ -s "$RELEASE_JSON" ]; then
    DIGEST=$(awk -v want="\"$ASSET\"" '$0 ~ "\"name\": *" want {f=1} f && /"digest"/ {print; exit}' "$RELEASE_JSON" | grep -oE 'sha256:[0-9a-f]+')
  fi
  if [ -n "$DIGEST" ]; then
    echo "verificando $ASSET contra $DIGEST"
    echo "${DIGEST#sha256:}  /tmp/netpulse-go.tgz" | sha256sum -c - || { echo "ERROR: sha256 del asset no coincide"; exit 1; }
  else
    echo "aviso: la API no publicó digest para $ASSET; verificación omitida"
  fi
  tar xzf /tmp/netpulse-go.tgz -C /opt/netpulse/server-go/pkg.new
  echo "PROGRESS:75"

  echo "STEP:install"
  # Swap atómico del binario: si algo falló antes, el anterior queda intacto
  chmod +x /opt/netpulse/server-go/pkg.new/netpulse
  mv /opt/netpulse/server-go/netpulse /opt/netpulse/server-go/netpulse.old
  mv /opt/netpulse/server-go/pkg.new/netpulse /opt/netpulse/server-go/netpulse
  rm -rf /opt/netpulse/server-go/pkg.new /opt/netpulse/server-go/netpulse.old /tmp/netpulse-go.tgz "$RELEASE_JSON"

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
