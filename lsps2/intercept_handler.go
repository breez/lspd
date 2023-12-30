package lsps2

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

type InterceptorConfig struct {
	NodeId                       []byte
	ChainHash                    chainhash.Hash
	AdditionalChannelCapacitySat uint64
	MinConfs                     *uint32
	TargetConf                   uint32
	FeeStrategy                  chain.FeeStrategy
	MinPaymentSizeMsat           uint64
	MaxPaymentSizeMsat           uint64
	TimeLockDelta                uint32
	HtlcMinimumMsat              uint64
	MppTimeout                   time.Duration
}

type Interceptor struct {
	store               Lsps2Store
	openingService      common.OpeningService
	client              lightning.Client
	feeEstimator        chain.FeeEstimator
	config              *InterceptorConfig
	newPart             chan *partState
	registrationFetched chan *registrationFetchedEvent
	paymentReady        chan string
	paymentFailure      chan *paymentFailureEvent
	paymentChanOpened   chan *paymentChanOpenedEvent
	inflightPayments    map[string]*paymentState
}

func NewInterceptHandler(
	store Lsps2Store,
	openingService common.OpeningService,
	client lightning.Client,
	feeEstimator chain.FeeEstimator,
	config *InterceptorConfig,
) *Interceptor {
	if config.MppTimeout.Nanoseconds() == 0 {
		config.MppTimeout = time.Duration(90 * time.Second)
	}

	return &Interceptor{
		store:          store,
		openingService: openingService,
		client:         client,
		feeEstimator:   feeEstimator,
		config:         config,
		// TODO: make sure the chan sizes do not lead to deadlocks.
		newPart:             make(chan *partState, 1000),
		registrationFetched: make(chan *registrationFetchedEvent, 1000),
		paymentReady:        make(chan string, 1000),
		paymentFailure:      make(chan *paymentFailureEvent, 1000),
		paymentChanOpened:   make(chan *paymentChanOpenedEvent, 1000),
		inflightPayments:    make(map[string]*paymentState),
	}
}

type paymentState struct {
	id               string
	fakeScid         lightning.ShortChannelID
	outgoingSumMsat  uint64
	paymentSizeMsat  uint64
	feeMsat          uint64
	registration     *BuyRegistration
	parts            map[string]*partState
	isFinal          bool
	timoutChanClosed bool
	timeoutChan      chan struct{}
}

func (p *paymentState) closeTimeoutChan() {
	if p.timoutChanClosed {
		return
	}

	close(p.timeoutChan)
	p.timoutChanClosed = true
}

type partState struct {
	req        *common.InterceptRequest
	resolution chan *common.InterceptResult
}

type registrationFetchedEvent struct {
	paymentId    string
	isRegistered bool
	registration *BuyRegistration
}

type paymentChanOpenedEvent struct {
	paymentId       string
	scid            lightning.ShortChannelID
	channelPoint    *wire.OutPoint
	htlcMinimumMsat uint64
}

type paymentFailureEvent struct {
	paymentId string
	message   common.InterceptFailureCode
}

func (i *Interceptor) Start(ctx context.Context) {
	// Main event loop for stages of htlcs to be handled. Note that the event
	// loop has to execute quickly, so any code running in the 'handle' methods
	// must execute quickly. If there is i/o involved, or any waiting, run that
	// code in a goroutine, and place an event onto the event loop to continue
	// processing after the slow operation is done.
	// The nice thing about an event loop is that it runs on a single thread.
	// So there's no locking needed, as long as everything that needs
	// synchronization goes through the event loop.
	for {
		select {
		case part := <-i.newPart:
			i.handleNewPart(part)
		case ev := <-i.registrationFetched:
			i.handleRegistrationFetched(ev)
		case paymentId := <-i.paymentReady:
			i.handlePaymentReady(paymentId)
		case ev := <-i.paymentFailure:
			i.handlePaymentFailure(ev.paymentId, ev.message)
		case ev := <-i.paymentChanOpened:
			i.handlePaymentChanOpened(ev)
		}
	}
}

