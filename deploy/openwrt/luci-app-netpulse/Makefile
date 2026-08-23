# luci-app-netpulse OpenWrt Makefile (Fase 11)
# Paquete LuCI de solo ficheros (JS/JSON/ucode, PKGARCH:=all): vista de nodo
# del agente NetPulse. Makefile plano a propósito (no luci.mk): construye en
# el SDK pelado igual que netpulse-agent, sin necesitar el feed luci en el
# host de build. Las dependencias (luci-base) se resuelven al instalar.
#
# Uso (desde el SDK o buildroot de OpenWrt):
#   cp -r luci-app-netpulse package/
#   make package/luci-app-netpulse/compile V=s
#
# O empaquetado directo sin buildroot: deploy/openwrt/package-luci.sh

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-netpulse
# ?= so the CI can override the version from the release tag via the env
# (PKG_VERSION / PKG_RELEASE); falls back to these defaults for local builds.
PKG_VERSION?=2.7.2
PKG_RELEASE?=1

PKG_LICENSE:=AGPL-3.0-only
PKG_MAINTAINER:=Nacho <netpulse@cloudless.club>
PKGARCH:=all

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-netpulse
	SECTION:=luci
	CATEGORY:=LuCI
	SUBMENU:=3. Applications
	TITLE:=Node-local NetPulse agent view for LuCI
	URL:=https://github.com/gnacho/netpulse
	DEPENDS:=+luci-base +netpulse-agent
	PKGARCH:=all
endef

define Package/luci-app-netpulse/description
	LuCI pages for the NetPulse agent on THIS router: procd status,
	binary version, RSS, heartbeat, recent syslog lines, restart button
	and editable UCI configuration, plus a link to the NetPulse web app
	(network-wide view). Modern client-rendered LuCI (JS views + ucode
	rpcd plugin); adds no HTTP listener or resident memory to the agent.
endef

# Sin compilación: todo son ficheros de texto.
define Build/Compile
endef

define Package/luci-app-netpulse/install
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/netpulse
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/netpulse/status.js $(1)/www/luci-static/resources/view/netpulse/
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/netpulse/settings.js $(1)/www/luci-static/resources/view/netpulse/

	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./files/usr/share/luci/menu.d/luci-app-netpulse.json $(1)/usr/share/luci/menu.d/

	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./files/usr/share/rpcd/acl.d/luci-app-netpulse.json $(1)/usr/share/rpcd/acl.d/

	$(INSTALL_DIR) $(1)/usr/libexec/rpcd
	$(INSTALL_BIN) ./files/usr/libexec/rpcd/luci.netpulse $(1)/usr/libexec/rpcd/luci.netpulse
endef

# rpcd descubre los plugins exec al arrancar y LuCI cachea el menú:
# recargar ambos en instalación real (no en imagebuilder, $IPKG_INSTROOT).
define Package/luci-app-netpulse/postinst
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	/etc/init.d/rpcd restart 2>/dev/null || true
	rm -f /tmp/luci-indexcache* 2>/dev/null || true
fi
exit 0
endef

define Package/luci-app-netpulse/postrm
#!/bin/sh
if [ -z "$${IPKG_INSTROOT}" ]; then
	/etc/init.d/rpcd restart 2>/dev/null || true
	rm -f /tmp/luci-indexcache* 2>/dev/null || true
fi
exit 0
endef

$(eval $(call BuildPackage,luci-app-netpulse))
