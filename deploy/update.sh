#!/bin/bash
# NetPulse — script de actualización (lo lanza el updater del backend, corre
# como netpulse). Único flujo soportado: Go.
#   - Go   (/opt/netpulse/server-go/netpulse): git pull → binario precompilado
#          de CI (go-latest) → verificación sha256 → swap atómico → reinicio
#          diferido.
# Progreso (issue #280): cada paso emite STEP:<nombre> y los hitos largos
# PROGRESS:<0-100>; el updater los reenvía al frontend vía SSE.
#
# Safeguards (issue #425):
#   - Backup de .ssh/ y de la BD antes de tocar nada.
#   - Restauración automática de .ssh/ si git lo borró o regeneró.
#   - El binario anterior se conserva como .prev hasta que el post-update
#     healthcheck pase.
#   - Si tras el reinicio no se ven routers, se hace rollback al binario,
#     claves y BD anteriores y se reinicia de nuevo.
set -e

REPO_ROOT="${REPO_ROOT:-/opt/netpulse}"
DATA_DIR="${DATA_DIR:-/opt/netpulse/server/data}"
RESTART_DIR="${RESTART_DIR:-/opt/netpulse/server-go/data}"
SERVER_BIN="${SERVER_BIN:-/opt/netpulse/server-go/netpulse}"
PORT="${PORT:-3000}"
HEALTH_URL="http://127.0.0.1:${PORT}/api/health"

cd "$REPO_ROOT"

SSH_DIR="$DATA_DIR/.ssh"
DB_FILE="$DATA_DIR/netpulse.db"
BACKUP_DIR="$DATA_DIR/.update-backup-$(date +%Y%m%d-%H%M%S)"
PREV_BIN="$SERVER_BIN.prev"

mkdir -p "$BACKUP_DIR"

echo "STEP:backup"
# Respaldo de las claves SSH (si no existe, no hay nada que proteger).
if [ -d "$SSH_DIR" ]; then
  cp -a "$SSH_DIR" "$BACKUP_DIR/"
  echo "copia de seguridad de .ssh en $BACKUP_DIR/.ssh"
fi
# Respaldo consistente de la BD. Preferimos sqlite3 VACUUM INTO; si no está
# disponible, usamos Python; como último recurso cp (a riesgo del admin).
if [ -f "$DB_FILE" ]; then
  if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$DB_FILE" ".backup '$BACKUP_DIR/netpulse.db'" || cp -a "$DB_FILE" "$BACKUP_DIR/netpulse.db"
  elif python3 -c "import sqlite3" >/dev/null 2>&1; then
    python3 - <<PYEOF
import sqlite3, sys
src, dst = "$DB_FILE", "$BACKUP_DIR/netpulse.db"
try:
    c = sqlite3.connect(src)
    c.execute("VACUUM INTO ?", (dst,))
    c.close()
except Exception as e:
    print(e, file=sys.stderr)
    sys.exit(1)
PYEOF
  else
    cp -a "$DB_FILE" "$BACKUP_DIR/netpulse.db"
  fi
  echo "copia de seguridad de BD en $BACKUP_DIR/netpulse.db"
fi

echo "STEP:fetch"
git fetch origin main
git reset --hard origin/main
SHA=$(git rev-parse HEAD)

# Si git reset --hard (o git clean) eliminó .ssh, lo restauramos desde el
# backup. Esto es lo que pasó en issue #425.
if [ -d "$BACKUP_DIR/.ssh" ]; then
  if [ ! -d "$SSH_DIR" ] || [ ! -f "$SSH_DIR/id_ed25519" ] || [ ! -f "$SSH_DIR/id_ed25519.pub" ]; then
    rm -rf "$SSH_DIR"
    cp -a "$BACKUP_DIR/.ssh" "$DATA_DIR/"
    chmod 700 "$SSH_DIR" 2>/dev/null || true
    chmod 600 "$SSH_DIR/id_ed25519" 2>/dev/null || true
    chmod 644 "$SSH_DIR/id_ed25519.pub" "$SSH_DIR/known_hosts" 2>/dev/null || true
    echo "restaurada .ssh desde copia de seguridad"
  fi
fi

