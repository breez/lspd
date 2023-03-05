package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/config"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LndHtlcInterceptor struct {
	config        *config.NodeConfig
	client        *LndClient
	stopRequested bool
	initWg        sync.WaitGroup
	doneWg        sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewLndHtlcInterceptor(conf *config.NodeConfig) (*LndHtlcInterceptor, error) {
	if conf.Lnd == nil {
		return nil, fmt.Errorf("missing lnd configuration")
	}
	client, err := NewLndClient(conf.Lnd)
	if err != nil {
		return nil, err
	}
	i := &LndHtlcInterceptor{
		config: conf,
		client: client,
	}

	i.initWg.Add(1)

	return i, nil
}

func (i *LndHtlcInterceptor) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	i.ctx = ctx
	i.cancel = cancel
	i.stopRequested = false
	go forwardingHistorySynchronize(ctx, i.client)
	go channelsSynchronize(ctx, i.client)

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

			nextHop := "<unknown>"
			chanInfo, err := i.client.client.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{ChanId: request.OutgoingRequestedChanId})
			if err == nil && chanInfo != nil {
				if chanInfo.Node1Pub == i.config.NodePubkey {
					nextHop = chanInfo.Node2Pub
				}
				if chanInfo.Node2Pub == i.config.NodePubkey {
					nextHop = chanInfo.Node1Pub
				}
			}

			fmt.Printf("htlc: %v\nchanID: %v\nnextHop: %v\nincoming amount: %v\noutgoing amount: %v\nincomin expiry: %v\noutgoing expiry: %v\npaymentHash: %x\nonionBlob: %x\n\n",
				request.IncomingCircuitKey.HtlcId,
				request.IncomingCircuitKey.ChanId,
				nextHop,
				request.IncomingAmountMsat,
				request.OutgoingAmountMsat,
				request.IncomingExpiry,
				request.OutgoingExpiry,
				request.PaymentHash,
				request.OnionBlob,
			)

			i.doneWg.Add(1)
			go func() {
				interceptResult := intercept(i.client, i.config, nextHop, request.PaymentHash, request.OutgoingAmountMsat, request.OutgoingExpiry, request.IncomingExpiry)
				switch interceptResult.action {
				case INTERCEPT_RESUME_WITH_ONION:
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey:      request.IncomingCircuitKey,
						Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
						OutgoingAmountMsat:      interceptResult.amountMsat,
						OutgoingRequestedChanId: uint64(interceptResult.channelId),
						OnionBlob:               interceptResult.onionBlob,
					})
				case INTERCEPT_FAIL_HTLC_WITH_CODE:
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey: request.IncomingCircuitKey,
						Action:             routerrpc.ResolveHoldForwardAction_FAIL,
						FailureCode:        i.mapFailureCode(interceptResult.failureCode),
					})
				case INTERCEPT_RESUME:
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

func (i *LndHtlcInterceptor) mapFailureCode(original interceptFailureCode) lnrpc.Failure_FailureCode {
	switch original {
	case FAILURE_TEMPORARY_CHANNEL_FAILURE:
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	case FAILURE_TEMPORARY_NODE_FAILURE:
		return lnrpc.Failure_TEMPORARY_NODE_FAILURE
	case FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS:
		return lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
	default:
		log.Printf("Unknown failure code %v, default to temporary channel failure.", original)
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	}
}
