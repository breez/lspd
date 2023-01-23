package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/breez/lspd/cln_plugin"
)

func main() {
	plugin := cln_plugin.NewClnPlugin(os.Stdin, os.Stdout)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		// Stop everything gracefully on stop signal
		plugin.Stop()
	}()
	plugin.Start()
}
