include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-airsim
PKG_VERSION:=0.1.0
PKG_RELEASE:=21
PKG_LICENSE:=PolyForm-Noncommercial-1.0.0 GPL-2.0-only GPL-2.0-or-later LGPL-2.1-or-later MIT BSD-3-Clause

include $(INCLUDE_DIR)/package.mk

define Package/luci-app-airsim
  SECTION:=luci
  CATEGORY:=LuCI
  SUBMENU:=3. Applications
  TITLE:=AirSIM for MU300
  DEPENDS:=+luci-base +rpcd +uclient-fetch +ca-bundle
endef

define Package/luci-app-airsim/description
  AirSIM Agent, MU300 PCM bridge, encrypted client pairing and LuCI management.
  Requires mu300-linux OpenWrt and a matching mu300-audio installation.
endef

define Build/Compile
	$(if $(wildcard $(CURDIR)/bin/airsim-agent),,$(error Run scripts/build.sh and copy out/linux-arm64 into packaging/openwrt/bin first))
endef

define Package/luci-app-airsim/conffiles
/etc/config/airsim
endef

define Package/luci-app-airsim/install
	$(CP) ./root/* $(1)/
	$(INSTALL_DIR) $(1)/usr/lib/airsim
	$(INSTALL_BIN) ./bin/airsim-agent $(1)/usr/lib/airsim/
	$(INSTALL_BIN) ./bin/airsim-mu300d $(1)/usr/lib/airsim/
endef

$(eval $(call BuildPackage,luci-app-airsim))
