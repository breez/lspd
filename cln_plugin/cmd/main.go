package main

import (
	"os"

	"github.com/breez/lspd/cln_plugin"
)

func main() {
	plugin := cln_plugin.NewClnPlugin(os.Stdin, os.Stdout)
	plugin.Start()
}
