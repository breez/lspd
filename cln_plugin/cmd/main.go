package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/breez/lspd/cln_plugin"
)

func main() {
	listen := os.Getenv("LISTEN_ADDRESS")
	server := cln_plugin.NewServer(listen)
	plugin := cln_plugin.NewClnPlugin(server)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT)
	go func() {
		sig := <-c
		log.Printf("Received stop signal %v. Stopping.", sig)
		plugin.Stop()
	}()

	err := plugin.Start()
	if err == nil {
		log.Printf("cln plugin stopped.")
	} else {
		log.Printf("cln plugin stopped with error: %v", err)
	}
}
