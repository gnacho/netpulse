#!/bin/bash
# package.sh — builds the netpulse-agent OpenWrt package as .ipk or .apk
# using the OpenWrt SDK.
#
# Usage:
#   package.sh <SDK_VERSION> <SDK_TARGET> <SDK_SUBTARGET> <FORMAT> <BINARY_PATH>
#
#   SDK_VERSION:   24.10.5 or 25.12.5
#   SDK_TARGET:    mediatek/filogic or qualcommax/ipq807x
#   SDK_SUBTARGET: (empty, detected automatically)
#   FORMAT:        ipk or apk
#   BINARY_PATH:   path to the prebuilt netpulse-agent binary (arm64)
#
# The package version defaults to the netpulse-agent Makefile (PKG_VERSION /
# PKG_RELEASE) and can be overridden through the PKG_VERSION / PKG_RELEASE env
# vars, which is what the CI does to match the release tag.
#
# Example:
#   package.sh 24.10.5 mediatek/filogic "" ipk ./netpulse-agent-arm64
#   package.sh 25.12.5 qualcommax/ipq807x "" apk ./netpulse-agent-arm64

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

PKG_VERSION="${PKG_VERSION:-$(sed -n 's/^PKG_VERSION:=//p' "$SCRIPT_DIR/netpulse-agent/Makefile" | head -1)}"
PKG_RELEASE="${PKG_RELEASE:-$(sed -n 's/^PKG_RELEASE:=//p' "$SCRIPT_DIR/netpulse-agent/Makefile" | head -1)}"

echo "=== package.sh: $FORMAT SDK=$SDK_VERSION target=$SDK_TARGET version=${PKG_VERSION:-0.0.0}-${PKG_RELEASE:-1} ==="

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# SDK URL. The toolchain name in the archive differs across OpenWrt releases
# (24.10 uses gcc-13.3.0, 25.12 uses gcc-14.3.0), so resolve the real name
# from the download index instead of hardcoding it.
SDK_DIR_URL="https://downloads.openwrt.org/releases/${SDK_VERSION}/targets/${SDK_TARGET}/"
SDK_NAME="$(curl -fsSL "$SDK_DIR_URL" 2>/dev/null | grep -o 'openwrt-sdk-[^"]*\.tar\.zst' | head -1 || true)"
if [ -z "$SDK_NAME" ]; then
  echo "ERROR: SDK not found under ${SDK_DIR_URL}" >&2
  exit 1
fi
SDK_URL="${SDK_DIR_URL}${SDK_NAME}"

echo "  SDK: $SDK_URL"

# Download and extract the SDK
cd "$WORK_DIR"
curl -fsSL "$SDK_URL" -o sdk.tar.zst
tar --zstd -xf sdk.tar.zst
SDK_DIR="$WORK_DIR/${SDK_NAME%.tar.zst}"

# Stage the binary and package files
cp "$BINARY" "$WORK_DIR/netpulse-agent"
chmod 755 "$WORK_DIR/netpulse-agent"

PKG_DIR="$WORK_DIR/netpulse-agent-pkg"
mkdir -p "$PKG_DIR/CONTROL" "$PKG_DIR/usr/sbin" "$PKG_DIR/etc/init.d" "$PKG_DIR/etc/config" "$PKG_DIR/etc/uci-defaults"

cp "$WORK_DIR/netpulse-agent" "$PKG_DIR/usr/sbin/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.init" "$PKG_DIR/etc/init.d/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.config" "$PKG_DIR/etc/config/netpulse-agent"
cp "$REPO_ROOT/deploy/openwrt/netpulse-agent/files/netpulse-agent.defaults" "$PKG_DIR/etc/uci-defaults/90-netpulse-agent"

# CONTROL files
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

# Build the package
OUT_DIR="$REPO_ROOT/dist/packages"
mkdir -p "$OUT_DIR"

if [ "$FORMAT" = "ipk" ]; then
  echo "  Building .ipk..."
  "$SDK_DIR/scripts/ipkg-build" "$PKG_DIR" "$OUT_DIR"
  echo "  IPK: $(ls "$OUT_DIR"/netpulse-agent_*.ipk)"

elif [ "$FORMAT" = "apk" ]; then
  echo "  Building .apk via SDK..."
  # SDK 25.12+ builds .apk with make. Generate a source-less Makefile with a
  # no-op Build/Compile: the prebuilt binary and its files are staged in
  # files/, so the SDK neither clones the repo nor compiles Go.
  PKG_DIR_SDK="$SDK_DIR/package/utils/netpulse-agent"
  mkdir -p "$PKG_DIR_SDK/files"
  cp -r "$PKG_DIR/usr" "$PKG_DIR/etc" "$PKG_DIR_SDK/files/"
  cat > "$PKG_DIR_SDK/Makefile" << 'MAKE'
include $(TOPDIR)/rules.mk

PKG_NAME:=netpulse-agent
PKG_VERSION:=@PKG_VERSION@
PKG_RELEASE:=@PKG_RELEASE@

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@cloudless.club>

include $(INCLUDE_DIR)/package.mk

define Package/netpulse-agent
	SECTION:=utils
	CATEGORY:=Utilities
	TITLE:=NetPulse native agent for OpenWrt
	URL:=https://github.com/gnacho/netpulse
	DEPENDS:=+iw @(aarch64||arm||x86_64)
endef

define Package/netpulse-agent/description
	Native OpenWrt agent for NetPulse network monitor. Probes the local
	router (system, wireless, DHCP, FDB) and pushes data to a NetPulse
	server. Stateless: no writes to flash.
endef

define Build/Compile
endef

define Package/netpulse-agent/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) ./files/usr/sbin/netpulse-agent $(1)/usr/sbin/netpulse-agent
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/netpulse-agent $(1)/etc/init.d/netpulse-agent
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/netpulse-agent $(1)/etc/config/netpulse-agent
	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/etc/uci-defaults/90-netpulse-agent $(1)/etc/uci-defaults/90-netpulse-agent
endef

define Package/netpulse-agent/postinst
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	/etc/init.d/netpulse-agent stop 2>/dev/null || true
	[ -f /etc/uci-defaults/90-netpulse-agent ] && sh /etc/uci-defaults/90-netpulse-agent
fi
exit 0
endef

$(eval $(call BuildPackage,netpulse-agent))
MAKE
  sed -i "s/@PKG_VERSION@/${PKG_VERSION:-0.0.0}/; s/@PKG_RELEASE@/${PKG_RELEASE:-1}/" "$PKG_DIR_SDK/Makefile"

  cd "$SDK_DIR"
  # Minimal feeds so the package index resolves iw; best effort.
  ./scripts/feeds update packages 2>/dev/null || true
  ./scripts/feeds install iw 2>/dev/null || true
  make package/netpulse-agent/compile V=s 2>&1 | tail -20

  find bin/packages -name "netpulse-agent*.apk" -exec cp {} "$OUT_DIR/" \;
  echo "  APK: $(ls "$OUT_DIR"/netpulse-agent*.apk 2>/dev/null || echo 'NOT FOUND')"

else
  echo "ERROR: FORMAT must be ipk or apk" >&2
  exit 1
fi

echo "=== package.sh: OK ==="
