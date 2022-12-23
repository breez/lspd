package cln_plugin

import (
	"encoding/hex"
	"log"
	"os"

	"github.com/breez/lspd/basetypes"
	"github.com/niftynei/glightning/glightning"
)

type ClnPlugin struct {
	server *server
	plugin *glightning.Plugin
}

func NewClnPlugin(server *server) *ClnPlugin {
	c := &ClnPlugin{
		server: server,
	}

	c.plugin = glightning.NewPlugin(c.onInit)
	c.plugin.RegisterHooks(&glightning.Hooks{
		HtlcAccepted: c.onHtlcAccepted,
	})

	return c
}

func (c *ClnPlugin) Start() error {
	err := c.plugin.Start(os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("Plugin error: %v", err)
		return err
	}

	return nil
}

func (c *ClnPlugin) Stop() {
	c.plugin.Stop()
	c.server.Stop()
}

func (c *ClnPlugin) onInit(plugin *glightning.Plugin, options map[string]glightning.Option, config *glightning.Config) {
	log.Printf("successfully init'd! %v\n", config.RpcFile)

	log.Printf("Starting htlc grpc server.")
	go func() {
		err := c.server.Start()
		if err == nil {
			log.Printf("WARNING server stopped.")
		} else {
			log.Printf("ERROR Server stopped with error: %v", err)
		}
	}()

	//lightning server
	clientcln := glightning.NewLightning()
	clientcln.SetTimeout(60)
	clientcln.StartUp(config.RpcFile, config.LightningDir)

	log.Printf("successfull clientcln.StartUp")
}

func (c *ClnPlugin) onHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	payload, err := hex.DecodeString(event.Onion.Payload)
	if err != nil {
		log.Printf("ERROR failed to decode payload %s: %v", event.Onion.Payload, err)
		return nil, err
	}
	scid, err := basetypes.NewShortChannelIDFromString(event.Onion.ShortChannelId)
	if err != nil {
		log.Printf("ERROR failed to decode short channel id %s: %v", event.Onion.ShortChannelId, err)
		return nil, err
	}
	ph, err := hex.DecodeString(event.Htlc.PaymentHash)
	if err != nil {
		log.Printf("ERROR failed to decode payment hash %s: %v", event.Onion.ShortChannelId, err)
		return nil, err
	}

	resp := c.server.Send(&HtlcAccepted{
		Onion: &Onion{
			Payload:           payload,
			ShortChannelId:    uint64(*scid),
			ForwardAmountMsat: event.Onion.ForwardAmount,
		},
		Htlc: &HtlcOffer{
			AmountMsat:         event.Htlc.AmountMilliSatoshi,
			CltvExpiryRelative: uint32(event.Htlc.CltvExpiryRelative),
			CltvExpiry:         uint32(event.Htlc.CltvExpiry),
			PaymentHash:        ph,
		},
	})

	_, ok := resp.Outcome.(*HtlcResolution_Continue)
	if ok {
		return event.Continue(), nil
	}

	cont, ok := resp.Outcome.(*HtlcResolution_ContinueWith)
	if ok {
		chanId := hex.EncodeToString(cont.ContinueWith.ChannelId)
		pl := hex.EncodeToString(cont.ContinueWith.Payload)
		return event.ContinueWith(chanId, pl), nil
	}

	fail, ok := resp.Outcome.(*HtlcResolution_Fail)
	if ok {
		fm := hex.EncodeToString(fail.Fail.FailureMessage)
		return event.Fail(fm), err
	}

	log.Printf("Unexpected htlc resolution type %T: %+v", resp.Outcome, resp.Outcome)
	return event.Fail("1007"), nil // temporary channel failure
}
