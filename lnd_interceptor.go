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
	client        *LndClient
	stopRequested bool
	initWg        sync.WaitGroup
	doneWg        sync.WaitGroup
}

func NewLndHtlcInterceptor() *LndHtlcInterceptor {
	i := &LndHtlcInterceptor{
		client: NewLndClient(),
	}

	i.initWg.Add(1)

	return i
}

func (i *LndHtlcInterceptor) Start() error {
	go forwardingHistorySynchronize(i.client)
	go channelsSynchronize(i.client)
	i.initWg.Done()
	return i.intercept()
}

func (i *LndHtlcInterceptor) Stop() error {
	i.stopRequested = true
	return nil
}

func (i *LndHtlcInterceptor) WaitStarted() LightningClient {
	i.initWg.Wait()
	return i.client
}

func (i *LndHtlcInterceptor) intercept() error {
	defer func() {
		log.Printf("LND intercept(): stopping. Waiting for in-progress interceptions to complete.")
		i.doneWg.Wait()
	}()

	for {
		if i.stopRequested {
			return nil
		}

		cancellableCtx, cancel := context.WithCancel(context.Background())
		interceptorClient, err := i.client.routerClient.HtlcInterceptor(cancellableCtx)
		if err != nil {
			log.Printf("routerClient.HtlcInterceptor(): %v", err)
			cancel()
			time.Sleep(1 * time.Second)
			continue
		}

		for {
			if i.stopRequested {
				cancel()
				return nil
			}
			request, err := interceptorClient.Recv()
			if err != nil {
				// If it is  just the error result of the context cancellation
				// the we exit silently.
				status, ok := status.FromError(err)
				if ok && status.Code() == codes.Canceled {
					break
				}
				// Otherwise it an unexpected error, we fail the test.
				log.Printf("unexpected error in interceptor.Recv() %v", err)
				cancel()
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
				case INTERCEPT_FAIL_HTLC:
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
				case INTERCEPT_FAIL_HTLC_WITH_CODE:
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey: request.IncomingCircuitKey,
						Action:             routerrpc.ResolveHoldForwardAction_FAIL,
						FailureCode:        mapFailureCode(interceptResult.failureCode),
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

		cancel()
	}
}

func mapFailureCode(original interceptFailureCode) lnrpc.Failure_FailureCode {
	switch original {
	case FAILURE_TEMPORARY_CHANNEL_FAILURE:
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	case FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS:
		return lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
	default:
		return lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
	}
}

func failForwardSend(interceptorClient routerrpc.Router_HtlcInterceptorClient, incomingCircuitKey *routerrpc.CircuitKey) {
	interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: incomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
}
