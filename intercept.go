package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	sphinx "github.com/lightningnetwork/lightning-onion"
)

func checkPayment(incomingAmountMsat, outgoingAmountMsat int64) error {
	fees := incomingAmountMsat * channelFeePermyriad / 10_000 / 1_000 * 1_000
	if fees < channelMinimumFeeMsat {
		fees = channelMinimumFeeMsat
	}
	if incomingAmountMsat-outgoingAmountMsat < fees {
		return fmt.Errorf("not enough fees")
	}
	return nil
}

func openChannel(client LightningClient, paymentHash, destination []byte, incomingAmountMsat int64) (*wire.OutPoint, error) {
	capacity := incomingAmountMsat/1000 + additionalChannelCapacity
	if capacity == publicChannelAmount {
		capacity++
	}
	channelPoint, err := client.OpenChannel(&OpenChannelRequest{
		Destination: destination,
		CapacitySat: uint64(capacity),
		TargetConf:  6,
		IsPrivate:   true,
		IsZeroConf:  true,
	})
	if err != nil {
		log.Printf("client.OpenChannelSync(%x, %v) error: %v", destination, capacity, err)
		return nil, err
	}
	sendOpenChannelEmailNotification(
		paymentHash,
		incomingAmountMsat,
		destination,
		capacity,
		channelPoint.String(),
	)
	err = setFundingTx(paymentHash, channelPoint)
	return channelPoint, err
}

func failForwardSend(interceptorClient routerrpc.Router_HtlcInterceptorClient, incomingCircuitKey *routerrpc.CircuitKey) {
	interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: incomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
}

func intercept(client *LndClient) {
	for {
		cancellableCtx, cancel := context.WithCancel(context.Background())
		clientCtx := metadata.AppendToOutgoingContext(cancellableCtx, "macaroon", os.Getenv("LND_MACAROON_HEX"))
		interceptorClient, err := client.routerClient.HtlcInterceptor(clientCtx)
		if err != nil {
			log.Printf("routerClient.HtlcInterceptor(): %v", err)
			cancel()
			time.Sleep(1 * time.Second)
			continue
		}

		for {
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

			paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, channelPoint, err := paymentInfo(request.PaymentHash)
			if err != nil {
				log.Printf("paymentInfo(%x) error: %v", request.PaymentHash, err)
				failForwardSend(interceptorClient, request.IncomingCircuitKey)
				continue
			}
			log.Printf("paymentHash:%x\npaymentSecret:%x\ndestination:%x\nincomingAmountMsat:%v\noutgoingAmountMsat:%v\n\n",
				paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat)
			if paymentSecret != nil {

				if channelPoint == nil {
					if bytes.Compare(paymentHash, request.PaymentHash) == 0 {
						channelPoint, err = openChannel(client, request.PaymentHash, destination, incomingAmountMsat)
						log.Printf("openChannel(%x, %v) err: %v", destination, incomingAmountMsat, err)
						if err != nil {
							failForwardSend(interceptorClient, request.IncomingCircuitKey)
							continue
						}
					} else { //probing
						failureCode := lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE
						isConnected, _ := client.IsConnected(destination)
						if err != nil || !*isConnected {
							failureCode = lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
						}

						interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
							IncomingCircuitKey: request.IncomingCircuitKey,
							Action:             routerrpc.ResolveHoldForwardAction_FAIL,
							FailureCode:        failureCode,
						})
						continue
					}
				}

				pubKey, err := btcec.ParsePubKey(destination)
				if err != nil {
					log.Printf("btcec.ParsePubKey(%x): %v", destination, err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}

				sessionKey, err := btcec.NewPrivateKey()
				if err != nil {
					log.Printf("btcec.NewPrivateKey(): %v", err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}

				var bigProd, bigAmt big.Int
				amt := (bigAmt.Div(bigProd.Mul(big.NewInt(outgoingAmountMsat), big.NewInt(int64(request.OutgoingAmountMsat))), big.NewInt(incomingAmountMsat))).Int64()

				var addr [32]byte
				copy(addr[:], paymentSecret)
				hop := route.Hop{
					AmtToForward:     lnwire.MilliSatoshi(amt),
					OutgoingTimeLock: request.OutgoingExpiry,
					MPP:              record.NewMPP(lnwire.MilliSatoshi(outgoingAmountMsat), addr),
					CustomRecords:    make(record.CustomSet),
				}

				var b bytes.Buffer
				err = hop.PackHopPayload(&b, uint64(0))
				if err != nil {
					log.Printf("hop.PackHopPayload(): %v", err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}

				payload, err := sphinx.NewHopPayload(nil, b.Bytes())
				if err != nil {
					log.Printf("sphinx.NewHopPayload(): %v", err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}

				var sphinxPath sphinx.PaymentPath
				sphinxPath[0] = sphinx.OnionHop{
					NodePub:    *pubKey,
					HopPayload: payload,
				}
				sphinxPacket, err := sphinx.NewOnionPacket(
					&sphinxPath, sessionKey, request.PaymentHash,
					sphinx.DeterministicPacketFiller,
				)
				if err != nil {
					log.Printf("sphinx.NewOnionPacket(): %v", err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}
				var onionBlob bytes.Buffer
				err = sphinxPacket.Encode(&onionBlob)
				if err != nil {
					log.Printf("sphinxPacket.Encode(): %v", err)
					failForwardSend(interceptorClient, request.IncomingCircuitKey)
					continue
				}

				go resumeOrCancel(
					clientCtx, interceptorClient, request.IncomingCircuitKey, destination,
					*channelPoint, uint64(amt), onionBlob.Bytes(),
				)

			} else {
				interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
					IncomingCircuitKey:      request.IncomingCircuitKey,
					Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
					OutgoingAmountMsat:      request.OutgoingAmountMsat,
					OutgoingRequestedChanId: request.OutgoingRequestedChanId,
					OnionBlob:               request.OnionBlob,
				})
			}
		}
	}
}

func resumeOrCancel(
	ctx context.Context,
	interceptorClient routerrpc.Router_HtlcInterceptorClient,
	incomingCircuitKey *routerrpc.CircuitKey,
	destination []byte,
	channelPoint wire.OutPoint,
	outgoingAmountMsat uint64,
	onionBlob []byte,
) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		ch, err := client.GetChannel(destination, channelPoint)
		if err != nil {
			failForwardSend(interceptorClient, incomingCircuitKey)
			return
		}

		if uint64(ch.InitialChannelID) != 0 {
			interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
				IncomingCircuitKey:      incomingCircuitKey,
				Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
				OutgoingAmountMsat:      outgoingAmountMsat,
				OutgoingRequestedChanId: uint64(ch.InitialChannelID),
				OnionBlob:               onionBlob,
			})
			err := insertChannel(uint64(ch.InitialChannelID), uint64(ch.ConfirmedChannelID), channelPoint.String(), destination, time.Now())
			if err != nil {
				log.Printf("insertChannel error: %v", err)
			}
			return
		}
		log.Printf("getChannel(%x, %v) returns 0", destination, channelPoint)
		if time.Now().After(deadline) {
			log.Printf("Stop retrying getChannel(%x, %v)", destination, channelPoint)
			break
		}
		time.Sleep(1 * time.Second)
	}
	failForwardSend(interceptorClient, incomingCircuitKey)
}