func (i *Interceptor) handleNewPart(part *partState) {
	// Get the associated payment for this part, or create a new payment if it
	// doesn't exist for this part yet.
	paymentId := part.req.PaymentId()
	payment, paymentExisted := i.inflightPayments[paymentId]
	if !paymentExisted {
		payment = &paymentState{
			id:          paymentId,
			fakeScid:    part.req.Scid,
			parts:       make(map[string]*partState),
			timeoutChan: make(chan struct{}),
		}
		i.inflightPayments[paymentId] = payment
	}

	// Check whether we already have this part, because it may have been
	// replayed.
	existingPart, partExisted := payment.parts[part.req.HtlcId()]
	// Adds the part to the in-progress parts. Or replaces it, if it already
	// exists, to make sure we always reply to the correct identifier. If a htlc
	// was replayed, assume the latest event is the truth to respond to.
	payment.parts[part.req.HtlcId()] = part

	if partExisted {
		// If the part already existed, that means it has been replayed. In this
		// case the first occurence can be safely ignored, because we won't be
		// able to reply to that htlc anyway. Keep the last replayed version for
		// further processing. This result below tells the caller to ignore the
		// htlc.
		existingPart.resolution <- &common.InterceptResult{
			Action: common.INTERCEPT_IGNORE,
		}

		return
	}

	// If this is the first part for this payment, setup the timeout, and fetch
	// the registration.
	if !paymentExisted {
		go func() {
			select {
			case <-time.After(i.config.MppTimeout):
				// Handle timeout inside the event loop, to make sure there are
				// no race conditions, since this timeout watcher is running in
				// a goroutine.
				i.paymentFailure <- &paymentFailureEvent{
					paymentId: paymentId,
					message:   common.FailureTemporaryChannelFailure(nil),
				}
			case <-payment.timeoutChan:
				// Stop listening for timeouts when the payment is ready.
			}
		}()

		// Fetch the buy registration in a goroutine, to avoid blocking the
		// event loop.
		go i.fetchRegistration(part.req.PaymentId(), part.req.Scid)
	}

	// If the registration was already fetched, this part might complete the
	// payment. Process the part. Otherwise, the part will be processed after
	// the registration was fetched, as a result of 'registrationFetched'.
	if payment.registration != nil {
		i.processPart(payment, part)
	}
}

func (i *Interceptor) processPart(payment *paymentState, part *partState) {
	if payment.registration.IsComplete {
		i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
		return
	}

	// Fail parts that come in after the payment is already final. To avoid
	// inconsistencies in the payment state.
	if payment.isFinal {
		i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
		return
	}

	var err error
	if payment.registration.Mode == OpeningMode_NoMppVarInvoice {
		// Mode == no-MPP+var-invoice
		if payment.paymentSizeMsat != 0 {
			// Another part is already processed for this payment, and with
			// no-MPP+var-invoice there can be only a single part, so this
			// part will be failed back.
			i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
			return
		}

		// If the mode is no-MPP+var-invoice, the payment size comes from
		// the actual forwarded amount.
		payment.paymentSizeMsat = part.req.OutgoingAmountMsat

		// Make sure the minimum and maximum are not exceeded.
		if payment.paymentSizeMsat > i.config.MaxPaymentSizeMsat ||
			payment.paymentSizeMsat < i.config.MinPaymentSizeMsat {
			i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
			return
		}

		// Make sure there is enough fee to deduct.
		payment.feeMsat, err = computeOpeningFee(
			payment.paymentSizeMsat,
			payment.registration.OpeningFeeParams.Proportional,
			payment.registration.OpeningFeeParams.MinFeeMsat,
		)
		if err != nil {
			i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
			return
		}

		// Make sure the part fits the htlc and fee constraints.
		if payment.feeMsat+i.config.HtlcMinimumMsat >
			payment.paymentSizeMsat {
			i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
			return
		}
	} else {
		// Mode == MPP+fixed-invoice
		payment.paymentSizeMsat = *payment.registration.PaymentSizeMsat
		payment.feeMsat, err = computeOpeningFee(
			payment.paymentSizeMsat,
			payment.registration.OpeningFeeParams.Proportional,
			payment.registration.OpeningFeeParams.MinFeeMsat,
		)
		if err != nil {
			log.Printf(
				"Opening fee calculation error while trying to open channel "+
					"for scid %s: %v",
				payment.registration.Scid.ToString(),
				err,
			)
			i.failPart(payment, part, common.FAILURE_UNKNOWN_NEXT_PEER)
			return
		}
	}

	// Make sure the cltv delta is enough (actual cltv delta + 2).
	if int64(part.req.IncomingExpiry)-int64(part.req.OutgoingExpiry) <
		int64(i.config.TimeLockDelta)+2 {
		peerid, _ := hex.DecodeString(payment.registration.PeerId)
		chanUpdate := common.ConstructChanUpdate(
			i.config.ChainHash,
			i.config.NodeId,
			peerid,
			payment.fakeScid,
			uint16(i.config.TimeLockDelta),
			i.config.HtlcMinimumMsat,
			payment.paymentSizeMsat,
		)
		i.failPart(payment, part, common.FailureIncorrectCltvExpiry(part.req.IncomingExpiry, chanUpdate))
		return
	}

	// Make sure htlc minimum is enough
	if part.req.OutgoingAmountMsat < i.config.HtlcMinimumMsat {
		i.failPart(payment, part, common.FAILURE_AMOUNT_BELOW_MINIMUM)
		return
	}

	// Make sure we're not getting tricked
	if part.req.IncomingAmountMsat < part.req.OutgoingAmountMsat {
		i.failPart(payment, part, common.FAILURE_AMOUNT_BELOW_MINIMUM)
		return
	}

	// Update the sum of htlcs currently in-flight.
	payment.outgoingSumMsat += part.req.OutgoingAmountMsat

	// If payment_size_msat is reached, the payment is ready to forward. (this
	// is always true in no-MPP+var-invoice mode)
	if payment.outgoingSumMsat >= payment.paymentSizeMsat {
		payment.isFinal = true
		i.paymentReady <- part.req.PaymentId()
	}
}

