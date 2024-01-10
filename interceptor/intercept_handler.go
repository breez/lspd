package interceptor

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/history"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/notifications"
	"github.com/btcsuite/btcd/wire"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/singleflight"
)

type Interceptor struct {
	nodeid              []byte
	client              lightning.Client
	config              *config.NodeConfig
	store               InterceptStore
	historyStore        history.Store
	openingService      common.OpeningService
	feeEstimator        chain.FeeEstimator
	feeStrategy         chain.FeeStrategy
	payHashGroup        singleflight.Group
	notificationService *notifications.NotificationService
	enableSplicing      bool
}

func NewInterceptHandler(
	client lightning.Client,
	config *config.NodeConfig,
	store InterceptStore,
	historyStore history.Store,
	openingService common.OpeningService,
	feeEstimator chain.FeeEstimator,
	feeStrategy chain.FeeStrategy,
	notificationService *notifications.NotificationService,
	enableSplicing bool,
) *Interceptor {
	nodeid, _ := hex.DecodeString(config.NodePubkey)
	return &Interceptor{
		nodeid:              nodeid,
		client:              client,
		config:              config,
		store:               store,
		historyStore:        historyStore,
		openingService:      openingService,
		feeEstimator:        feeEstimator,
		feeStrategy:         feeStrategy,
		notificationService: notificationService,
		enableSplicing:      enableSplicing,
	}
}

