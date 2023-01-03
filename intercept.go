package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"golang.org/x/sync/singleflight"
)

type interceptAction int

const (
	INTERCEPT_RESUME              interceptAction = 0
	INTERCEPT_RESUME_WITH_ONION   interceptAction = 1
	INTERCEPT_FAIL_HTLC_WITH_CODE interceptAction = 2
)

type interceptFailureCode uint16

var (
	FAILURE_TEMPORARY_CHANNEL_FAILURE            interceptFailureCode = 0x1007
	FAILURE_TEMPORARY_NODE_FAILURE               interceptFailureCode = 0x2002
	FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS interceptFailureCode = 0x400F
)

var payHashGroup singleflight.Group

type interceptResult struct {
	action       interceptAction
	failureCode  interceptFailureCode
	destination  []byte
	amountMsat   uint64
	channelPoint *wire.OutPoint
	channelId    uint64
	onionBlob    []byte
}

func intercept(client LightningClient, config *NodeConfig, reqPaymentHash []byte, reqOutgoingAmountMsat uint64, reqOutgoingExpiry uint32) interceptResult {
	reqPaymentHashStr := hex.EncodeToString(reqPaymentHash)
	resp, _, _ := payHashGroup.Do(reqPaymentHashStr, func() (interface{}, error) {
		paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, channelPoint, err := paymentInfo(reqPaymentHash)
		if err != nil {
			log.Printf("paymentInfo(%x) error: %v", reqPaymentHash, err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_NODE_FAILURE,
			}, nil
		}
		log.Printf("paymentHash:%x\npaymentSecret:%x\ndestination:%x\nincomingAmountMsat:%v\noutgoingAmountMsat:%v",
			paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat)
		if paymentSecret == nil {
			return interceptResult{
				action: INTERCEPT_RESUME,
			}, nil
		}

		if channelPoint == nil {
			if bytes.Equal(paymentHash, reqPaymentHash) {
				channelPoint, err = openChannel(client, config, reqPaymentHash, destination, incomingAmountMsat)
				if err != nil {
					log.Printf("openChannel(%x, %v) err: %v", destination, incomingAmountMsat, err)
					return interceptResult{
						action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
						failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
					}, nil
				}
			} else { //probing
				return interceptResult{
					action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					failureCode: FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS,
				}, nil
			}
		}

		pubKey, err := btcec.ParsePubKey(destination)
		if err != nil {
			log.Printf("btcec.ParsePubKey(%x): %v", destination, err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		sessionKey, err := btcec.NewPrivateKey()
		if err != nil {
			log.Printf("btcec.NewPrivateKey(): %v", err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		var bigProd, bigAmt big.Int
		amt := (bigAmt.Div(bigProd.Mul(big.NewInt(outgoingAmountMsat), big.NewInt(int64(reqOutgoingAmountMsat))), big.NewInt(incomingAmountMsat))).Int64()

		var addr [32]byte
		copy(addr[:], paymentSecret)
		hop := route.Hop{
			AmtToForward:     lnwire.MilliSatoshi(amt),
			OutgoingTimeLock: reqOutgoingExpiry,
			MPP:              record.NewMPP(lnwire.MilliSatoshi(outgoingAmountMsat), addr),
			CustomRecords:    make(record.CustomSet),
		}

		var b bytes.Buffer
		err = hop.PackHopPayload(&b, uint64(0))
		if err != nil {
			log.Printf("hop.PackHopPayload(): %v", err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		payload, err := sphinx.NewHopPayload(nil, b.Bytes())
		if err != nil {
			log.Printf("sphinx.NewHopPayload(): %v", err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
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
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}
		var onionBlob bytes.Buffer
		err = sphinxPacket.Encode(&onionBlob)
		if err != nil {
			log.Printf("sphinxPacket.Encode(): %v", err)
			return interceptResult{
				action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		deadline := time.Now().Add(60 * time.Second)

		for {
			chanResult, _ := client.GetChannel(destination, *channelPoint)
			if chanResult != nil {
				log.Printf("channel opended successfully alias: %v, confirmed: %v", chanResult.InitialChannelID.ToString(), chanResult.ConfirmedChannelID.ToString())

				err := insertChannel(
					uint64(chanResult.InitialChannelID),
					uint64(chanResult.ConfirmedChannelID),
					channelPoint.String(),
					destination,
					time.Now(),
				)

				if err != nil {
					log.Printf("insertChannel error: %v", err)
					return interceptResult{
						action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
						failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
					}, nil
				}

				channelID := uint64(chanResult.ConfirmedChannelID)
				if channelID == 0 {
					channelID = uint64(chanResult.InitialChannelID)
				}

				return interceptResult{
					action:       INTERCEPT_RESUME_WITH_ONION,
					destination:  destination,
					channelPoint: channelPoint,
					channelId:    channelID,
					amountMsat:   uint64(amt),
					onionBlob:    onionBlob.Bytes(),
				}, nil
			}

			log.Printf("waiting for channel to get opened.... %v\n", destination)
			if time.Now().After(deadline) {
				log.Printf("Stop retrying getChannel(%v, %v)", destination, channelPoint.String())
				break
			}
			<-time.After(1 * time.Second)
		}

		log.Printf("Error: Channel failed to opened... timed out. ")
		return interceptResult{
			action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
			failureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
		}, nil
	})

	return resp.(interceptResult)
}

func checkPayment(config *NodeConfig, incomingAmountMsat, outgoingAmountMsat int64) error {
	fees := incomingAmountMsat * config.ChannelFeePermyriad / 10_000 / 1_000 * 1_000
	if fees < config.ChannelMinimumFeeMsat {
		fees = config.ChannelMinimumFeeMsat
	}
	if incomingAmountMsat-outgoingAmountMsat < fees {
		return fmt.Errorf("not enough fees")
	}
	return nil
}

func openChannel(client LightningClient, config *NodeConfig, paymentHash, destination []byte, incomingAmountMsat int64) (*wire.OutPoint, error) {
	capacity := incomingAmountMsat/1000 + config.AdditionalChannelCapacity
	if capacity == config.PublicChannelAmount {
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
