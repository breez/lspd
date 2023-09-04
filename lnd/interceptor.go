package lnd

import (
	"bytes"
	"context"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/btcec/v2"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LndHtlcInterceptor struct {
	fwsync        *ForwardingHistorySync
	interceptor   *interceptor.Interceptor
	config        *config.NodeConfig
	client        *LndClient
	stopRequested bool
	initWg        sync.WaitGroup
	doneWg        sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewLndHtlcInterceptor(
	conf *config.NodeConfig,
	client *LndClient,
	fwsync *ForwardingHistorySync,
	interceptor *interceptor.Interceptor,
) (*LndHtlcInterceptor, error) {
	i := &LndHtlcInterceptor{
		config:      conf,
		client:      client,
		fwsync:      fwsync,
		interceptor: interceptor,
	}

	i.initWg.Add(1)

	return i, nil
}

func (i *LndHtlcInterceptor) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	i.ctx = ctx
	i.cancel = cancel
	i.stopRequested = false
	go i.fwsync.ForwardingHistorySynchronize(ctx)
	go i.fwsync.ChannelsSynchronize(ctx)

	return i.intercept()
}

func (i *LndHtlcInterceptor) Stop() error {
	// Setting stopRequested to true will make the interceptor stop receiving.
	i.stopRequested = true

	// Wait until all already received htlcs are handled, responses sent back.
	i.doneWg.Wait()

	// Close the grpc connection.
	i.cancel()
	return nil
}

func (i *LndHtlcInterceptor) WaitStarted() {
	i.initWg.Wait()
}

func (i *LndHtlcInterceptor) intercept() error {
	inited := false
	defer func() {
		if !inited {
			i.initWg.Done()
		}
		log.Printf("LND intercept(): stopping. Waiting for in-progress interceptions to complete.")
		i.doneWg.Wait()
	}()

	for {
		if i.ctx.Err() != nil {
			return i.ctx.Err()
		}

		log.Printf("Connecting LND HTLC interceptor.")
		interceptorClient, err := i.client.routerClient.HtlcInterceptor(i.ctx)
		if err != nil {
			log.Printf("routerClient.HtlcInterceptor(): %v", err)
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

			// Stop receiving if stop if requested. The defer func on top of this
			// function will assure all htlcs that are currently being processed
			// will complete.
			if i.stopRequested {
				return nil
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

			i.doneWg.Add(1)
			go func() {
				scid := lightning.ShortChannelID(request.OutgoingRequestedChanId)
				interceptResult := i.interceptor.Intercept(shared.InterceptRequest{
					Identifier:         request.IncomingCircuitKey.String(),
					Scid:               scid,
					PaymentHash:        request.PaymentHash,
					IncomingAmountMsat: request.IncomingAmountMsat,
					OutgoingAmountMsat: request.OutgoingAmountMsat,
					IncomingExpiry:     request.IncomingExpiry,
					OutgoingExpiry:     request.OutgoingExpiry,
				})
				switch interceptResult.Action {
				case shared.INTERCEPT_RESUME_WITH_ONION:
					onion, err := i.constructOnion(interceptResult, request.OutgoingExpiry, request.PaymentHash)
					if err == nil {
						interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
							IncomingCircuitKey:      request.IncomingCircuitKey,
							Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
							OutgoingAmountMsat:      interceptResult.AmountMsat,
							OutgoingRequestedChanId: uint64(interceptResult.Scid),
							OnionBlob:               onion,
						})
					} else {
						interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
							IncomingCircuitKey: request.IncomingCircuitKey,
							Action:             routerrpc.ResolveHoldForwardAction_FAIL,
							FailureCode:        lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE,
						})
					}

				case shared.INTERCEPT_FAIL_HTLC_WITH_CODE:
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey: request.IncomingCircuitKey,
						Action:             routerrpc.ResolveHoldForwardAction_FAIL,
						FailureCode:        i.mapFailureCode(interceptResult.FailureCode),
					})
				case shared.INTERCEPT_RESUME:
					fallthrough
				default:
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey:      request.IncomingCircuitKey,
						Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
						OutgoingAmountMsat:      request.OutgoingAmountMsat,
						OutgoingRequestedChanId: request.OutgoingRequestedChanId,
						OnionBlob:               request.OnionBlob,
					})
				}

				i.doneWg.Done()
			}()
		}

		<-time.After(time.Second)
	}
}

func (i *LndHtlcInterceptor) mapFailureCode(original shared.InterceptFailureCode) lnrpc.Failure_FailureCode {
	switch original {
	case shared.FAILURE_TEMPORARY_CHANNEL_FAILURE:
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	case shared.FAILURE_TEMPORARY_NODE_FAILURE:
		return lnrpc.Failure_TEMPORARY_NODE_FAILURE
	case shared.FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS:
		return lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
	default:
		log.Printf("Unknown failure code %v, default to temporary channel failure.", original)
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	}
}

func (i *LndHtlcInterceptor) constructOnion(
	interceptResult shared.InterceptResult,
	reqOutgoingExpiry uint32,
	reqPaymentHash []byte,
) ([]byte, error) {
	pubKey, err := btcec.ParsePubKey(interceptResult.Destination)
	if err != nil {
		log.Printf("btcec.ParsePubKey(%x): %v", interceptResult.Destination, err)
		return nil, err
	}

	sessionKey, err := btcec.NewPrivateKey()
	if err != nil {
		log.Printf("btcec.NewPrivateKey(): %v", err)
		return nil, err
	}

	var addr [32]byte
	copy(addr[:], interceptResult.PaymentSecret)
	hop := route.Hop{
		AmtToForward:     lnwire.MilliSatoshi(interceptResult.AmountMsat),
		OutgoingTimeLock: reqOutgoingExpiry,
		MPP:              record.NewMPP(lnwire.MilliSatoshi(interceptResult.TotalAmountMsat), addr),
		CustomRecords:    make(record.CustomSet),
	}

	var b bytes.Buffer
	err = hop.PackHopPayload(&b, uint64(0))
	if err != nil {
		log.Printf("hop.PackHopPayload(): %v", err)
		return nil, err
	}

	payload, err := sphinx.NewHopPayload(nil, b.Bytes())
	if err != nil {
		log.Printf("sphinx.NewHopPayload(): %v", err)
		return nil, err
	}

	var sphinxPath sphinx.PaymentPath
	sphinxPath[0] = sphinx.OnionHop{
		NodePub:    *pubKey,
		HopPayload: payload,
	}
	sphinxPacket, err := sphinx.NewOnionPacket(
		&sphinxPath, sessionKey, reqPaymentHash,
		sphinx.DeterministicPacketFiller,
	)
	if err != nil {
		log.Printf("sphinx.NewOnionPacket(): %v", err)
		return nil, err
	}
	var onionBlob bytes.Buffer
	err = sphinxPacket.Encode(&onionBlob)
	if err != nil {
		log.Printf("sphinxPacket.Encode(): %v", err)
		return nil, err
	}

	return onionBlob.Bytes(), nil
}
