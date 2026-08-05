# netpulse-agent — OpenWrt Makefile (Fase 7.4)
# Empaqueta el binario Go estático precompilado para arm64/armv7/amd64.
#
# Uso (desde el SDK o buildroot de OpenWrt):
#   cp -r netpulse-agent package/utils/
#   make menuconfig  # seleccionar Utilities → netpulse-agent
#   make package/netpulse-agent/compile V=s
#
# O con el feed propio:
#   echo "src-git netpulse https://github.com/gnacho/netpulse.git" >> feeds.conf
#   ./scripts/feeds update netpulse && ./scripts/feeds install netpulse-agent

include $(TOPDIR)/rules.mk

PKG_NAME:=netpulse-agent
PKG_VERSION:=2.6.0
PKG_RELEASE:=1

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/gnacho/netpulse.git
PKG_SOURCE_VERSION:=main
PKG_MIRROR_HASH:=skip

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@example.com>

include $(INCLUDE_DIR)/package.mk

# Go binario estático — sin dependencias de librerías C
# Construido con CGO_ENABLED=0; arquitecturas soportadas:
#   aarch64 (arm64) — GL-MT6000, Redmi AX6 y la mayoría de routers modernos
#   arm (armv7)    — routers Cortex-A7/A9 más antiguos
#   x86_64 (amd64) — PCs/x86 corriendo OpenWrt

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
	server. Listens for nl80211 events (assoc/disassoc) in real time and
	receives commands from the server via SSE. Stateless: no writes to
	flash; logs go to syslog via stderr.
endef

# ---------------------------------------------------------------------------
# Build — el binario se compila desde el código fuente del repo con Go.
# Requiere golang en el host de compilación (el SDK de OpenWrt incluye Go).
# Si no quieres compilar, sustituye esta sección por la descarga del binario
# precompilado de la release.
# ---------------------------------------------------------------------------

GO_ARCH_DETECT:=$(shell echo $(ARCH) | sed 's/aarch64/arm64/;s/arm_cortex-a7/armv7/;s/arm_cortex-a9/armv7/;s/arm_cortex-a53/arm64/;s/arm_cortex-a55/arm64/;s/x86_64/amd64/')

define Build/Compile
	(cd $(PKG_BUILD_DIR)/agent && \
	 CGO_ENABLED=0 GOOS=linux GOARCH=$(GO_ARCH_DETECT) \
	 go build -trimpath -ldflags "-s -w -X main.Version=$(PKG_VERSION)" \
	 -o $(PKG_BUILD_DIR)/netpulse-agent ./cmd/netpulse-agent)
endef

define Package/netpulse-agent/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/netpulse-agent $(1)/usr/sbin/netpulse-agent

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/netpulse-agent.init $(1)/etc/init.d/netpulse-agent

	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/netpulse-agent.config $(1)/etc/config/netpulse-agent

	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/netpulse-agent.defaults $(1)/etc/uci-defaults/90-netpulse-agent
endef

$(eval $(call BuildPackage,netpulse-agent))
