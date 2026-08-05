#!/bin/bash
# package.sh — empaqueta netpulse-agent en .ipk y .apk usando el SDK de OpenWrt.
#
# Uso:
#   package.sh <SDK_VERSION> <SDK_TARGET> <SDK_SUBTARGET> <FORMAT> <BINARY_PATH>
#
#   SDK_VERSION:  24.10.5  o  25.12.5
#   SDK_TARGET:   mediatek/filogic  o  qualcommax/ipq807x
#   SDK_SUBTARGET: (vacío para 24.10, se detecta solo)
#   FORMAT:       ipk  o  apk
#   BINARY_PATH:  ruta al binario netpulse-agent precompilado (arm64)
#
# Ejemplo:
#   package.sh 24.10.5 mediatek/filogic "" ipk ./netpulse-agent-arm64
#   package.sh 25.12.5 qualcommax/ipq807x "" apk  ./netpulse-agent-arm64

set -euo pipefail

SDK_VERSION="${1:?falta SDK_VERSION}"
SDK_TARGET="${2:?falta SDK_TARGET}"
SDK_SUBTARGET="${3:-}"
FORMAT="${4:?falta FORMAT (ipk|apk)}"
BINARY="${5:?falta BINARY_PATH}"

if [ ! -f "$BINARY" ]; then
  echo "ERROR: binario no encontrado: $BINARY" >&2
  exit 1
fi

echo "=== package.sh: $FORMAT SDK=$SDK_VERSION target=$SDK_TARGET ==="

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# ── SDK URL ──────────────────────────────────────────────────────────
SDK_NAME="openwrt-sdk-${SDK_VERSION}-${SDK_TARGET//\//-}_gcc-13.3.0_musl.Linux-x86_64"
SDK_URL="https://downloads.openwrt.org/releases/${SDK_VERSION}/targets/${SDK_TARGET}/${SDK_NAME}.tar.zst"

echo "  SDK: $SDK_URL"

# ── Descargar y extraer SDK ──────────────────────────────────────────
cd "$WORK_DIR"
curl -fsSL "$SDK_URL" -o sdk.tar.zst
tar --zstd -xf sdk.tar.zst
SDK_DIR="$WORK_DIR/$SDK_NAME"

# ── Preparar binario ─────────────────────────────────────────────────
BIN_SIZE=$(stat -c%s "$BINARY" 2>/dev/null || stat -f%z "$BINARY")
cp "$BINARY" "$WORK_DIR/netpulse-agent"
chmod 755 "$WORK_DIR/netpulse-agent"

# ── Crear estructura de paquete ──────────────────────────────────────
PKG_DIR="$WORK_DIR/netpulse-agent-pkg"
mkdir -p "$PKG_DIR/CONTROL" "$PKG_DIR/usr/sbin" "$PKG_DIR/etc/init.d" "$PKG_DIR/etc/config" "$PKG_DIR/etc/uci-defaults"

cp "$WORK_DIR/netpulse-agent" "$PKG_DIR/usr/sbin/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.init"   "$PKG_DIR/etc/init.d/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.config" "$PKG_DIR/etc/config/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.defaults" "$PKG_DIR/etc/uci-defaults/90-netpulse-agent"

# ── CONTROL files ────────────────────────────────────────────────────
cat > "$PKG_DIR/CONTROL/control" << CTRL
Package: netpulse-agent
Version: ${PKG_VERSION:-0.0.0}-${PKG_RELEASE:-1}
Depends: iw
License: AGPL-3.0-only
Section: utils
Architecture: aarch64_cortex-a53
Maintainer: Nacho <netpulse@cloudless.club>
Description: NetPulse native agent for OpenWrt
 Native OpenWrt agent for NetPulse network monitor. Probes the local
 router (system, wireless, DHCP, FDB) and pushes data to a NetPulse
 server. Listens for nl80211 events (assoc/disassoc) in real time and
 receives commands from the server via SSE. Stateless: no writes to
 flash; logs go to syslog via stderr.
CTRL

cat > "$PKG_DIR/CONTROL/postinst" << 'POSTINST'
#!/bin/sh
if [ -f /etc/init.d/netpulse-agent ] && /etc/init.d/netpulse-agent enabled; then
  /etc/init.d/netpulse-agent stop 2>/dev/null || true
fi
[ -f /etc/uci-defaults/90-netpulse-agent ] && sh /etc/uci-defaults/90-netpulse-agent
exit 0
POSTINST
chmod 755 "$PKG_DIR/CONTROL/postinst"

cat > "$PKG_DIR/CONTROL/prerm" << 'PRERM'
#!/bin/sh
[ -f /etc/init.d/netpulse-agent ] && /etc/init.d/netpulse-agent stop 2>/dev/null || true
[ -f /etc/init.d/netpulse-agent ] && /etc/init.d/netpulse-agent disable 2>/dev/null || true
crontab -l 2>/dev/null | grep -v netpulse-watchdog | crontab - 2>/dev/null || true
exit 0
PRERM
chmod 755 "$PKG_DIR/CONTROL/prerm"

echo "/etc/config/netpulse-agent" > "$PKG_DIR/CONTROL/conffiles"

# ── Construir paquete ────────────────────────────────────────────────
OUT_DIR="$REPO_ROOT/dist/packages"
mkdir -p "$OUT_DIR"

if [ "$FORMAT" = "ipk" ]; then
  echo "  Construyendo .ipk..."
  "$SDK_DIR/scripts/ipkg-build" "$PKG_DIR" "$OUT_DIR"
  echo "  IPK: $(ls "$OUT_DIR"/netpulse-agent_*.ipk)"

elif [ "$FORMAT" = "apk" ]; then
  echo "  Construyendo .apk vía SDK..."
  # El SDK 25.12+ produce .apk con make. Copiamos el Makefile existente
  # pero le inyectamos el binario precompilado (sin compilar Go).
  mkdir -p "$SDK_DIR/package/utils/netpulse-agent/files"
  cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/Makefile" "$SDK_DIR/package/utils/netpulse-agent/"
  cp -r "$PKG_DIR/usr" "$PKG_DIR/etc" "$SDK_DIR/package/utils/netpulse-agent/files/" 2>/dev/null || true

  # Parche: saltar Build/Compile (el binario YA está en files/)
  sed -i 's/^define Build\/Compile/define Build\/Compile_disabled/' "$SDK_DIR/package/utils/netpulse-agent/Makefile"

  cd "$SDK_DIR"
  # Feeds mínimos para resolver dependencias
  ./scripts/feeds update packages 2>/dev/null || true
  ./scripts/feeds install iw 2>/dev/null || true
  make package/netpulse-agent/compile V=s 2>&1 | tail -20

  # Recoger .apk
  find bin/packages -name "netpulse-agent*.apk" -exec cp {} "$OUT_DIR/" \;
  echo "  APK: $(ls "$OUT_DIR"/netpulse-agent*.apk 2>/dev/null || echo 'NO ENCONTRADO')"

else
  echo "ERROR: FORMAT debe ser ipk o apk" >&2
  exit 1
fi

echo "=== package.sh: OK ==="