func (i *Interceptor) Intercept(req common.InterceptRequest) common.InterceptResult {
	reqPaymentHashStr := hex.EncodeToString(req.PaymentHash)
	log.Printf("Intercept: scid: %s, paymentHash: %s, outgoingAmount: %v, outgoingExpiry: %v, incomingExpiry: %v", req.Scid.ToString(), reqPaymentHashStr, req.OutgoingAmountMsat, req.OutgoingExpiry, req.IncomingExpiry)
	resp, _, _ := i.payHashGroup.Do(reqPaymentHashStr, func() (interface{}, error) {
		token, params, paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, channelPoint, tag, err := i.store.PaymentInfo(req.PaymentHash)
		if err != nil {
			log.Printf("paymentInfo(%x) error: %v", req.PaymentHash, err)
			return common.InterceptResult{
				Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: common.FAILURE_TEMPORARY_NODE_FAILURE,
			}, nil
		}

		isRegistered := paymentSecret != nil
		// Sanity check. If the payment is registered, the destination is always set.
		if isRegistered && (destination == nil || len(destination) != 33) {
			log.Printf("ERROR: Payment was registered without destination. paymentHash: %s", reqPaymentHashStr)
		}

		isProbe := isRegistered && !bytes.Equal(paymentHash, req.PaymentHash)
		nextHop, _ := i.client.GetPeerId(&req.Scid)
		if err != nil {
			log.Printf("GetPeerId(%s) error: %v", req.Scid.ToString(), err)
			return common.InterceptResult{
				Action: common.INTERCEPT_RESUME,
			}, nil
		}

		// If the payment was registered, but the next hop is not the destination
		// that means we are not the last hop of the payment, so we'll just forward.
		if isRegistered && nextHop != nil && !bytes.Equal(nextHop, destination) {
			log.Printf("paymentHash: %s, nextHop (%s) != destination (%s)", reqPaymentHashStr, hex.EncodeToString(nextHop), hex.EncodeToString(destination))
			return common.InterceptResult{
				Action: common.INTERCEPT_RESUME,
			}, nil
		}

		// nextHop is set if the sender's scid corresponds to a known channel.
		// destination is set if the payment was registered for a channel open.
		// The 'actual' next hop will be either of those.
		if nextHop == nil {

			// If the next hop cannot be deduced from the scid, and the payment
			// is not registered, there's nothing left to be done. Just continue.
			if !isRegistered {
				log.Printf("paymentHash: %s, nextHop == nil and not registered", reqPaymentHashStr)
				return common.InterceptResult{
					Action: common.INTERCEPT_RESUME,
				}, nil
			}

			// The payment was registered, so the next hop is the registered
			// destination
			nextHop = destination
		}

		isConnected, err := i.client.IsConnected(nextHop)
		if err != nil {
			log.Printf("IsConnected(%x) error: %v", nextHop, err)
			return &common.InterceptResult{
				Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
				FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
			}, nil
		}

		if isProbe {
			// If this is a known probe, we'll quit early for non-connected clients.
			if !isConnected {
				log.Printf("paymentHash: %s, probe and not connected", reqPaymentHashStr)
				return common.InterceptResult{
					Action: common.INTERCEPT_RESUME,
				}, nil
			}

			// If it's a probe, they are connected, but need a channel, we'll return
			// INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS. This is an out-of-spec
			// error code, because it shouldn't be returned by an intermediate
			// node. But senders implementnig the probing-01: prefix should
			// know that the actual payment would probably succeed.
			if channelPoint == nil {
				log.Printf("paymentHash: %s, probe and channelPoint == nil", reqPaymentHashStr)
				return common.InterceptResult{
					Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: common.FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS,
				}, nil
			}
		}

		if isConnected {
			// If the peer is connected, notify anyway, but don't wait for the result.
			go i.notificationService.Notify(
				hex.EncodeToString(nextHop),
				reqPaymentHashStr,
			)
		} else {
			// Make sure the client is connected by potentially notifying them to come online.
			notifyResult := i.notifyAndWait(reqPaymentHashStr, nextHop, isRegistered)
			if notifyResult != nil {
				log.Printf("paymentHash: %s, !isConnected and notifyResult != nil", reqPaymentHashStr)
				// return *notifyResult, nil
			}
		}

		// The peer is online, we can resume the htlc if it's not a channel open.
		if !isRegistered {
			return common.InterceptResult{
				Action: common.INTERCEPT_RESUME,
			}, nil
		}

		// The first htlc of a MPP will open the channel.
		if channelPoint == nil {
			// TODO: When opening_fee_params is enforced, turn this check in a temporary channel failure.
			if params == nil {
				log.Printf("DEPRECATED: Intercepted htlc with deprecated fee mechanism. Using default fees. payment hash: %s", reqPaymentHashStr)
				params = &common.OpeningFeeParams{
					MinFeeMsat:           uint64(i.config.ChannelMinimumFeeMsat),
					Proportional:         uint32(i.config.ChannelFeePermyriad * 100),
					ValidUntil:           time.Now().UTC().Add(time.Duration(time.Hour * 24)).Format(lsps0.TIME_FORMAT),
					MinLifetime:          uint32(i.config.MaxInactiveDuration / 600),
					MaxClientToSelfDelay: uint32(10000),
				}
			}

			// Make sure the cltv delta is enough.
			if int64(req.IncomingExpiry)-int64(req.OutgoingExpiry) < int64(i.config.TimeLockDelta) {
				log.Printf("paymentHash: %s, outgoingExpiry: %v, incomingExpiry: %v, i.config.TimeLockDelta: %v", reqPaymentHashStr, req.OutgoingExpiry, req.IncomingExpiry, i.config.TimeLockDelta)
				return common.InterceptResult{
					Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}

			validUntil, err := time.Parse(lsps0.TIME_FORMAT, params.ValidUntil)
			if err != nil {
				log.Printf("paymentHash: %s, time.Parse(%s, %s) failed. Failing channel open: %v", reqPaymentHashStr, lsps0.TIME_FORMAT, params.ValidUntil, err)
				return common.InterceptResult{
					Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}

			// Make sure the opening_fee_params are not expired.
			// If they are expired, but the current chain fee is fine, open channel anyway.
			if time.Now().UTC().After(validUntil) {
				if !i.openingService.IsCurrentChainFeeCheaper(token, params) {
					log.Printf("Intercepted expired payment registration. Failing payment. payment hash: %s, valid until: %s", reqPaymentHashStr, params.ValidUntil)
					return common.InterceptResult{
						Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
						FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
					}, nil
				}

				log.Printf("Intercepted expired payment registration. Opening channel anyway, because it's cheaper at the current rate. paymenthash: %s, params: %+v", reqPaymentHashStr, params)
			}

			channelPoint, err = i.addLiquidity(req.PaymentHash, destination, incomingAmountMsat, tag)
			if err != nil {
				log.Printf("paymentHash: %s, addLiquidity(%x, %v) err: %v", reqPaymentHashStr, destination, incomingAmountMsat, err)
				return common.InterceptResult{
					Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
					FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
				}, nil
			}
		}

		var bigProd, bigAmt big.Int
		amt := (bigAmt.Div(bigProd.Mul(big.NewInt(outgoingAmountMsat), big.NewInt(int64(req.OutgoingAmountMsat))), big.NewInt(incomingAmountMsat))).Int64()

		deadline := time.Now().Add(60 * time.Second)

		for {
			chanResult, _ := i.client.GetChannel(destination, *channelPoint)
			if chanResult != nil {
				log.Printf("paymentHash: %s, channel opened successfully alias: '%v', confirmed: '%v'", reqPaymentHashStr, chanResult.AliasScid.ToString(), chanResult.ConfirmedScid.ToString())

				var scid *lightning.ShortChannelID
				if chanResult.ConfirmedScid == nil {
					if chanResult.AliasScid == nil {
						log.Printf("Error: GetChannel: Both confirmed scid and alias scid are nil: %+v", chanResult)
						<-time.After(1 * time.Second)
						continue
					}
					scid = chanResult.AliasScid
				} else {
					scid = chanResult.ConfirmedScid
				}

				err = i.historyStore.UpdateChannels(context.TODO(), []*history.ChannelUpdate{{
					NodeID:        i.nodeid,
					PeerId:        destination,
					AliasScid:     chanResult.AliasScid,
					ConfirmedScid: chanResult.ConfirmedScid,
					ChannelPoint:  channelPoint,
					LastUpdate:    time.Now(),
				}})
				if err != nil {
					// Don't break here, this is not critical.
					log.Printf("paymentHash: %s, failed to insert newly opened channel in history store. %v: %v", reqPaymentHashStr, channelPoint.String(), err)
				}

				err = i.historyStore.AddOpenChannelHtlc(context.TODO(), &history.OpenChannelHtlc{
					NodeId:             i.nodeid,
					PeerId:             destination,
					ChannelPoint:       channelPoint,
					OriginalAmountMsat: req.OutgoingAmountMsat,
					ForwardAmountMsat:  uint64(amt),
					IncomingAmountMsat: req.IncomingAmountMsat,
					ForwardTime:        time.Now(),
				})
				if err != nil {
					// Don't break here, this is not critical.
					log.Printf(
						"paymentHash: %s, failed to insert htlc used for channel open in history store: channel: %v,"+
							" original amount: %v, forward amount: %v, error: %v",
						reqPaymentHashStr,
						channelPoint.String(),
						req.OutgoingAmountMsat,
						amt,
						err,
					)
				}

				useLegacyOnionBlob := slices.Contains(i.config.LegacyOnionTokens, token)
				return common.InterceptResult{
					Action:             common.INTERCEPT_RESUME_WITH_ONION,
					Destination:        destination,
					ChannelPoint:       channelPoint,
					Scid:               *scid,
					PaymentSecret:      paymentSecret,
					AmountMsat:         uint64(amt),
					TotalAmountMsat:    uint64(outgoingAmountMsat),
					UseLegacyOnionBlob: useLegacyOnionBlob,
				}, nil
			}

			log.Printf("paymentHash: %s, waiting for channel to get opened... %x", reqPaymentHashStr, destination)
			if time.Now().After(deadline) {
				log.Printf("paymentHash: %s, Stop retrying getChannel(%v, %v)", reqPaymentHashStr, destination, channelPoint.String())
				break
			}
			<-time.After(1 * time.Second)
		}

		log.Printf("paymentHash: %s, Error: Channel failed to open... timed out. ", reqPaymentHashStr)
		return common.InterceptResult{
			Action:      common.INTERCEPT_FAIL_HTLC_WITH_CODE,
			FailureCode: common.FAILURE_TEMPORARY_CHANNEL_FAILURE,
		}, nil
	})

	return resp.(common.InterceptResult)
}

func (i *Interceptor) notifyAndWait(reqPaymentHashStr string, nextHop []byte, isRegistered bool) *common.InterceptResult {
	// If not connected, send a notification to the registered
	// notification service for this client if available.
	notified, err := i.notificationService.Notify(
		hex.EncodeToString(nextHop),
		reqPaymentHashStr,
	)

	// If this errors or the client is not notified, the client
	// is offline or unknown. We'll resume the HTLC (which will
	// result in UNKOWN_NEXT_PEER)
	if err != nil || !notified {
		return &common.InterceptResult{
			Action: common.INTERCEPT_RESUME,
		}
	}

	log.Printf("paymentHash %s, Notified %x of pending htlc", reqPaymentHashStr, nextHop)
	d, err := time.ParseDuration(i.config.NotificationTimeout)
	if err != nil {
		log.Printf("WARN: No NotificationTimeout set. Using default 1m")
		d = time.Minute
	}
	timeout := time.Now().Add(d)

	// Wait for a while to allow the client to come online.
	err = i.client.WaitOnline(nextHop, timeout)

	// If there's an error waiting, resume the htlc. It will
	// probably fail with UNKNOWN_NEXT_PEER.
	if err != nil {
		log.Printf(
			"paymentHash %s, waiting for peer %x to come online failed with %v",
			reqPaymentHashStr,
			nextHop,
			err,
		)
		return &common.InterceptResult{
			Action: common.INTERCEPT_RESUME,
		}
	}

	log.Printf("paymentHash %s, Peer %x is back online. Continue htlc.", reqPaymentHashStr, nextHop)
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
				"paymentHash %s, waiting for channel with %x to become active failed with %v",
				reqPaymentHashStr,
				nextHop,
				err,
			)
			return &common.InterceptResult{
				Action: common.INTERCEPT_RESUME,
			}
		}
	}

	return nil
}

