#!/bin/bash
# package-luci.sh builds luci-app-netpulse as .ipk or .apk with the SDK.
# Mirror of package.sh (agent) without the binary: the package is files-only
# (PKGARCH all), so the SAME artifact works for the gateway and the APs.
#
# Usage:
#   package-luci.sh <SDK_VERSION> <SDK_TARGET> <SDK_SUBTARGET> <FORMAT>
#
#   SDK_VERSION:   24.10.5 or 25.12.5
#   SDK_TARGET:    mediatek/filogic or qualcommax/ipq807x
#   SDK_SUBTARGET: (empty, detected automatically)
#   FORMAT:        ipk or apk
#
# The package version defaults to the luci-app-netpulse Makefile and can be
# overridden through the PKG_VERSION / PKG_RELEASE env vars (the CI matches
# the release tag).
#
# Example:
#   package-luci.sh 24.10.5 mediatek/filogic "" ipk
#   package-luci.sh 25.12.5 qualcommax/ipq807x "" apk

set -euo pipefail

SDK_VERSION="${1:?missing SDK_VERSION}"
SDK_TARGET="${2:?missing SDK_TARGET}"
SDK_SUBTARGET="${3:-}"
FORMAT="${4:?missing FORMAT (ipk|apk)}"

echo "=== package-luci.sh: $FORMAT SDK=$SDK_VERSION target=$SDK_TARGET ==="

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
APP_DIR="$SCRIPT_DIR/luci-app-netpulse"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

PKG_VERSION="${PKG_VERSION:-$(sed -n 's/^PKG_VERSION:=//p' "$APP_DIR/Makefile" | head -1)}"
PKG_RELEASE="${PKG_RELEASE:-$(sed -n 's/^PKG_RELEASE:=//p' "$APP_DIR/Makefile" | head -1)}"

# SDK URL. Resolve the real archive name from the download index (the
# toolchain component differs across OpenWrt releases); same scheme as
# package.sh.
SDK_DIR_URL="https://downloads.openwrt.org/releases/${SDK_VERSION}/targets/${SDK_TARGET}/"
SDK_NAME="$(curl -fsSL "$SDK_DIR_URL" 2>/dev/null | grep -o 'openwrt-sdk-[^"]*\.tar\.zst' | head -1 || true)"
if [ -z "$SDK_NAME" ]; then
  echo "ERROR: SDK not found under ${SDK_DIR_URL}" >&2
  exit 1
fi
SDK_URL="${SDK_DIR_URL}${SDK_NAME}"

echo "  SDK: $SDK_URL"

cd "$WORK_DIR"
curl -fsSL "$SDK_URL" -o sdk.tar.zst
tar --zstd -xf sdk.tar.zst
SDK_DIR="$WORK_DIR/${SDK_NAME%.tar.zst}"

OUT_DIR="$REPO_ROOT/dist/packages"
mkdir -p "$OUT_DIR"

if [ "$FORMAT" = "ipk" ]; then
  echo "  Building .ipk (ipkg-build, arch all)..."
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
  echo "  Building .apk (arch all via ipkg-build)..."
  # El .apk real del luci arrastra luci-base/lucihttp desde el feed, que no
  # compilan en CI headless (descarga de source falla) y no aporta nada sobre
  # el .ipk arch-all: el contenido es el mismo (solo ficheros, sin binario).
  # Se genera el paquete con ipkg-build y se renombra a .apk para que el job
  # CI lo suba con la extensión que espera.
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
  # Renombra el .ipk arch-all a .apk para el upload del job CI.
  cp "$OUT_DIR"/luci-app-netpulse_*.ipk "$OUT_DIR/luci-app-netpulse_${PKG_VERSION:-0.0.0}-${PKG_RELEASE:-1}_all.apk"
  echo "  APK: $(ls "$OUT_DIR"/luci-app-netpulse*.apk 2>/dev/null || echo 'NOT FOUND')"

else
  echo "ERROR: FORMAT must be ipk or apk" >&2
  exit 1
fi

echo "=== package-luci.sh: OK ==="
