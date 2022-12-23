package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/breez/lspd/cln_plugin"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/tlv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type ClnHtlcInterceptor struct {
	pluginAddress string
	client        *ClnClient
	pluginClient  cln_plugin.ClnPluginClient
	initWg        sync.WaitGroup
	doneWg        sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewClnHtlcInterceptor() *ClnHtlcInterceptor {
	i := &ClnHtlcInterceptor{
		pluginAddress: os.Getenv("CLN_PLUGIN_ADDRESS"),
		client:        NewClnClient(),
	}

	i.initWg.Add(1)
	return i
}

func (i *ClnHtlcInterceptor) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	log.Printf("Dialing cln plugin on '%s'", i.pluginAddress)
	conn, err := grpc.DialContext(ctx, i.pluginAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("grpc.Dial error: %v", err)
		cancel()
		return err
	}

	i.pluginClient = cln_plugin.NewClnPluginClient(conn)
	i.ctx = ctx
	i.cancel = cancel
	return i.intercept()
}

func (i *ClnHtlcInterceptor) intercept() error {
	inited := false

	defer func() {
		if !inited {
			i.initWg.Done()
		}
		log.Printf("CLN intercept(): stopping. Waiting for in-progress interceptions to complete.")
		i.doneWg.Wait()
	}()

	for {
		if i.ctx.Err() != nil {
			return i.ctx.Err()
		}

		log.Printf("Connecting CLN HTLC interceptor.")
		interceptorClient, err := i.pluginClient.HtlcStream(i.ctx)
		if err != nil {
			log.Printf("pluginClient.HtlcStream(): %v", err)
			<-time.After(time.Second)
			continue
		}

		for {
			if i.ctx.Err() != nil {
				return i.ctx.Err()
			}

			if !inited {
				inited = true
				i.initWg.Done()
			}

			request, err := interceptorClient.Recv()
			if err != nil {
				// If it is  just the error result of the context cancellation
				// the we exit silently.
				status, ok := status.FromError(err)
				if ok && status.Code() == codes.Canceled {
					log.Printf("Got code canceled. Break.")
					break
				}

				// Otherwise it an unexpected error, we fail the test.
				log.Printf("unexpected error in interceptor.Recv() %v", err)
				break
			}

			log.Printf("correlationid: %v\nhtlc: %v\nchanID: %v\nincoming amount: %v\noutgoing amount: %v\nincoming expiry: %v\noutgoing expiry: %v\npaymentHash: %v\nonionBlob: %v\n\n",
				request.Correlationid,
				request.Htlc,
				request.Onion.ShortChannelId,
				request.Htlc.AmountMsat, //with fees
				request.Onion.ForwardAmountMsat,
				request.Htlc.CltvExpiryRelative,
				request.Htlc.CltvExpiry,
				request.Htlc.PaymentHash,
				request,
			)

			i.doneWg.Add(1)
			go func() {
				interceptResult := intercept(request.Htlc.PaymentHash, request.Onion.ForwardAmountMsat, request.Htlc.CltvExpiry)
				switch interceptResult.action {
				case INTERCEPT_RESUME_WITH_ONION:
					interceptorClient.Send(i.resumeWithOnion(request, interceptResult))
				case INTERCEPT_FAIL_HTLC_WITH_CODE:
					interceptorClient.Send(&cln_plugin.HtlcResolution{
						Correlationid: request.Correlationid,
						Outcome: &cln_plugin.HtlcResolution_Fail{
							Fail: &cln_plugin.HtlcFail{
								FailureMessage: i.mapFailureCode(interceptResult.failureCode),
							},
						},
					})
				case INTERCEPT_RESUME:
					fallthrough
				default:
					interceptorClient.Send(&cln_plugin.HtlcResolution{
						Correlationid: request.Correlationid,
						Outcome: &cln_plugin.HtlcResolution_Continue{
							Continue: &cln_plugin.HtlcContinue{},
						},
					})
				}

				i.doneWg.Done()
			}()
		}

		<-time.After(time.Second)
	}
}

func (i *ClnHtlcInterceptor) Stop() error {
	i.cancel()
	i.doneWg.Wait()
	return nil
}

func (i *ClnHtlcInterceptor) WaitStarted() LightningClient {
	i.initWg.Wait()
	return i.client
}

func (i *ClnHtlcInterceptor) resumeWithOnion(request *cln_plugin.HtlcAccepted, interceptResult interceptResult) *cln_plugin.HtlcResolution {
	//decoding and encoding onion with alias in type 6 record.
	newPayload, err := encodePayloadWithNextHop(request.Onion.Payload, interceptResult.channelId)
	if err != nil {
		log.Printf("encodePayloadWithNextHop error: %v", err)
		return &cln_plugin.HtlcResolution{
			Correlationid: request.Correlationid,
			Outcome: &cln_plugin.HtlcResolution_Fail{
				Fail: &cln_plugin.HtlcFail{
					FailureMessage: i.mapFailureCode(FAILURE_TEMPORARY_CHANNEL_FAILURE),
				},
			},
		}
	}

	chanId := lnwire.NewChanIDFromOutPoint(interceptResult.channelPoint)
	log.Printf("forwarding htlc to the destination node and a new private channel was opened")
	return &cln_plugin.HtlcResolution{
		Correlationid: request.Correlationid,
		Outcome: &cln_plugin.HtlcResolution_ContinueWith{
			ContinueWith: &cln_plugin.HtlcContinueWith{
				ChannelId: chanId[:],
				Payload:   newPayload,
			},
		},
	}
}

func encodePayloadWithNextHop(payload []byte, channelId uint64) ([]byte, error) {
	bufReader := bytes.NewBuffer(payload)
	var b [8]byte
	varInt, err := sphinx.ReadVarInt(bufReader, &b)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload length %x: %v", payload, err)
	}

	innerPayload := make([]byte, varInt)
	if _, err := io.ReadFull(bufReader, innerPayload[:]); err != nil {
		return nil, fmt.Errorf("failed to decode payload %x: %v", innerPayload[:], err)
	}

	s, _ := tlv.NewStream()
	tlvMap, err := s.DecodeWithParsedTypes(bytes.NewReader(innerPayload))
	if err != nil {
		return nil, fmt.Errorf("DecodeWithParsedTypes failed for %x: %v", innerPayload[:], err)
	}

	tt := record.NewNextHopIDRecord(&channelId)
	buf := bytes.NewBuffer([]byte{})
	if err := tt.Encode(buf); err != nil {
		return nil, fmt.Errorf("failed to encode nexthop %x: %v", innerPayload[:], err)
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
		return nil, fmt.Errorf("tlv.NewStream(%x) error: %v", tlvRecords, err)
	}
	var newPayloadBuf bytes.Buffer
	err = s.Encode(&newPayloadBuf)
	if err != nil {
		return nil, fmt.Errorf("encode error: %v", err)
	}
	return newPayloadBuf.Bytes(), nil
}

func (i *ClnHtlcInterceptor) mapFailureCode(original interceptFailureCode) []byte {
	switch original {
	case FAILURE_TEMPORARY_CHANNEL_FAILURE:
		return []byte{0x10, 0x07}
	case FAILURE_TEMPORARY_NODE_FAILURE:
		return []byte{0x20, 0x02}
	case FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS:
		return []byte{0x40, 0x0F}
	default:
		log.Printf("Unknown failure code %v, default to temporary channel failure.", original)
		return []byte{0x10, 0x07} // temporary channel failure
	}
}