func (i *Interceptor) addLiquidity(paymentHash, destination []byte, incomingAmountMsat int64, tag *string) (*wire.OutPoint, error) {
	capacity := incomingAmountMsat/1000 + i.config.AdditionalChannelCapacity
	if capacity == i.config.PublicChannelAmount {
		capacity++
	}

	fee, err := i.feeEstimator.EstimateFeeRate(
		context.Background(),
		i.feeStrategy,
	)
	if err != nil || fee == nil {
		log.Printf("Error estimating chain fee, fallback to target conf: %v", err)
		fee = &chain.FeeEstimation{
			TargetConf: &i.config.TargetConf,
		}
	}

	var channelPoint *wire.OutPoint
	splice, originalChannelPoint, err := i.shouldSplice(destination)
	if err != nil {
		log.Printf("paymenthash %x: Failed to determine whether to splice: %v", paymentHash, err)
	}

	if splice {
		channelPoint, err = i.splice(
			paymentHash,
			destination,
			originalChannelPoint,
			capacity,
			fee,
			incomingAmountMsat,
			tag,
		)
		if err != nil {
			return nil, fmt.Errorf("splice err: %w", err)
		}
	} else {
		channelPoint, err = i.openChannel(
			destination,
			paymentHash,
			capacity,
			fee,
			incomingAmountMsat,
			tag,
		)
		if err != nil {
			return nil, fmt.Errorf("openChannel err: %w", err)
		}
	}

	err = i.store.SetFundingTx(paymentHash, channelPoint)
	return channelPoint, err
}