// Fetches the registration from the store. If the registration exists, posts
// a registrationReady event. If it doesn't, posts a 'notRegistered' event.
func (i *Interceptor) fetchRegistration(
	paymentId string,
	scid lightning.ShortChannelID,
) {
	registration, err := i.store.GetBuyRegistration(
		context.TODO(),
		scid,
	)

	if err != nil && err != ErrNotFound {
		log.Printf(
			"Failed to get buy registration for %v: %v",
			uint64(scid),
			err,
		)
	}

	i.registrationFetched <- &registrationFetchedEvent{
		paymentId:    paymentId,
		isRegistered: err == nil,
		registration: registration,
	}
}

func (i *Interceptor) handleRegistrationFetched(ev *registrationFetchedEvent) {
	if !ev.isRegistered {
		i.finalizeAllParts(ev.paymentId, &common.InterceptResult{
			Action: common.INTERCEPT_RESUME,
		})
		return
	}

	payment, ok := i.inflightPayments[ev.paymentId]
	if !ok {
		// Apparently the payment is already finished.
		return
	}

	payment.registration = ev.registration
	for _, part := range payment.parts {
		i.processPart(payment, part)
	}
}

func (i *Interceptor) handlePaymentReady(paymentId string) {
	payment, ok := i.inflightPayments[paymentId]
	if !ok {
		// Apparently this payment is already finalized.
		return
	}

	// TODO: Handle notifications.
	// Stops the timeout listeners
	payment.closeTimeoutChan()

	go i.ensureChannelOpen(payment)
}

