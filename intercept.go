package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
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

func isConnected(ctx context.Context, client lnrpc.LightningClient, destination []byte) error {
	pubKey := hex.EncodeToString(destination)
	r, err := client.ListPeers(ctx, &lnrpc.ListPeersRequest{LatestError: true})
	if err != nil {
		log.Printf("client.ListPeers() error: %v", err)
		return fmt.Errorf("client.ListPeers() error: %w", err)
	}
	for _, peer := range r.Peers {
		if pubKey == peer.PubKey {
			log.Printf("destination online: %x", destination)
			return nil
		}
	}
	log.Printf("destination offline: %x", destination)
	return fmt.Errorf("destination offline")
}

func openChannel(ctx context.Context, client lnrpc.LightningClient, paymentHash, destination []byte, incomingAmountMsat int64) ([]byte, uint32, error) {
	capacity := incomingAmountMsat/1000 + additionalChannelCapacity
	if capacity == publicChannelAmount {
		capacity++
	}
	channelPoint, err := client.OpenChannelSync(ctx, &lnrpc.OpenChannelRequest{
		NodePubkey:         destination,
		LocalFundingAmount: capacity,
		TargetConf:         6,
		Private:            true,
	})
	if err != nil {
		log.Printf("client.OpenChannelSync(%x, %v) error: %v", destination, capacity, err)
		return nil, 0, err
	}
	sendOpenChannelEmailNotification(
		paymentHash,
		incomingAmountMsat,
		destination,
		capacity,
		channelPoint.GetFundingTxidBytes(),
		channelPoint.OutputIndex,
	)
	err = setFundingTx(paymentHash, channelPoint.GetFundingTxidBytes(), uint32(channelPoint.OutputIndex))
	return channelPoint.GetFundingTxidBytes(), channelPoint.OutputIndex, err
}

func getChannel(ctx context.Context, client lnrpc.LightningClient, node []byte, channelPoint string) uint64 {
	r, err := client.ListChannels(ctx, &lnrpc.ListChannelsRequest{Peer: node})
	if err != nil {
		log.Printf("client.ListChannels(%x) error: %v", node, err)
		return 0
	}
	for _, c := range r.Channels {
		log.Printf("getChannel(%x): %v", node, c.ChanId)
		if c.ChannelPoint == channelPoint && c.Active {
			return c.ChanId
		}
	}
	log.Printf("No channel found: getChannel(%x)", node)
	return 0
}

func intercept() {
	for {
		cancellableCtx, cancel := context.WithCancel(context.Background())
		clientCtx := metadata.AppendToOutgoingContext(cancellableCtx, "macaroon", os.Getenv("LND_MACAROON_HEX"))
		interceptorClient, err := routerClient.HtlcInterceptor(clientCtx)
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

			paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum, err := paymentInfo(request.PaymentHash)
			if err != nil {
				log.Printf("paymentInfo(%x) error: %v", request.PaymentHash, err)
			}
			log.Printf("paymentHash:%x\npaymentSecret:%x\ndestination:%x\nincomingAmountMsat:%v\noutgoingAmountMsat:%v\n\n",
				paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat)
			if paymentSecret != nil {

				if fundingTxID == nil {
					if bytes.Compare(paymentHash, request.PaymentHash) == 0 {
						fundingTxID, fundingTxOutnum, err = openChannel(clientCtx, client, request.PaymentHash, destination, incomingAmountMsat)
						log.Printf("openChannel(%x, %v) err: %v", destination, incomingAmountMsat, err)
						if err != nil {
							interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
								IncomingCircuitKey: request.IncomingCircuitKey,
								Action:             routerrpc.ResolveHoldForwardAction_FAIL,
							})
							continue
						}
					} else { //probing
						failureCode := routerrpc.ForwardHtlcInterceptResponse_TEMPORARY_CHANNEL_FAILURE
						if err := isConnected(clientCtx, client, destination); err == nil {
							failureCode = routerrpc.ForwardHtlcInterceptResponse_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS
						}
						interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
							IncomingCircuitKey: request.IncomingCircuitKey,
							Action:             routerrpc.ResolveHoldForwardAction_FAIL,
							FailureCode:        failureCode,
						})
						continue
					}
				}

				pubKey, err := btcec.ParsePubKey(destination, btcec.S256())
				log.Printf("btcec.ParsePubKey(%x): %v", destination, err)

				sessionKey, err := btcec.NewPrivateKey(btcec.S256())
				log.Printf("btcec.NewPrivateKey(): %v", err)

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
				log.Printf("hop.PackHopPayload(): %v", err)

				payload, err := sphinx.NewHopPayload(nil, b.Bytes())
				log.Printf("sphinx.NewHopPayload(): %v", err)

				var sphinxPath sphinx.PaymentPath
				sphinxPath[0] = sphinx.OnionHop{
					NodePub:    *pubKey,
					HopPayload: payload,
				}
				sphinxPacket, err := sphinx.NewOnionPacket(
					&sphinxPath, sessionKey, request.PaymentHash,
					sphinx.DeterministicPacketFiller,
				)
				var onionBlob bytes.Buffer
				err = sphinxPacket.Encode(&onionBlob)
				log.Printf("sphinxPacket.Encode(): %v", err)
				var h chainhash.Hash
				err = h.SetBytes(fundingTxID)
				if err != nil {
					log.Printf("h.SetBytes(%x) error: %v", fundingTxID, err)
					interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
						IncomingCircuitKey: request.IncomingCircuitKey,
						Action:             routerrpc.ResolveHoldForwardAction_FAIL,
					})
					continue
				}
				channelPoint := wire.NewOutPoint(&h, fundingTxOutnum).String()
				go resumeOrCancel(
					clientCtx, interceptorClient, request.IncomingCircuitKey, destination,
					channelPoint, uint64(amt), onionBlob.Bytes(),
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
	channelPoint string,
	outgoingAmountMsat uint64,
	onionBlob []byte,
) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		chanID := getChannel(ctx, client, destination, channelPoint)
		if chanID != 0 {
			interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
				IncomingCircuitKey:      incomingCircuitKey,
				Action:                  routerrpc.ResolveHoldForwardAction_RESUME,
				OutgoingAmountMsat:      outgoingAmountMsat,
				OutgoingRequestedChanId: chanID,
				OnionBlob:               onionBlob,
			})
			err := insertChannel(chanID, chanID, channelPoint, destination, time.Now())
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
	interceptorClient.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: incomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
}
