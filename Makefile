PKG := github.com/breez/lspd
TAG := $(shell git describe --tags --dirty)

release-all: release-lspd release-plugin

release-lspd:
	go get $(PKG)
	go build -v -trimpath -o lspd -ldflags "-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)

release-plugin:
	go get $(PKG)/cln_plugin/cmd
	go build -v -trimpath -o lspd_cln_plugin -ldflags="-s -w -X $(PKG)/build.tag=$(TAG)" $(PKG)/cln_plugin/cmd

clean:
	rm -f lspd
	rm -f lspd_cln_plugin
