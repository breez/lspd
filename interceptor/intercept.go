package interceptor

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/notifications"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
	"golang.org/x/sync/singleflight"
)

type InterceptAction int

const (
	INTERCEPT_RESUME              InterceptAction = 0
	INTERCEPT_RESUME_WITH_ONION   InterceptAction = 1
	INTERCEPT_FAIL_HTLC_WITH_CODE InterceptAction = 2
)

type InterceptFailureCode uint16

var (
	FAILURE_TEMPORARY_CHANNEL_FAILURE            InterceptFailureCode = 0x1007
	FAILURE_TEMPORARY_NODE_FAILURE               InterceptFailureCode = 0x2002
	FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS InterceptFailureCode = 0x400F
)

type InterceptResult struct {
	Action       InterceptAction
	FailureCode  InterceptFailureCode
	Destination  []byte
	AmountMsat   uint64
	ChannelPoint *wire.OutPoint
	ChannelId    uint64
	OnionBlob    []byte
}

type Interceptor struct {
	client              lightning.Client
	config              *config.NodeConfig
	store               InterceptStore
	feeEstimator        chain.FeeEstimator
	feeStrategy         chain.FeeStrategy
	payHashGroup        singleflight.Group
	notificationService *notifications.NotificationService
}

func NewInterceptor(
	client lightning.Client,
	config *config.NodeConfig,
	store InterceptStore,
	feeEstimator chain.FeeEstimator,
	feeStrategy chain.FeeStrategy,
	notificationService *notifications.NotificationService,
) *Interceptor {
	return &Interceptor{
		client:              client,
		config:              config,
		store:               store,
		feeEstimator:        feeEstimator,
		feeStrategy:         feeStrategy,
		notificationService: notificationService,
	}
}

