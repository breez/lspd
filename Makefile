PKG := github.com/breez/lspd
TAG := $(shell git describe --tags --dirty)

release-all: release-lspd release-plugin

release-lspd:
	go get $(PKG)/cmd/lspd
	go build -v -trimpath -o lspd -ldflags "-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd

release-plugin:
	go get $(PKG)/cmd/lspd_cln_plugin
	go build -v -trimpath -o lspd_cln_plugin -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cmd/lspd_cln_plugin

clean:
	rm -f lspd
	rm -f lspd_cln_plugin
