package main

import (
	"log"
	"os"
	"sync"

	"github.com/niftynei/glightning/glightning"
)

//C-lightning plugin functions
func StartPlugin() {
	//c-lightning plugin initiate
	plugin := glightning.NewPlugin(onInit)

	plugin.RegisterHooks(&glightning.Hooks{
		HtlcAccepted: OnHtlcAccepted,
	})

	err := plugin.Start(os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("start plugin error: %v", err)
	}
}
func onInit(plugin *glightning.Plugin, options map[string]glightning.Option, config *glightning.Config) {
	log.Printf("successfully init'd! %s\n", config.RpcFile)
}

func OnHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	log.Printf("htlc_accepted called\n")

	onion := event.Onion
	log.Printf("has perhop? %t", onion.PerHop != nil)
	log.Printf("type is %s", onion.Type)

	var on string
	if onion.PaymentSecret == "" {
		on = ""
	} else {
		on = "not "
	}
	log.Printf("payment secret is %sempty", on)

	if onion.TotalMilliSatoshi == "" {
		on = "empty"
	} else {
		on = onion.TotalMilliSatoshi
	}
	log.Printf("amount is %s", on)

	return event.Continue(), nil
}

func run_clightning() {
	var wg sync.WaitGroup
	//c-lightning plugin started
	wg.Add(1)
	go StartPlugin()
	wg.Wait() //wait for greenlight intercep
}
