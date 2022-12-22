package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
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
		return event.Fail(i.mapFailureCode(FAILURE_TEMPORARY_CHANNEL_FAILURE)), nil
	}

	interceptResult := intercept(paymentHashBytes, onion.ForwardAmount, uint32(event.Htlc.CltvExpiry))
	switch interceptResult.action {
	case INTERCEPT_RESUME_WITH_ONION:
		return i.resumeWithOnion(event, interceptResult), nil
	case INTERCEPT_FAIL_HTLC_WITH_CODE:
		return event.Fail(i.mapFailureCode(interceptResult.failureCode)), nil
	case INTERCEPT_RESUME:
		fallthrough
	default:
		return event.Continue(), nil
	}
}

func (i *ClnHtlcInterceptor) resumeWithOnion(event *glightning.HtlcAcceptedEvent, interceptResult interceptResult) *glightning.HtlcAcceptedResponse {
	//decoding and encoding onion with alias in type 6 record.
	newPayload, err := encodePayloadWithNextHop(event.Onion.Payload, interceptResult.channelId)
	if err != nil {
		log.Printf("encodePayloadWithNextHop error: %v", err)
		return event.Fail(i.mapFailureCode(FAILURE_TEMPORARY_CHANNEL_FAILURE))
	}

	chanId := lnwire.NewChanIDFromOutPoint(interceptResult.channelPoint)
	log.Printf("forwarding htlc to the destination node and a new private channel was opened")
	return event.ContinueWith(chanId.String(), newPayload)
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

func (i *ClnHtlcInterceptor) mapFailureCode(original interceptFailureCode) string {
	switch original {
	case FAILURE_TEMPORARY_CHANNEL_FAILURE:
		return "1007"
	case FAILURE_TEMPORARY_NODE_FAILURE:
		return "2002"
	case FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS:
		return "400F"
	default:
		log.Printf("Unknown failure code %v, default to temporary channel failure.", original)
		return "1007" // temporary channel failure
	}
}