func (i *Interceptor) shouldSplice(destination []byte) (bool, *wire.OutPoint, error) {
	if !i.enableSplicing {
		return false, nil, nil
	}

	peerInfo, err := i.client.GetPeerInfo(destination)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get peer info for %x: %w", destination, err)
	}

	if !peerInfo.SupportsSplicing {
		return false, nil, nil
	}

	if len(peerInfo.Channels) == 0 {
		return false, nil, nil
	}

	var bestChannel *lightning.PeerChannel
	for _, ch := range peerInfo.Channels {
		// only splice on anchor channels (since they are preferred over non-anchor channels)
		if !ch.IsZeroFeeHtlcTx {
			continue
		}

		if bestChannel == nil {
			bestChannel = ch
			continue
		}

		if ch.ConfirmedScid == nil {
			if bestChannel.ConfirmedScid == nil {
				// Both are equivalent. Pick the one with the 'lowest' txid.
				if bytes.Compare(
					ch.FundingOutpoint.Hash[:],
					bestChannel.FundingOutpoint.Hash[:],
				) < 0 {
					bestChannel = ch
				}
			}

			continue
		}

		if bestChannel.ConfirmedScid == nil {
			bestChannel = ch
			continue
		}

		// Pick the oldest channel
		if *ch.ConfirmedScid < *bestChannel.ConfirmedScid {
			bestChannel = ch
			continue
		}

		if *ch.ConfirmedScid > *bestChannel.ConfirmedScid {
			continue
		}
	}

	if bestChannel == nil {
		return false, nil, nil
	}

	return true, bestChannel.FundingOutpoint, nil
}