if [ -f "$SERVER_BIN" ]; then
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
  curl -fsSL -m 30 "https://api.github.com/repos/gnacho/netpulse/releases/tags/go-latest" -o "$RELEASE_JSON" || rm -f "$RELEASE_JSON"
  # 1) asset exacto del commit; 2) si no existe (commit sin cambios en
  #    server-go/ ni app/), el asset más reciente de esta arquitectura.
  if ! curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/go-latest/$ASSET" -o /tmp/netpulse-go.tgz; then
    ASSET=$(grep -oP '"name":\s*"\Knetpulse-server-[0-9a-f]+-linux-'"$ARCH"'\.tar\.gz' "$RELEASE_JSON" 2>/dev/null | head -1)
    if [ -n "$ASSET" ] && curl -fsSL -m 300 "https://github.com/gnacho/netpulse/releases/download/go-latest/$ASSET" -o /tmp/netpulse-go.tgz; then
      echo "aviso (#471): sin asset para el commit $SHA; instalando el más reciente ($ASSET). El updater seguirá ofreciendo actualización hasta que main publique un asset propio de su SHA."
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
  # Swap atómico: conservamos el anterior como .prev hasta pasar el healthcheck.
  chmod +x /opt/netpulse/server-go/pkg.new/netpulse
  mv "$SERVER_BIN" "$PREV_BIN"
  if ! mv /opt/netpulse/server-go/pkg.new/netpulse "$SERVER_BIN"; then
    # Swap falló: intentar devolver el anterior y abortar.
    mv "$PREV_BIN" "$SERVER_BIN"
    rm -rf /opt/netpulse/server-go/pkg.new /tmp/netpulse-go.tgz "$RELEASE_JSON"
    echo "ERROR: swap del binario falló"
    exit 1
  fi
  rm -rf /opt/netpulse/server-go/pkg.new /tmp/netpulse-go.tgz "$RELEASE_JSON"

  echo "STEP:restart"
  # Marcador de éxito pre-reinicio (#444): el reinicio vía systemd.path mata
  # este script (hijo del servidor) y sin este fichero el updater registra
  # "update_exit_-1" aunque el swap haya ido bien. El updater lo interpreta
  # como el camino de éxito esperado y mantiene el pendingApply (#161).
  echo "$SHA" > "$REPO_ROOT/.update-applied"
  # Reinicio vía unidad systemd.path del CT (netpulse-go-restart.path vigila
  # este flag y ejecuta systemctl restart netpulse-go.service como root).
  # El dir puede no existir (DATA_DIR real vive en /opt/netpulse/server-go/data):
  # crearlo antes de tocar el flag (bug visto 1-Ago-2026: touch fallaba y el
  # binario nuevo quedaba instalado sin reiniciar).
  mkdir -p "$RESTART_DIR"
  touch "$RESTART_DIR/.restart-me"

  echo "STEP:verify"
  # Esperamos a que el servidor vuelva y haya sondeado al menos un router.
  # /api/health es público; devicesTotal > 0 indica que SSH/known_hosts/BD funcionan.
  HEALTHY=0
  DEVICES=0
  for i in $(seq 1 60); do
    HEALTH_JSON=$(curl -fsS --max-time 5 "$HEALTH_URL" 2>/dev/null || true)
    if [ -n "$HEALTH_JSON" ]; then
      DEVICES=$(echo "$HEALTH_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("devicesTotal",0))' 2>/dev/null || echo 0)
      AGENTS=$(echo "$HEALTH_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("agentsConnected",0))' 2>/dev/null || echo 0)
      if [ "$DEVICES" -gt 0 ] || [ "$AGENTS" -gt 0 ]; then
        HEALTHY=1
        break
      fi
    fi
    sleep 2
  done

  if [ "$HEALTHY" -eq 0 ]; then
    echo "ERROR: el servidor no respondió o no ve routers/agentes tras el reinicio (devices=$DEVICES)"
    DO_ROLLBACK=1
  else
    echo "healthcheck OK: devices=$DEVICES agents=$AGENTS"
  fi

  if [ "${DO_ROLLBACK:-0}" -eq 1 ]; then
    if [ -f "$PREV_BIN" ]; then
      mv "$PREV_BIN" "$SERVER_BIN"
    fi
    if [ -d "$BACKUP_DIR/.ssh" ]; then
      rm -rf "$SSH_DIR"
      cp -a "$BACKUP_DIR/.ssh" "$DATA_DIR/"
      chmod 700 "$SSH_DIR" 2>/dev/null || true
      chmod 600 "$SSH_DIR/id_ed25519" 2>/dev/null || true
      chmod 644 "$SSH_DIR/id_ed25519.pub" "$SSH_DIR/known_hosts" 2>/dev/null || true
    fi
    if [ -f "$BACKUP_DIR/netpulse.db" ]; then
      cp -a "$BACKUP_DIR/netpulse.db" "$DB_FILE"
    fi
    # Forzamos un nuevo reinicio para levantar con la versión anterior.
    touch "$RESTART_DIR/.restart-me"
    echo "ERROR: rollback completado; versión anterior restaurada"
    exit 1
  fi

  # Todo OK: ya podemos descartar el binario anterior.
  rm -f "$PREV_BIN"
fi

echo "STEP:done"
