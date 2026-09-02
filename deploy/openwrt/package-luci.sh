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
  echo "  Building .apk (real apk v3 via 25.12 SDK)..."
  # #468: renaming an ipk to .apk does NOT work: apk-tools v3 on the router
  # refuses the ipk bytes with "v2 package format error". Build a real v3
  # package with the SDK, same source-less Makefile pattern package.sh uses
  # for netpulse-agent: files staged under files/, no-op Build/Compile, and
  # the SDK's own `apk mkpkg` produces the signed-v3 tarball. Dependencies
  # are recorded as metadata; the SDK does not need them present.
  PKG_DIR_SDK="$SDK_DIR/package/utils/luci-app-netpulse"
  mkdir -p "$PKG_DIR_SDK/files"
  cp -r "$APP_DIR/files/." "$PKG_DIR_SDK/files/"
  chmod 755 "$PKG_DIR_SDK/files/usr/libexec/rpcd/luci.netpulse"
  cat > "$PKG_DIR_SDK/Makefile" << 'MAKE'
include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-netpulse
PKG_VERSION:=@PKG_VERSION@
PKG_RELEASE:=@PKG_RELEASE@

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@cloudless.club>

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-netpulse
	SECTION:=luci
	CATEGORY:=LuCI
	TITLE:=Node-local NetPulse agent view for LuCI
	URL:=https://github.com/gnacho/netpulse
	DEPENDS:=+luci-base +netpulse-agent
	PKGARCH:=all
endef

define Package/luci-app-netpulse/description
	LuCI pages for the NetPulse agent on this router: procd status,
	binary version, RSS, heartbeat, recent syslog lines, restart button
	and editable UCI configuration, plus a link to the NetPulse web app.
endef

define Build/Compile
endef

define Package/luci-app-netpulse/install
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./files/usr/share/luci/menu.d/luci-app-netpulse.json $(1)/usr/share/luci/menu.d/
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./files/usr/share/rpcd/acl.d/luci-app-netpulse.json $(1)/usr/share/rpcd/acl.d/
	$(INSTALL_DIR) $(1)/usr/libexec/rpcd
	$(INSTALL_BIN) ./files/usr/libexec/rpcd/luci.netpulse $(1)/usr/libexec/rpcd/
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/netpulse
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/netpulse/*.js $(1)/www/luci-static/resources/view/netpulse/
endef

define Package/luci-app-netpulse/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/rpcd restart 2>/dev/null || true
rm -f /tmp/luci-indexcache* 2>/dev/null || true
exit 0
endef

$(eval $(call BuildPackage,luci-app-netpulse))
MAKE
  sed -i "s/@PKG_VERSION@/${PKG_VERSION:-0.0.0}/; s/@PKG_RELEASE@/${PKG_RELEASE:-1}/" "$PKG_DIR_SDK/Makefile"

  cd "$SDK_DIR"
  # .config válido sin terminal (menuconfig muere en CI headless).
  make defconfig >/dev/null 2>&1 || true
  make package/luci-app-netpulse/compile V=s 2>&1 | tail -20
  find bin/packages -name "luci-app-netpulse*.apk" -exec cp {} "$OUT_DIR/" \;
  echo "  APK: $(ls "$OUT_DIR"/luci-app-netpulse*.apk 2>/dev/null || echo 'NOT FOUND')"

else
  echo "ERROR: FORMAT must be ipk or apk" >&2
  exit 1
fi

echo "=== package-luci.sh: OK ==="
