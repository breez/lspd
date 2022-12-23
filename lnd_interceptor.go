package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LndHtlcInterceptor struct {
	client *LndClient
	initWg sync.WaitGroup
	doneWg sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewLndHtlcInterceptor() *LndHtlcInterceptor {
	i := &LndHtlcInterceptor{
		client: NewLndClient(),
	}

	i.initWg.Add(1)

	return i
}

func (i *LndHtlcInterceptor) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	i.ctx = ctx
	i.cancel = cancel
	go forwardingHistorySynchronize(ctx, i.client)
	go channelsSynchronize(ctx, i.client)

	return i.intercept()
}

func (i *LndHtlcInterceptor) Stop() error {
	i.cancel()
	i.doneWg.Wait()
	return nil
}

func (i *LndHtlcInterceptor) WaitStarted() LightningClient {
	i.initWg.Wait()
	return i.client
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

			fmt.Printf("htlc: %v\nchanID: %v\nincoming amount: %v\noutgoing amount: %v\nincomin expiry: %v\noutgoing expiry: %v\npaymentHash: %x\nonionBlob: %x\n\n",
				request.IncomingCircuitKey.HtlcId,
				request.IncomingCircuitKey.ChanId,
				request.IncomingAmountMsat,
				request.OutgoingAmountMsat,
				request.IncomingExpiry,
				request.OutgoingExpiry,
				request.PaymentHash,
				request.OnionBlob,
			)

			i.doneWg.Add(1)
			go func() {
				interceptResult := intercept(request.PaymentHash, request.OutgoingAmountMsat, request.OutgoingExpiry)
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