// Opens a channel to the destination and waits for the channel to become
// active. When the channel is active, sends an openChanEvent. Should be run in
// a goroutine.
func (i *Interceptor) ensureChannelOpen(payment *paymentState) {
	destination, _ := hex.DecodeString(payment.registration.PeerId)
	peerid, _ := hex.DecodeString(payment.registration.PeerId)
	chanUpdate := common.ConstructChanUpdate(
		i.config.ChainHash,
		i.config.NodeId,
		peerid,
		payment.fakeScid,
		uint16(i.config.TimeLockDelta),
		i.config.HtlcMinimumMsat,
		payment.paymentSizeMsat,
	)

	if payment.registration.ChannelPoint == nil {

		validUntil, err := time.Parse(
			lsps0.TIME_FORMAT,
			payment.registration.OpeningFeeParams.ValidUntil,
		)
		if err != nil {
			log.Printf(
				"Failed parse validUntil '%s' for paymentId %s: %v",
				payment.registration.OpeningFeeParams.ValidUntil,
				payment.id,
				err,
			)
			i.paymentFailure <- &paymentFailureEvent{
				paymentId: payment.id,
				message:   common.FAILURE_UNKNOWN_NEXT_PEER,
			}
			return
		}

		// With expired fee params, the current chainfees are checked. If
		// they're not cheaper now, fail the payment.
		if time.Now().After(validUntil) &&
			!i.openingService.IsCurrentChainFeeCheaper(
				payment.registration.Token,
				&payment.registration.OpeningFeeParams,
			) {
			log.Printf("LSPS2: Intercepted expired payment registration. "+
				"Failing payment. scid: %s, valid until: %s, destination: %s",
				payment.fakeScid.ToString(),
				payment.registration.OpeningFeeParams.ValidUntil,
				payment.registration.PeerId,
			)
			i.paymentFailure <- &paymentFailureEvent{
				paymentId: payment.id,
				message:   common.FAILURE_UNKNOWN_NEXT_PEER,
			}
			return
		}

		var targetConf *uint32
		confStr := "<nil>"
		var feeEstimation *float64
		feeStr := "<nil>"
		if i.feeEstimator != nil {
			fee, err := i.feeEstimator.EstimateFeeRate(
				context.Background(),
				i.config.FeeStrategy,
			)
			if err == nil {
				feeEstimation = &fee.SatPerVByte
				feeStr = fmt.Sprintf("%.5f", *feeEstimation)
			} else {
				log.Printf("Error estimating chain fee, fallback to target "+
					"conf: %v", err)
				targetConf = &i.config.TargetConf
				confStr = fmt.Sprintf("%v", *targetConf)
			}
		}

		capacity := ((payment.paymentSizeMsat - payment.feeMsat +
			999) / 1000) + i.config.AdditionalChannelCapacitySat

		log.Printf(
			"LSPS2: Opening zero conf channel. Destination: %x, capacity: %v, "+
				"fee: %s, targetConf: %s",
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
			log.Printf(
				"LSPS2 openChannel: client.OpenChannel(%x, %v) error: %v",
				destination,
				capacity,
				err,
			)

			code := common.FAILURE_UNKNOWN_NEXT_PEER
			if strings.Contains(err.Error(), "not enough") {
				code = common.FailureTemporaryChannelFailure(&chanUpdate)
			}

			// TODO: Verify that a client disconnect before receiving
			// funding_signed doesn't cause the OpenChannel call to error.
			// unknown_next_peer should only be returned if the client rejects
			// the channel, or the channel cannot be opened at all. If the
			// client disconnects before receiving funding_signed,
			// temporary_channel_failure should be returned.
			i.paymentFailure <- &paymentFailureEvent{
				paymentId: payment.id,
				message:   code,
			}
			return
		}

		err = i.store.SetChannelOpened(
			context.TODO(),
			&ChannelOpened{
				RegistrationId:  payment.registration.Id,
				Outpoint:        channelPoint,
				FeeMsat:         payment.feeMsat,
				PaymentSizeMsat: payment.paymentSizeMsat,
			},
		)
		if err != nil {
			log.Printf(
				"LSPS2 openChannel: store.SetOpenedChannel(%d, %s) error: %v",
				payment.registration.Id,
				channelPoint.String(),
				err,
			)
			i.paymentFailure <- &paymentFailureEvent{
				paymentId: payment.id,
				message:   common.FailureTemporaryChannelFailure(&chanUpdate),
			}
			return
		}

		payment.registration.ChannelPoint = channelPoint
		// TODO: Send open channel email notification.
	}
	deadline := time.Now().Add(time.Minute)
	// Wait for the channel to open.
	for {
		chanResult, _ := i.client.GetChannel(
			destination,
			*payment.registration.ChannelPoint,
		)
		if chanResult == nil {
			select {
			case <-time.After(time.Second):
				continue
			case <-time.After(time.Until(deadline)):
				i.paymentFailure <- &paymentFailureEvent{
					paymentId: payment.id,
					message:   common.FailureTemporaryChannelFailure(&chanUpdate),
				}
				return
			}
		}
		log.Printf(
			"Got new channel for forward successfully. scid alias: %v, "+
				"confirmed scid: %v",
			chanResult.InitialChannelID.ToString(),
			chanResult.ConfirmedChannelID.ToString(),
		)

		scid := chanResult.ConfirmedChannelID
		if uint64(scid) == 0 {
			scid = chanResult.InitialChannelID
		}

		i.paymentChanOpened <- &paymentChanOpenedEvent{
			paymentId:       payment.id,
			scid:            scid,
			channelPoint:    payment.registration.ChannelPoint,
			htlcMinimumMsat: chanResult.HtlcMinimumMsat,
		}
		break
	}
}

