#!/bin/sh
# Publish the SPA build of the latest stable NetPulse release as the live demo.
#
# Runs from netpulse-demo-publish.timer on the demo host. It needs no
# credentials: it reads public release metadata and assets from the GitHub API,
# so CI never holds a key for this machine.
#
# Layout under /opt/netpulse-demo:
#   releases/<tag>/  unpacked app/dist of each published release
#   public           symlink to releases/<tag>, nginx serves this path
#   .published-tag   last tag published successfully
#
# Usage: publish-demo.sh [--force]
#   --force  republish even when .published-tag already matches the release
#
# Environment overrides (mostly for testing):
#   DEMO_REPO          default gnacho/netpulse
#   DEMO_BASE          default /opt/netpulse-demo
#   DEMO_PUBLIC_URL    default https://demo.netpulse.cloudless.club/
#   DEMO_CHECK_PUBLIC  set to 0 to skip the served-bundle comparison
#   DEMO_KEEP          number of release directories to keep, default 5
#
# Exit codes: 0 published or already current, 1 failure (the unit goes failed
# so the drift shows up in systemctl and in the journal).

set -eu

REPO="${DEMO_REPO:-gnacho/netpulse}"
BASE="${DEMO_BASE:-/opt/netpulse-demo}"
PUBLIC_URL="${DEMO_PUBLIC_URL:-https://demo.netpulse.cloudless.club/}"
CHECK_PUBLIC="${DEMO_CHECK_PUBLIC:-1}"
KEEP="${DEMO_KEEP:-5}"
API="${DEMO_RELEASE_API:-https://api.github.com/repos/$REPO/releases/latest}"
FORCE=0
[ "${1:-}" = "--force" ] && FORCE=1

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
die() { log "ERROR: $*"; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl no esta instalado"
command -v python3 >/dev/null 2>&1 || die "python3 no esta instalado"

release_json=$(curl -fsSL --max-time 30 -H 'Accept: application/vnd.github+json' "$API") \
  || die "no pude consultar $API"

tag=$(printf '%s' "$release_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])') \
  || die "respuesta sin tag_name"
[ -n "$tag" ] || die "tag vacio en la respuesta de la API"

asset_url=$(printf '%s' "$release_json" | python3 -c '
import json, sys
data = json.load(sys.stdin)
want = "app-dist-%s.tar.gz" % data["tag_name"]
for asset in data.get("assets", []):
    if asset.get("name") == want:
        print(asset["browser_download_url"])
        break
') || die "no pude leer los assets del release"
[ -n "$asset_url" ] || die "el release $tag no trae app-dist-$tag.tar.gz"

if [ "$FORCE" -eq 0 ] && [ -f "$BASE/.published-tag" ] && [ "$(cat "$BASE/.published-tag")" = "$tag" ]; then
  log "al dia: $tag ya esta publicado"
  exit 0
fi

mkdir -p "$BASE/releases"
work=$(mktemp -d "$BASE/.tmp.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

asset_name="app-dist-$tag.tar.gz"
log "descargando $asset_name"
curl -fsSL --max-time 180 -o "$work/$asset_name" "$asset_url" || die "no pude descargar $asset_url"

sum_url=$(printf '%s' "$release_json" | python3 -c '
import json, sys
for asset in json.load(sys.stdin).get("assets", []):
    if asset.get("name") == "app-dist.sha256":
        print(asset["browser_download_url"])
        break
')
if [ -n "$sum_url" ]; then
  curl -fsSL --max-time 30 -o "$work/app-dist.sha256" "$sum_url" || die "no pude descargar el checksum"
  (cd "$work" && sha256sum -c app-dist.sha256) || die "el checksum no casa con el artefacto"
else
  log "AVISO: el release $tag no publica app-dist.sha256, sigo sin verificar"
fi

staging="$BASE/releases/$tag.new"
rm -rf "$staging"
mkdir -p "$staging"
tar xzf "$work/$asset_name" -C "$staging" || die "no pude descomprimir $asset_name"
[ -f "$staging/index.html" ] || die "el artefacto no trae index.html"
bundle=$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' "$staging/index.html" | head -1 || true)
[ -n "$bundle" ] || die "index.html no referencia ningun bundle en assets/"
[ -f "$staging/$bundle" ] || die "el bundle $bundle falta en el artefacto"

chmod -R a+rX "$staging"
chown -R 1000:1000 "$staging" 2>/dev/null || true

dest="$BASE/releases/$tag"
[ -e "$dest" ] && rm -rf "$dest"
mv "$staging" "$dest"

if [ -d "$BASE/public" ] && [ ! -L "$BASE/public" ]; then
  legacy="$BASE/public.presymlink-$(date -u +%Y%m%d-%H%M%S)"
  log "public es un directorio, lo conservo en $legacy y lo sustituyo por un enlace"
  mv "$BASE/public" "$legacy"
fi
ln -sfn "releases/$tag" "$BASE/.public.new"
mv -Tf "$BASE/.public.new" "$BASE/public"

if [ "$CHECK_PUBLIC" = "1" ]; then
  attempt=1
  served=""
  while [ "$attempt" -le 10 ]; do
    served=$(curl -fsS --max-time 10 "$PUBLIC_URL" 2>/dev/null \
      | grep -o 'assets/index-[A-Za-z0-9_-]*\.js' | head -1 || true)
    [ "$served" = "$bundle" ] && break
    attempt=$((attempt + 1))
    sleep 6
  done
  [ "$served" = "$bundle" ] || die "el demo sirve '$served' y la release $tag trae '$bundle'"
  log "verificado en $PUBLIC_URL: $served"
fi

printf '%s\n' "$tag" > "$BASE/.published-tag"

if [ "$KEEP" -gt 0 ]; then
  find "$BASE/releases" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' 2>/dev/null \
    | sort -rn | tail -n +$((KEEP + 1)) | cut -d' ' -f2- \
    | while read -r old; do
        rm -rf "$old"
      done
fi

log "publicado $tag ($bundle)"
