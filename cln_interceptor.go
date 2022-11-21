package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/tlv"
	"github.com/niftynei/glightning/glightning"
)

type ClnHtlcInterceptor struct {
	client *ClnClient
	plugin *glightning.Plugin
	initWg sync.WaitGroup
}

func NewClnHtlcInterceptor() *ClnHtlcInterceptor {
	i := &ClnHtlcInterceptor{}

	i.initWg.Add(1)
	return i
}

func (i *ClnHtlcInterceptor) Start() error {
	//c-lightning plugin initiate
	plugin := glightning.NewPlugin(i.onInit)
	i.plugin = plugin
	plugin.RegisterHooks(&glightning.Hooks{
		HtlcAccepted: i.OnHtlcAccepted,
	})

	err := plugin.Start(os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("Plugin error: %v", err)
		return err
	}

	return nil
}

func (i *ClnHtlcInterceptor) Stop() error {
	plugin := i.plugin
	if plugin != nil {
		plugin.Stop()
	}

	return nil
}

func (i *ClnHtlcInterceptor) WaitStarted() LightningClient {
	i.initWg.Wait()
	return i.client
}

func (i *ClnHtlcInterceptor) onInit(plugin *glightning.Plugin, options map[string]glightning.Option, config *glightning.Config) {
	log.Printf("successfully init'd! %v\n", config.RpcFile)

	//lightning server
	clientcln := glightning.NewLightning()
	clientcln.SetTimeout(60)
	clientcln.StartUp(config.RpcFile, config.LightningDir)

	i.client = &ClnClient{
		client: clientcln,
	}

	log.Printf("successfull clientcln.StartUp")
	i.initWg.Done()
}

func (i *ClnHtlcInterceptor) OnHtlcAccepted(event *glightning.HtlcAcceptedEvent) (*glightning.HtlcAcceptedResponse, error) {
	log.Printf("htlc_accepted called\n")
	onion := event.Onion

	log.Printf("htlc: %v\nchanID: %v\nincoming amount: %v\noutgoing amount: %v\nincoming expiry: %v\noutgoing expiry: %v\npaymentHash: %v\nonionBlob: %v\n\n",
		event.Htlc,
		onion.ShortChannelId,
		event.Htlc.AmountMilliSatoshi, //with fees
		onion.ForwardAmount,
		event.Htlc.CltvExpiryRelative,
		event.Htlc.CltvExpiry,
		event.Htlc.PaymentHash,
		onion,
	)

	// fail htlc in case payment hash is not valid.
	paymentHashBytes, err := hex.DecodeString(event.Htlc.PaymentHash)
	if err != nil {
		log.Printf("hex.DecodeString(%v) error: %v", event.Htlc.PaymentHash, err)
		return event.Fail(uint16(FAILURE_TEMPORARY_CHANNEL_FAILURE)), nil
	}

	interceptResult := intercept(paymentHashBytes, onion.ForwardAmount, uint32(event.Htlc.CltvExpiry))
	switch interceptResult.action {
	case INTERCEPT_RESUME_OR_CANCEL:
		return i.resumeOrCancel(event, interceptResult), nil
	case INTERCEPT_FAIL_HTLC:
		return event.Fail(uint16(FAILURE_TEMPORARY_CHANNEL_FAILURE)), nil
	case INTERCEPT_FAIL_HTLC_WITH_CODE:
		return event.Fail(uint16(interceptResult.failureCode)), nil
	case INTERCEPT_RESUME:
		fallthrough
	default:
		return event.Continue(), nil
	}
}

func (i *ClnHtlcInterceptor) resumeOrCancel(event *glightning.HtlcAcceptedEvent, interceptResult interceptResult) *glightning.HtlcAcceptedResponse {
	deadline := time.Now().Add(60 * time.Second)

	for {
		chanResult, _ := i.client.GetChannel(interceptResult.destination, *interceptResult.channelPoint)
		if chanResult != nil {
			log.Printf("channel opended successfully alias: %v, confirmed: %v", chanResult.InitialChannelID.ToString(), chanResult.ConfirmedChannelID.ToString())

			err := insertChannel(
				uint64(chanResult.InitialChannelID),
				uint64(chanResult.ConfirmedChannelID),
				interceptResult.channelPoint.String(),
				interceptResult.destination,
				time.Now(),
			)

			if err != nil {
				log.Printf("insertChannel error: %v", err)
				return event.Fail(uint16(FAILURE_TEMPORARY_CHANNEL_FAILURE))
			}

			channelID := uint64(chanResult.ConfirmedChannelID)
			if channelID == 0 {
				channelID = uint64(chanResult.InitialChannelID)
			}
			//decoding and encoding onion with alias in type 6 record.
			newPayload, err := encodePayloadWithNextHop(event.Onion.Payload, channelID)
			if err != nil {
				log.Printf("encodePayloadWithNextHop error: %v", err)
				return event.Fail(uint16(FAILURE_TEMPORARY_CHANNEL_FAILURE))
			}

			log.Printf("forwarding htlc to the destination node and a new private channel was opened")
			return event.ContinueWithPayload(newPayload)
		}

		log.Printf("waiting for channel to get opened.... %v\n", interceptResult.destination)
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%v, %v)", interceptResult.destination, interceptResult.channelPoint.String())
			break
		}
		time.Sleep(1 * time.Second)
	}
	log.Printf("Error: Channel failed to opened... timed out. ")
	return event.Fail(uint16(FAILURE_TEMPORARY_CHANNEL_FAILURE))
}

func encodePayloadWithNextHop(payloadHex string, channelId uint64) (string, error) {
	payload, err := hex.DecodeString(payloadHex)
	if err != nil {
		log.Printf("failed to decode types. error: %v", err)
		return "", err
	}
	bufReader := bytes.NewBuffer(payload)
	var b [8]byte
	varInt, err := sphinx.ReadVarInt(bufReader, &b)
	if err != nil {
		return "", fmt.Errorf("failed to read payload length %v: %v", payloadHex, err)
	}

	innerPayload := make([]byte, varInt)
	if _, err := io.ReadFull(bufReader, innerPayload[:]); err != nil {
		return "", fmt.Errorf("failed to decode payload %x: %v", innerPayload[:], err)
	}

	s, _ := tlv.NewStream()
	tlvMap, err := s.DecodeWithParsedTypes(bytes.NewReader(innerPayload))
	if err != nil {
		return "", fmt.Errorf("DecodeWithParsedTypes failed for %x: %v", innerPayload[:], err)
	}

	tt := record.NewNextHopIDRecord(&channelId)
	buf := bytes.NewBuffer([]byte{})
	if err := tt.Encode(buf); err != nil {
		return "", fmt.Errorf("failed to encode nexthop %x: %v", innerPayload[:], err)
	}

	uTlvMap := make(map[uint64][]byte)
	for t, b := range tlvMap {
		if t == record.NextHopOnionType {
			uTlvMap[uint64(t)] = buf.Bytes()
			continue
		}
		uTlvMap[uint64(t)] = b
	}
	tlvRecords := tlv.MapToRecords(uTlvMap)
	s, err = tlv.NewStream(tlvRecords...)
	if err != nil {
		return "", fmt.Errorf("tlv.NewStream(%x) error: %v", tlvRecords, err)
	}
	var newPayloadBuf bytes.Buffer
	err = s.Encode(&newPayloadBuf)
	if err != nil {
		return "", fmt.Errorf("encode error: %v", err)
	}
	return hex.EncodeToString(newPayloadBuf.Bytes()), nil
}