func (i *Interceptor) openChannel(
	destination []byte,
	paymentHash []byte,
	capacity int64,
	fee *chain.FeeEstimation,
	incomingAmountMsat int64,
	tag *string,
) (*wire.OutPoint, error) {

	log.Printf(
		"Opening zero conf channel. Paymenthash: %x, Destination: %x, capacity: %v, fee: %s",
		paymentHash,
		destination,
		capacity,
		fee.String(),
	)
	channelPoint, err := i.client.OpenChannel(&lightning.OpenChannelRequest{
		Destination:    destination,
		CapacitySat:    uint64(capacity),
		MinConfs:       i.config.MinConfs,
		FeeSatPerVByte: fee.SatPerVByte,
		TargetConf:     fee.TargetConf,
	})
	if err != nil {
		log.Printf("paymenthash %x, client.OpenChannel(%x, %v) error: %v", paymentHash, destination, capacity, err)
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

	return channelPoint, nil
}

func (i *Interceptor) splice(
	paymentHash []byte,
	destination []byte,
	originalChannelPoint *wire.OutPoint,
	capacity int64,
	fee *chain.FeeEstimation,
	incomingAmountMsat int64,
	tag *string,
) (*wire.OutPoint, error) {
	log.Printf(
		"Splicing in additional liquidity. Paymenthash: %x, Destination: %x, capacity: %v, fee: %s",
		paymentHash,
		destination,
		capacity,
		fee.String(),
	)
	channelPoint, err := i.client.SpliceIn(&lightning.SpliceInRequest{
		PeerId:                destination,
		ChannelOutpoint:       originalChannelPoint,
		AdditionalCapacitySat: uint64(capacity),
		FeeSatPerVByte:        fee.SatPerVByte,
		TargetConf:            fee.TargetConf,
		MinConfs:              i.config.MinConfs,
	})
	if err != nil {
		log.Printf("paymenthash %x, client.SpliceIn(%x, %v) error: %v", paymentHash, destination, capacity, err)
		return nil, err
	}

	sendSpliceEmailNotification(
		paymentHash,
		incomingAmountMsat,
		destination,
		capacity,
		channelPoint.String(),
		tag,
	)

	return channelPoint, nil
}
