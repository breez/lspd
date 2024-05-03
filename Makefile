OUT_DIR := .
PKG := github.com/breez/lspd
TAG := $(shell git describe --tags --dirty)

release-all: release-lspd release-plugin release-cli

release-lspd:
	CGO_ENABLED=0 go build -v -trimpath -o $(OUT_DIR)/lspd -ldflags "-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd

release-plugin:
	CGO_ENABLED=0 go build -v -trimpath -o $(OUT_DIR)/lspd_cln_plugin -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd_cln_plugin

release-cli:
	CGO_ENABLED=0 go build -v -trimpath -o $(OUT_DIR)/lspd_revenue_cli -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd_revenue_cli

clean:
	rm -f $(out_dir)/lspd
	rm -f $(OUT_DIR)/lspd_cln_plugin
	rm -f $(OUT_DIR)/lspd_revenue_cli
