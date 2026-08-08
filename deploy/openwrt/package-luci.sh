#!/bin/bash
# package-luci.sh — empaqueta luci-app-netpulse en .ipk y .apk con el SDK.
# Espejo de package.sh (agente) sin la parte de binario: el paquete es solo
# ficheros (PKGARCH all), así que el MISMO artefacto vale para gateway y APs.
#
# Uso:
#   package-luci.sh <SDK_VERSION> <SDK_TARGET> <SDK_SUBTARGET> <FORMAT>
#
#   SDK_VERSION:  24.10.5  o  25.12.5
#   SDK_TARGET:   mediatek/filogic  o  qualcommax/ipq807x
#   SDK_SUBTARGET: (vacío, se detecta solo)
#   FORMAT:       ipk  o  apk
#
# Ejemplo:
#   package-luci.sh 24.10.5 mediatek/filogic "" ipk
#   package-luci.sh 25.12.5 qualcommax/ipq807x "" apk

set -euo pipefail

SDK_VERSION="${1:?falta SDK_VERSION}"
SDK_TARGET="${2:?falta SDK_TARGET}"
SDK_SUBTARGET="${3:-}"
FORMAT="${4:?falta FORMAT (ipk|apk)}"

echo "=== package-luci.sh: $FORMAT SDK=$SDK_VERSION target=$SDK_TARGET ==="

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APP_DIR="$SCRIPT_DIR/luci-app-netpulse"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

PKG_VERSION=$(sed -n 's/^PKG_VERSION:=//p' "$APP_DIR/Makefile" | head -1)
PKG_RELEASE=$(sed -n 's/^PKG_RELEASE:=//p' "$APP_DIR/Makefile" | head -1)

# ── SDK URL (mismo esquema que package.sh) ───────────────────────────
SDK_NAME="openwrt-sdk-${SDK_VERSION}-${SDK_TARGET//\//-}_gcc-13.3.0_musl.Linux-x86_64"
SDK_URL="https://downloads.openwrt.org/releases/${SDK_VERSION}/targets/${SDK_TARGET}/${SDK_NAME}.tar.zst"

echo "  SDK: $SDK_URL"

cd "$WORK_DIR"
curl -fsSL "$SDK_URL" -o sdk.tar.zst
tar --zstd -xf sdk.tar.zst
SDK_DIR="$WORK_DIR/$SDK_NAME"

OUT_DIR="$REPO_ROOT/dist/packages"
mkdir -p "$OUT_DIR"

if [ "$FORMAT" = "ipk" ]; then
  echo "  Construyendo .ipk (ipkg-build, arch all)..."
  PKG_DIR="$WORK_DIR/luci-app-netpulse-pkg"
  mkdir -p "$PKG_DIR/CONTROL"
  cp -r "$APP_DIR/files/." "$PKG_DIR/"
  chmod 755 "$PKG_DIR/usr/libexec/rpcd/luci.netpulse"

  cat > "$PKG_DIR/CONTROL/control" << CTRL
Package: luci-app-netpulse
Version: ${PKG_VERSION:-0.0.0}-${PKG_RELEASE:-1}
Depends: luci-base, netpulse-agent
License: AGPL-3.0-only
Section: luci
Architecture: all
Maintainer: Nacho <netpulse@cloudless.club>
Description: Node-local NetPulse agent view for LuCI
 LuCI pages for the NetPulse agent on THIS router: procd status, binary
 version, RSS, heartbeat, recent syslog lines, restart button and
 editable UCI configuration, plus a link to the NetPulse web app.
CTRL

  cat > "$PKG_DIR/CONTROL/postinst" << 'POSTINST'
#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
/etc/init.d/rpcd restart 2>/dev/null || true
rm -f /tmp/luci-indexcache* 2>/dev/null || true
exit 0
POSTINST
  chmod 755 "$PKG_DIR/CONTROL/postinst"

  cat > "$PKG_DIR/CONTROL/postrm" << 'POSTRM'
#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
/etc/init.d/rpcd restart 2>/dev/null || true
rm -f /tmp/luci-indexcache* 2>/dev/null || true
exit 0
POSTRM
  chmod 755 "$PKG_DIR/CONTROL/postrm"

  "$SDK_DIR/scripts/ipkg-build" "$PKG_DIR" "$OUT_DIR"
  echo "  IPK: $(ls "$OUT_DIR"/luci-app-netpulse_*.ipk)"

elif [ "$FORMAT" = "apk" ]; then
  echo "  Construyendo .apk vía SDK..."
  cp -r "$APP_DIR" "$SDK_DIR/package/luci-app-netpulse"

  cd "$SDK_DIR"
  # luci-base solo como dependencia de instalación; el feed luci se indexa
  # para que el resolutor del SDK la conozca.
  ./scripts/feeds update luci 2>/dev/null || true
  ./scripts/feeds install luci-base 2>/dev/null || true
  make defconfig >/dev/null 2>&1 || true
  make package/luci-app-netpulse/compile V=s 2>&1 | tail -20

  find bin/packages -name "luci-app-netpulse*.apk" -exec cp {} "$OUT_DIR/" \;
  echo "  APK: $(ls "$OUT_DIR"/luci-app-netpulse*.apk 2>/dev/null || echo 'NO ENCONTRADO')"

else
  echo "ERROR: FORMAT debe ser ipk o apk" >&2
  exit 1
fi

echo "=== package-luci.sh: OK ==="
