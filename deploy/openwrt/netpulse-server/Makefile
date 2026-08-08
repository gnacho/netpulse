# netpulse-server — OpenWrt Makefile (Fase 9 R1)
# Empaqueta el servidor NetPulse (binario Go estático con la webapp embebida)
# para correr on-box en el gateway. El mismo binario que el CT, con
# NETPULSE_ONBOX=1 activando TLS autofirmado + config UCI + pairing.
#
# Uso (desde el SDK o buildroot de OpenWrt):
#   cp -r netpulse-server package/utils/
#   make menuconfig  # seleccionar Network → netpulse
#   make package/netpulse/compile V=s

include $(TOPDIR)/rules.mk

PKG_NAME:=netpulse
PKG_VERSION:=2.7.2
PKG_RELEASE:=1

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/gnacho/netpulse.git
PKG_SOURCE_VERSION:=main
PKG_MIRROR_HASH:=skip

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@example.com>

include $(INCLUDE_DIR)/package.mk

# Requisitos: curl para /fingerprint durante pairing, ca-bundle para HTTPS
# saliente (updater). El binario Go es estático (CGO_ENABLED=0).
define Package/netpulse
	SECTION:=net
	CATEGORY:=Network
	TITLE:=NetPulse network monitor (on-box server)
	URL:=https://github.com/gnacho/netpulse
	DEPENDS:=+curl +ca-bundle @(aarch64||arm||x86_64)
endef

define Package/netpulse/description
	NetPulse server running on-box: serves the web app and ingests agent
	data over HTTPS with self-signed TLS + SPKI pinning. Stores data in
	SQLite (overlay or USB). Same binary as the dedicated-node deployment;
	NETPULSE_ONBOX=1 activates UCI config, TLS and pairing.
endef

GO_ARCH_DETECT:=$(shell echo $(ARCH) | sed 's/aarch64/arm64/;s/arm_cortex-a7/armv7/;s/arm_cortex-a9/armv7/;s/arm_cortex-a53/arm64/;s/arm_cortex-a55/arm64/;s/x86_64/amd64/')

# El build compila el frontend (npm) y luego el binario Go con el dist
# embebido. Requiere node + go en el host de compilación.
define Build/Compile
	# Frontend
	(cd $(PKG_BUILD_DIR)/app && npm ci --no-audit --no-fund && npm run build)
	# Copiar dist al embed del binario
	rm -rf $(PKG_BUILD_DIR)/server-go/internal/staticspa/dist
	cp -r $(PKG_BUILD_DIR)/app/dist $(PKG_BUILD_DIR)/server-go/internal/staticspa/dist
	# Agent binaries para embed (Fase 6.2: GET /api/agents/{slug}/binary)
	for arch in amd64 arm64 arm; do \
		(cd $(PKG_BUILD_DIR)/agent && \
		 CGO_ENABLED=0 GOOS=linux GOARCH=$${arch/armv7/arm/} GOARM=$$(test $$arch = arm && echo 7 || true) \
		 go build -trimpath -ldflags "-s -w -X main.Version=$(PKG_VERSION)" \
		 -o $(PKG_BUILD_DIR)/server-go/internal/agentbin/agents/netpulse-agent-$$arch ./cmd/netpulse-agent) || true; \
	done
	# Server binary
	(cd $(PKG_BUILD_DIR)/server-go && \
	 CGO_ENABLED=0 GOOS=linux GOARCH=$(GO_ARCH_DETECT) \
	 go build -trimpath -ldflags "-s -w" \
	 -o $(PKG_BUILD_DIR)/netpulse ./cmd/netpulse)
endef

define Package/netpulse/install
	$(INSTALL_DIR) $(1)/usr/sbin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/netpulse $(1)/usr/sbin/netpulse

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/netpulse-server.init $(1)/etc/init.d/netpulse

	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/netpulse-server.config $(1)/etc/config/netpulse

	$(INSTALL_DIR) $(1)/etc/uci-defaults
	$(INSTALL_BIN) ./files/netpulse-server.defaults $(1)/etc/uci-defaults/90-netpulse-server
endef

$(eval $(call BuildPackage,netpulse))
