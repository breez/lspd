package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type LndHtlcInterceptor struct {
	client        *LndClient
	stopRequested bool
}

func NewLndHtlcInterceptor(client *LndClient) *LndHtlcInterceptor {
	return &LndHtlcInterceptor{
		client: client,
	}
}

func (i *LndHtlcInterceptor) Start() error {
	go forwardingHistorySynchronize(i.client)
	go channelsSynchronize(i.client)
	return i.intercept()
}

func (i *LndHtlcInterceptor) Stop() error {
	i.stopRequested = true
	return nil
}

func (i *LndHtlcInterceptor) intercept() error {
	for {
		if i.stopRequested {
			return nil
		}

		cancellableCtx, cancel := context.WithCancel(context.Background())
		clientCtx := metadata.AppendToOutgoingContext(cancellableCtx, "macaroon", os.Getenv("LND_MACAROON_HEX"))
		interceptorClient, err := i.client.routerClient.HtlcInterceptor(clientCtx)
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

			interceptResult := intercept(request.PaymentHash, request.OutgoingAmountMsat, request.OutgoingExpiry)
			switch interceptResult.action {
			case INTERCEPT_RESUME_OR_CANCEL:
				go resumeOrCancel(clientCtx, interceptorClient, request.IncomingCircuitKey,
					interceptResult)
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

func resumeOrCancel(
	ctx context.Context,
	interceptorClient routerrpc.Router_HtlcInterceptorClient,
	incomingCircuitKey *routerrpc.CircuitKey,
	interceptResult interceptResult,
) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		ch, err := client.GetChannel(interceptResult.destination, *interceptResult.channelPoint)
		if err != nil {
			failForwardSend(interceptorClient, incomingCircuitKey)
			return
		}

		if uint64(ch.InitialChannelID) != 0 {
			interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
				IncomingCircuitKey:      incomingCircuitKey,
				Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
				OutgoingAmountMsat:      interceptResult.amountMsat,
				OutgoingRequestedChanId: uint64(ch.InitialChannelID),
				OnionBlob:               interceptResult.onionBlob,
			})
			err := insertChannel(uint64(ch.InitialChannelID), uint64(ch.ConfirmedChannelID), interceptResult.channelPoint.String(), interceptResult.destination, time.Now())
			if err != nil {
				log.Printf("insertChannel error: %v", err)
			}
			return
		}
		log.Printf("getChannel(%x, %v) returns 0", interceptResult.destination, interceptResult.channelPoint.String())
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%x, %v)", interceptResult.destination, interceptResult.channelPoint.String())
			break
		}
		time.Sleep(1 * time.Second)
	}
	failForwardSend(interceptorClient, incomingCircuitKey)
}
