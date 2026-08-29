#!/bin/bash
# package-server.sh builds the on-box netpulse SERVER package (.ipk/.apk)
# from a prebuilt static binary, mirroring package.sh (agent). Pure Go
# (modernc sqlite) so the arm64 build is fully static.
#
# Usage:
#   package-server.sh <SDK_VERSION> <SDK_TARGET> <SDK_SUBTARGET> <FORMAT> <BINARY_PATH>
#
#   FORMAT:      ipk or apk
#   BINARY_PATH: prebuilt netpulse server binary (arm64)
#
# PKG_VERSION / PKG_RELEASE overridable via env (CI uses the release tag).
#
# Example:
#   package-server.sh 25.12.5 qualcommax/ipq807x "" apk ./netpulse

set -euo pipefail

SDK_VERSION="${1:?missing SDK_VERSION}"
SDK_TARGET="${2:?missing SDK_TARGET}"
SDK_SUBTARGET="${3:-}"
FORMAT="${4:?missing FORMAT (ipk|apk)}"
BINARY="${5:?missing BINARY_PATH}"

if [ ! -f "$BINARY" ]; then
  echo "ERROR: binary not found: $BINARY" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PKG_VERSION="${PKG_VERSION:-2.0.0}"
PKG_RELEASE="${PKG_RELEASE:-1}"

echo "=== package-server.sh: $FORMAT SDK=$SDK_VERSION target=$SDK_TARGET version=${PKG_VERSION}-${PKG_RELEASE} ==="

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

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

# Stage binary and package files (package name: netpulse, like the Makefile)
cp "$BINARY" "$WORK_DIR/netpulse"
chmod 755 "$WORK_DIR/netpulse"

PKG_DIR="$WORK_DIR/netpulse-pkg"
mkdir -p "$PKG_DIR/CONTROL" "$PKG_DIR/usr/sbin" "$PKG_DIR/etc/init.d" "$PKG_DIR/etc/config" "$PKG_DIR/etc/uci-defaults"

cp "$WORK_DIR/netpulse" "$PKG_DIR/usr/sbin/netpulse"
cp "$REPO_ROOT/deploy/openwrt/netpulse-server/files/netpulse-server.init" "$PKG_DIR/etc/init.d/netpulse"
cp "$REPO_ROOT/deploy/openwrt/netpulse-server/files/netpulse-server.config" "$PKG_DIR/etc/config/netpulse"
cp "$REPO_ROOT/deploy/openwrt/netpulse-server/files/netpulse-server.defaults" "$PKG_DIR/etc/uci-defaults/95-netpulse-server"

cat > "$PKG_DIR/CONTROL/control" << CTRL
Package: netpulse
Version: ${PKG_VERSION}-${PKG_RELEASE}
Depends: curl, ca-bundle
License: AGPL-3.0-only
Section: net
Architecture: aarch64_cortex-a53
Maintainer: Nacho <netpulse@cloudless.club>
Description: NetPulse network monitor (on-box server)
 NetPulse server running on-box: serves the web app and ingests agent
 data over HTTPS with self-signed TLS + SPKI pinning. Stores data in
 SQLite (overlay or USB). Same binary as the dedicated-node deployment.
CTRL

cat > "$PKG_DIR/CONTROL/postinst" << 'POSTINST'
#!/bin/sh
# NetGrip takeover rule (#363): a router running NetGrip manages its own
# panel; installing the on-box server here is still allowed (they can
# coexist: different ports), so no guard needed for the server.
[ -f /etc/uci-defaults/95-netpulse-server ] && sh /etc/uci-defaults/95-netpulse-server
/etc/init.d/netpulse restart 2>/dev/null || true
exit 0
POSTINST
chmod 755 "$PKG_DIR/CONTROL/postinst"

cat > "$PKG_DIR/CONTROL/prerm" << 'PRERM'
#!/bin/sh
/etc/init.d/netpulse stop 2>/dev/null || true
/etc/init.d/netpulse disable 2>/dev/null || true
exit 0
PRERM
chmod 755 "$PKG_DIR/CONTROL/prerm"

echo "/etc/config/netpulse" > "$PKG_DIR/CONTROL/conffiles"

OUT_DIR="$REPO_ROOT/dist/packages"
mkdir -p "$OUT_DIR"

if [ "$FORMAT" = "ipk" ]; then
  echo "  Building .ipk..."
  "$SDK_DIR/scripts/ipkg-build" "$PKG_DIR" "$OUT_DIR"
  echo "  IPK: $(ls "$OUT_DIR"/netpulse_*.ipk 2>/dev/null || echo 'NOT FOUND')"

elif [ "$FORMAT" = "apk" ]; then
  echo "  Building .apk via SDK..."
  PKG_DIR_SDK="$SDK_DIR/package/utils/netpulse"
  mkdir -p "$PKG_DIR_SDK/files"
  cp -r "$PKG_DIR/usr" "$PKG_DIR/etc" "$PKG_DIR_SDK/files/"
  cat > "$PKG_DIR_SDK/Makefile" << 'MAKE'
include $(TOPDIR)/rules.mk

PKG_NAME:=netpulse
PKG_VERSION:=@PKG_VERSION@
PKG_RELEASE:=@PKG_RELEASE@

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@cloudless.club>

include $(INCLUDE_DIR)/package.mk

define Package/netpulse
	SECTION:=net
	CATEGORY:=Network
	TITLE:=NetPulse network monitor (on-box server)
	URL:=https://github.com/gnacho/netpulse
	DEPENDS:=+curl +ca-bundle @(aarch64||arm||x86_64)
endef

define Package/netpulse/description
	NetPulse server running on-box: serves the web app and ingests agent
	data over HTTPS with self-signed TLS + SPKI pinning. SQLite storage.
endef

define Build/Compile
endef

define Package/netpulse/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) ./files/usr/sbin/netpulse $(1)/usr/sbin/netpulse
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/netpulse $(1)/etc/init.d/netpulse
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/netpulse $(1)/etc/config/netpulse
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/etc/uci-defaults/95-netpulse-server $(1)/etc/uci-defaults/95-netpulse-server
endef

define Package/netpulse/postinst
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	[ -f /etc/uci-defaults/95-netpulse-server ] && sh /etc/uci-defaults/95-netpulse-server
	/etc/init.d/netpulse restart 2>/dev/null || true
fi
exit 0
endef

$(eval $(call BuildPackage,netpulse))
MAKE
  sed -i "s/@PKG_VERSION@/${PKG_VERSION}/; s/@PKG_RELEASE@/${PKG_RELEASE}/" "$PKG_DIR_SDK/Makefile"

  cd "$SDK_DIR"
  make defconfig >/dev/null 2>&1 || true
  make package/netpulse/compile V=s 2>&1 | tail -15
  find bin/packages -name "netpulse-*.apk" -exec cp {} "$OUT_DIR/" \;
  echo "  APK: $(ls "$OUT_DIR"/netpulse-*.apk 2>/dev/null || echo 'NOT FOUND')"

else
  echo "ERROR: FORMAT must be ipk or apk" >&2
  exit 1
fi

# El .apk del SERVER se llama netpulse-<v>.apk (sin sufijo de arch en 25.12
# cuando el paquete es arch-specific por DEPENDS); renombrar para evitar
# colisionar con el tarball netpulse_<v>_linux_arm64.tar.gz en la release.
echo "=== package-server.sh: OK ==="