func (i *Interceptor) handlePaymentChanOpened(event *paymentChanOpenedEvent) {
	payment, ok := i.inflightPayments[event.paymentId]
	if !ok {
		// Apparently this payment is already finalized.
		return
	}
	feeRemainingMsat := payment.feeMsat

	destination, _ := hex.DecodeString(payment.registration.PeerId)
	// Deduct the lsp fee from the parts to forward.
	resolutions := []*struct {
		part       *partState
		resolution *common.InterceptResult
	}{}
	for _, part := range payment.parts {
		deductMsat := uint64(math.Min(
			float64(feeRemainingMsat),
			float64(part.req.OutgoingAmountMsat-event.htlcMinimumMsat),
		))
		feeRemainingMsat -= deductMsat
		amountMsat := part.req.OutgoingAmountMsat - deductMsat
		var feeMsat *uint64
		if deductMsat > 0 {
			feeMsat = &deductMsat
		}
		resolutions = append(resolutions, &struct {
			part       *partState
			resolution *common.InterceptResult
		}{
			part: part,
			resolution: &common.InterceptResult{
				Action:       common.INTERCEPT_RESUME_WITH_ONION,
				Destination:  destination,
				ChannelPoint: event.channelPoint,
				AmountMsat:   amountMsat,
				FeeMsat:      feeMsat,
				Scid:         event.scid,
			},
		})
	}

	if feeRemainingMsat > 0 {
		// It is possible this case happens if the htlc_minimum_msat is larger
		// than 1. We might not be able to deduct the opening fees from the
		// payment entirely. This is an edge case, and we'll fail the payment.
		log.Printf(
			"After deducting fees from payment parts, there was still fee "+
				"remaining. payment id: %s, fee remaining msat: %d. Failing "+
				"payment.",
			event.paymentId,
			feeRemainingMsat,
		)

		peerid, _ := hex.DecodeString(payment.registration.PeerId)
		chanUpdate := common.ConstructChanUpdate(
			i.config.ChainHash,
			i.config.NodeId,
			peerid,
			payment.fakeScid,
			uint16(i.config.TimeLockDelta),
			i.config.HtlcMinimumMsat,
			payment.paymentSizeMsat,
		)
		// TODO: Verify temporary_channel_failure is the way to go here, maybe
		// unknown_next_peer is more appropriate.
		i.paymentFailure <- &paymentFailureEvent{
			paymentId: event.paymentId,
			message:   common.FailureTemporaryChannelFailure(&chanUpdate),
		}
		return
	}

	for _, resolution := range resolutions {
		resolution.part.resolution <- resolution.resolution
	}

	payment.registration.IsComplete = true
	go i.store.SetCompleted(context.TODO(), payment.registration.Id)
	delete(i.inflightPayments, event.paymentId)
}

func (i *Interceptor) handlePaymentFailure(
	paymentId string,
	message []byte,
) {
	i.finalizeAllParts(paymentId, &common.InterceptResult{
		Action:         common.INTERCEPT_FAIL_HTLC_WITH_CODE,
		FailureMessage: message,
	})
}

func (i *Interceptor) finalizeAllParts(
	paymentId string,
	result *common.InterceptResult,
) {
	payment, ok := i.inflightPayments[paymentId]
	if !ok {
		// Apparently this payment is already finalized.
		return
	}

	// Stops the timeout listeners
	payment.closeTimeoutChan()

	for _, part := range payment.parts {
		part.resolution <- result
	}
	delete(i.inflightPayments, paymentId)
}

func (i *Interceptor) Intercept(req common.InterceptRequest) common.InterceptResult {
	resolution := make(chan *common.InterceptResult, 1)
	i.newPart <- &partState{
		req:        &req,
		resolution: resolution,
	}
	res := <-resolution
	return *res
}

func (i *Interceptor) failPart(
	payment *paymentState,
	part *partState,
	message []byte,
) {
	part.resolution <- &common.InterceptResult{
		Action:         common.INTERCEPT_FAIL_HTLC_WITH_CODE,
		FailureMessage: message,
	}
	delete(payment.parts, part.req.HtlcId())
	if len(payment.parts) == 0 {
		payment.closeTimeoutChan()
		delete(i.inflightPayments, part.req.PaymentId())
	}
}
