PKG := github.com/breez/lspd
TAG := $(shell git describe --tags --dirty)

release-all: release-lspd release-plugin release-cli

release-lspd:
	go get $(PKG)/cmd/lspd
	go build -v -trimpath -o lspd -ldflags "-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd

release-plugin:
	go get $(PKG)/cmd/lspd_cln_plugin
	go build -v -trimpath -o lspd_cln_plugin -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd_cln_plugin

release-cli:
	go get $(PKG)/cmd/lspd_revenue_cli
	go build -v -trimpath -o lspd_revenue_cli -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd_revenue_cli

clean:
	rm -f lspd
	rm -f lspd_cln_plugin
	rm -f lspd_revenue_cli