func (i *Interceptor) Intercept(scid *basetypes.ShortChannelID, reqPaymentHash []byte, reqOutgoingAmountMsat uint64, reqOutgoingExpiry uint32, reqIncomingExpiry uint32) InterceptResult {
	reqPaymentHashStr := hex.EncodeToString(reqPaymentHash)
	resp, _, _ := i.payHashGroup.Do(reqPaymentHashStr, func() (interface{}, error) {
		token, params, paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, channelPoint, tag, err := i.store.PaymentInfo(reqPaymentHash)
		if err != nil {
			log.Printf("paymentInfo(%x) error: %v", reqPaymentHash, err)
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_NODE_FAILURE,
			}, nil
		}

		isRegistered := paymentSecret != nil
		isProbe := isRegistered && !bytes.Equal(paymentHash, reqPaymentHash)
		nextHop, _ := i.client.GetPeerId(scid)
		if err != nil {
			log.Printf("GetPeerId(%s) error: %v", scid.ToString(), err)
			return InterceptResult{
				Action: INTERCEPT_RESUME,
			}, nil
		}

		// If the payment was registered, but the next hop is not the destination
		// that means we are not the last hop of the payment, so we'll just forward.
		if destination != nil && nextHop != nil && !bytes.Equal(nextHop, destination) {
			return InterceptResult{
				Action: INTERCEPT_RESUME,
			}, nil
		}

		// nextHop is set if the sender's scid corresponds to a known channel
		// destination is set if the payment was registered for a channel open.
		// The 'actual' next hop will be either of those. Or nil if the next hop
		// is unknown.
		if nextHop == nil {
			nextHop = destination
		}

		if nextHop != nil {
			isConnected, err := i.client.IsConnected(nextHop)
			if err != nil {
				log.Printf("IsConnected(%x) error: %v", nextHop, err)
				return InterceptResult{
					Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}

			if !isConnected {
				// If not connected, send a notification to the registered
				// notification service for this client if available.
				notified, err := i.notificationService.Notify(
					hex.EncodeToString(nextHop),
					reqPaymentHashStr,
				)

				// If this errors or the client is not notified, the client
				// is offline or unknown. We'll resume the HTLC (which will
				// result in UNKOWN_NEXT_PEER)
				if err != nil {
					return InterceptResult{
						Action: INTERCEPT_RESUME,
					}, nil
				}

				if notified {
					log.Printf("Notified %x of pending htlc", nextHop)
					d, err := time.ParseDuration(i.config.NotificationTimeout)
					if err != nil {
						log.Printf("WARN: No NotificationTimeout set. Using default 1m")
						d = time.Minute
					}
					timeout := time.Now().Add(d)
					err = i.client.WaitOnline(nextHop, timeout)

					// If there's an error waiting, resume the htlc. It will
					// probably fail with UNKNOWN_NEXT_PEER.
					if err != nil {
						log.Printf(
							"waiting for peer %x to come online failed with %v",
							nextHop,
							err,
						)
						return InterceptResult{
							Action: INTERCEPT_RESUME,
						}, nil
					}

					log.Printf("Peer %x is back online. Continue htlc.", nextHop)
					// At this point we know a few things.
					// - This is either a channel partner or a registered payment
					// - they were offline
					// - They got notified about the htlc
					// - They came back online
					// So if this payment was not registered, this is a channel
					// partner and we have to wait for the channel to become active
					// before we can forward.
					if !isRegistered {
						err = i.client.WaitChannelActive(nextHop, timeout)
						if err != nil {
							log.Printf(
								"waiting for channnel with %x to become active failed with %v",
								nextHop,
								err,
							)
							return InterceptResult{
								Action: INTERCEPT_RESUME,
							}, nil
						}
					}
				} else if isProbe {
					return InterceptResult{
						Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
						FailureCode: FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS,
					}, nil
				} else {
					// If we haven't notified, resume the htlc. It will probably
					// fail with UNKNOWN_NEXT_PEER.
					return InterceptResult{
						Action: INTERCEPT_RESUME,
					}, nil
				}
			}
		}

		if !isRegistered {
			return InterceptResult{
				Action: INTERCEPT_RESUME,
			}, nil
		}

		if channelPoint == nil {
			if isProbe {
				return InterceptResult{
					Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS,
				}, nil
			}

			// TODO: When opening_fee_params is enforced, turn this check in a temporary channel failure.
			if params == nil {
				log.Printf("DEPRECATED: Intercepted htlc with deprecated fee mechanism. Using default fees. payment hash: %s", reqPaymentHashStr)
				params = &OpeningFeeParams{
					MinMsat:              uint64(i.config.ChannelMinimumFeeMsat),
					Proportional:         uint32(i.config.ChannelFeePermyriad * 100),
					ValidUntil:           time.Now().UTC().Add(time.Duration(time.Hour * 24)).Format(basetypes.TIME_FORMAT),
					MaxIdleTime:          uint32(i.config.MaxInactiveDuration / 600),
					MaxClientToSelfDelay: uint32(10000),
				}
			}

			if int64(reqIncomingExpiry)-int64(reqOutgoingExpiry) < int64(i.config.TimeLockDelta) {
				return InterceptResult{
					Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}

			validUntil, err := time.Parse(basetypes.TIME_FORMAT, params.ValidUntil)
			if err != nil {
				log.Printf("time.Parse(%s, %s) failed. Failing channel open: %v", basetypes.TIME_FORMAT, params.ValidUntil, err)
				return InterceptResult{
					Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}

			if time.Now().UTC().After(validUntil) {
				if !i.isCurrentChainFeeCheaper(token, params) {
					log.Printf("Intercepted expired payment registration. Failing payment. payment hash: %x, valid until: %s", paymentHash, params.ValidUntil)
					return InterceptResult{
						Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
						FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
					}, nil
				}

				log.Printf("Intercepted expired payment registration. Opening channel anyway, because it's cheaper at the current rate. paymenthash: %s, params: %+v", reqPaymentHashStr, params)
			}

			channelPoint, err = i.openChannel(reqPaymentHash, destination, incomingAmountMsat, tag)
			if err != nil {
				log.Printf("openChannel(%x, %v) err: %v", destination, incomingAmountMsat, err)
				return InterceptResult{
					Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}
		}

		pubKey, err := btcec.ParsePubKey(destination)
		if err != nil {
			log.Printf("btcec.ParsePubKey(%x): %v", destination, err)
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		sessionKey, err := btcec.NewPrivateKey()
		if err != nil {
			log.Printf("btcec.NewPrivateKey(): %v", err)
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
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
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		payload, err := sphinx.NewHopPayload(nil, b.Bytes())
		if err != nil {
			log.Printf("sphinx.NewHopPayload(): %v", err)
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
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
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}
		var onionBlob bytes.Buffer
		err = sphinxPacket.Encode(&onionBlob)
		if err != nil {
			log.Printf("sphinxPacket.Encode(): %v", err)
			return InterceptResult{
				Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		deadline := time.Now().Add(60 * time.Second)

		for {
			chanResult, _ := i.client.GetChannel(destination, *channelPoint)
			if chanResult != nil {
				log.Printf("channel opened successfully alias: %v, confirmed: %v", chanResult.InitialChannelID.ToString(), chanResult.ConfirmedChannelID.ToString())

				err := i.store.InsertChannel(
					uint64(chanResult.InitialChannelID),
					uint64(chanResult.ConfirmedChannelID),
					channelPoint.String(),
					destination,
					time.Now(),
				)

				if err != nil {
					log.Printf("insertChannel error: %v", err)
					return InterceptResult{
						Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
						FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
					}, nil
				}

				channelID := uint64(chanResult.ConfirmedChannelID)
				if channelID == 0 {
					channelID = uint64(chanResult.InitialChannelID)
				}

				return InterceptResult{
					Action:       INTERCEPT_RESUME_WITH_ONION,
					Destination:  destination,
					ChannelPoint: channelPoint,
					ChannelId:    channelID,
					AmountMsat:   uint64(amt),
					OnionBlob:    onionBlob.Bytes(),
				}, nil
			}

			log.Printf("waiting for channel to get opened.... %v\n", destination)
			if time.Now().After(deadline) {
				log.Printf("Stop retrying getChannel(%v, %v)", destination, channelPoint.String())
				break
			}
			<-time.After(1 * time.Second)
		}

		log.Printf("Error: Channel failed to open... timed out. ")
		return InterceptResult{
			Action:      INTERCEPT_FAIL_HTLC_WITH_CODE,
			FailureCode: FAILURE_TEMPORARY_CHANNEL_FAILURE,
		}, nil
	})

	return resp.(InterceptResult)
}

func (i *Interceptor) isCurrentChainFeeCheaper(token string, params *OpeningFeeParams) bool {
	settings, err := i.store.GetFeeParamsSettings(token)
	if err != nil {
		log.Printf("Failed to get fee params settings: %v", err)
		return false
	}

	for _, setting := range settings {
		if setting.Params.MinMsat <= params.MinMsat {
			return true
		}
	}

	return false
}

func (i *Interceptor) openChannel(paymentHash, destination []byte, incomingAmountMsat int64, tag *string) (*wire.OutPoint, error) {
	capacity := incomingAmountMsat/1000 + i.config.AdditionalChannelCapacity
	if capacity == i.config.PublicChannelAmount {
		capacity++
	}

	var targetConf *uint32
	confStr := "<nil>"
	var feeEstimation *float64
	feeStr := "<nil>"
	if i.feeEstimator != nil {
		fee, err := i.feeEstimator.EstimateFeeRate(
			context.Background(),
			i.feeStrategy,
		)
		if err == nil {
			feeEstimation = &fee.SatPerVByte
			feeStr = fmt.Sprintf("%.5f", *feeEstimation)
		} else {
			log.Printf("Error estimating chain fee, fallback to target conf: %v", err)
			targetConf = &i.config.TargetConf
			confStr = fmt.Sprintf("%v", *targetConf)
		}
	}

	log.Printf(
		"Opening zero conf channel. Destination: %x, capacity: %v, fee: %s, targetConf: %s",
		destination,
		capacity,
		feeStr,
		confStr,
	)
	channelPoint, err := i.client.OpenChannel(&lightning.OpenChannelRequest{
		Destination:    destination,
		CapacitySat:    uint64(capacity),
		MinConfs:       i.config.MinConfs,
		IsPrivate:      true,
		IsZeroConf:     true,
		FeeSatPerVByte: feeEstimation,
		TargetConf:     targetConf,
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
		tag,
	)
	err = i.store.SetFundingTx(paymentHash, channelPoint)
	return channelPoint, err
}
