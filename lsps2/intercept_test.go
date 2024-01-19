package lsps2

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/assert"
)

var defaultScid uint64 = 123
var defaultPaymentSizeMsat uint64 = 1_000_000
var defaultMinViableAmount uint64 = defaultOpeningFeeParams().MinFeeMsat + defaultConfig().HtlcMinimumMsat
var defaultFee, _ = computeOpeningFee(
	defaultPaymentSizeMsat,
	defaultOpeningFeeParams().Proportional,
	defaultOpeningFeeParams().MinFeeMsat,
)
var defaultChainHash = chainhash.Hash([32]byte{})
var defaultOutPoint = wire.NewOutPoint(&defaultChainHash, 0)
var defaultChannelScid uint64 = 456
var defaultChanResult = &lightning.GetChannelResult{
	HtlcMinimumMsat:    defaultConfig().HtlcMinimumMsat,
	InitialChannelID:   lightning.ShortChannelID(defaultChannelScid),
	ConfirmedChannelID: lightning.ShortChannelID(defaultChannelScid),
}

func defaultOpeningFeeParams() common.OpeningFeeParams {
	return common.OpeningFeeParams{
		MinFeeMsat:           1000,
		Proportional:         1000,
		ValidUntil:           time.Now().UTC().Add(5 * time.Hour).Format(lsps0.TIME_FORMAT),
		MinLifetime:          1000,
		MaxClientToSelfDelay: 2016,
		Promise:              "fake",
	}
}
func defaultStore() *mockLsps2Store {
	return &mockLsps2Store{
		registrations: map[uint64]*BuyRegistration{
			defaultScid: {
				PeerId:           "peer",
				Scid:             lightning.ShortChannelID(defaultScid),
				Mode:             OpeningMode_NoMppVarInvoice,
				OpeningFeeParams: defaultOpeningFeeParams(),
			},
		},
	}
}

func mppStore() *mockLsps2Store {
	s := defaultStore()
	for _, r := range s.registrations {
		r.Mode = OpeningMode_MppFixedInvoice
		r.PaymentSizeMsat = &defaultPaymentSizeMsat
	}
	return s
}

func defaultClient() *mockLightningClient {
	return &mockLightningClient{
		openResponses: []*wire.OutPoint{
			defaultOutPoint,
		},
		getChanResponses: []*lightning.GetChannelResult{
			defaultChanResult,
		},
	}
}

func defaultFeeEstimator() *mockFeeEstimator {
	return nil
}

func defaultopeningService() *mockOpeningService {
	return &mockOpeningService{
		isCurrentChainFeeCheaper: false,
	}
}

func defaultConfig() *InterceptorConfig {
	var minConfs uint32 = 1
	return &InterceptorConfig{
		AdditionalChannelCapacitySat: 100_000,
		MinConfs:                     &minConfs,
		TargetConf:                   6,
		FeeStrategy:                  chain.FeeStrategyEconomy,
		MinPaymentSizeMsat:           1_000,
		MaxPaymentSizeMsat:           4_000_000_000,
		TimeLockDelta:                144,
		HtlcMinimumMsat:              100,
	}
}

type interceptP struct {
	store          *mockLsps2Store
	openingService *mockOpeningService
	client         *mockLightningClient
	feeEstimator   *mockFeeEstimator
	config         *InterceptorConfig
}

func setupInterceptor(
	ctx context.Context,
	p *interceptP,
) *Interceptor {
	var store *mockLsps2Store
	if p != nil && p.store != nil {
		store = p.store
	} else {
		store = defaultStore()
	}

	var client *mockLightningClient
	if p != nil && p.client != nil {
		client = p.client
	} else {
		client = defaultClient()
	}

	var f *mockFeeEstimator
	if p != nil && p.feeEstimator != nil {
		f = p.feeEstimator
	} else {
		f = defaultFeeEstimator()
	}

	var config *InterceptorConfig
	if p != nil && p.config != nil {
		config = p.config
	} else {
		config = defaultConfig()
	}

	var openingService *mockOpeningService
	if p != nil && p.openingService != nil {
		openingService = p.openingService
	} else {
		openingService = defaultopeningService()
	}

	i := NewInterceptHandler(store, openingService, client, f, config)
	go i.Start(ctx)
	return i
}

type part struct {
	id        string
	scid      uint64
	ph        []byte
	amt       uint64
	cltvDelta uint32
}

func createPart(p *part) common.InterceptRequest {
	id := "first"
	if p != nil && p.id != "" {
		id = p.id
	}

	scid := lightning.ShortChannelID(defaultScid)
	if p != nil && p.scid != 0 {
		scid = lightning.ShortChannelID(p.scid)
	}

	ph := []byte("fake payment hash")
	if p != nil && p.ph != nil && len(p.ph) > 0 {
		ph = p.ph
	}

	var amt uint64 = 1_000_000
	if p != nil && p.amt != 0 {
		amt = p.amt
	}

	var cltv uint32 = 146
	if p != nil && p.cltvDelta != 0 {
		cltv = p.cltvDelta
	}

	return common.InterceptRequest{
		Identifier:         id,
		Scid:               scid,
		PaymentHash:        ph,
		IncomingAmountMsat: amt,
		OutgoingAmountMsat: amt,
		IncomingExpiry:     100 + cltv,
		OutgoingExpiry:     100,
	}
}

func runIntercept(i *Interceptor, req common.InterceptRequest, res *common.InterceptResult, wg *sync.WaitGroup) {
	go func() {
		*res = i.Intercept(req)
		wg.Done()
	}()
}

func assertEmpty(t *testing.T, i *Interceptor) {
	assert.Empty(t, i.inflightPayments)
	assert.Empty(t, i.newPart)
	assert.Empty(t, i.registrationFetched)
	assert.Empty(t, i.paymentChanOpened)
	assert.Empty(t, i.paymentFailure)
	assert.Empty(t, i.paymentReady)
}

// Asserts that a part that is not associated with a bought channel is not
// handled by the interceptor. This allows the legacy interceptor to pick up
// from there.
func Test_NotBought_SinglePart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)
	res := i.Intercept(createPart(&part{scid: 999}))
	assert.Equal(t, common.INTERCEPT_RESUME, res.Action)
	assertEmpty(t, i)
}

func Test_NotBought_TwoParts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	var res1 common.InterceptResult
	runIntercept(i, createPart(&part{id: "first", scid: 999}), &res1, &wg)

	var res2 common.InterceptResult
	runIntercept(i, createPart(&part{id: "second", scid: 999}), &res2, &wg)
	wg.Wait()
	assert.Equal(t, common.INTERCEPT_RESUME, res1.Action)
	assert.Equal(t, common.INTERCEPT_RESUME, res2.Action)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment works in the happy flow.
func Test_NoMpp_Happyflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assert.Equal(t, defaultPaymentSizeMsat-defaultFee, res.AmountMsat)
	assert.Equal(t, defaultFee, *res.FeeMsat)
	assert.Equal(t, defaultChannelScid, uint64(res.Scid))
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment works with the exact minimum
// amount.
func Test_NoMpp_AmountMinFeePlusHtlcMinPlusOne(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{amt: defaultMinViableAmount}))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assert.Equal(t, defaultConfig().HtlcMinimumMsat, res.AmountMsat)
	assert.Equal(t, defaultOpeningFeeParams().MinFeeMsat, *res.FeeMsat)
	assert.Equal(t, defaultChannelScid, uint64(res.Scid))
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment fails with the exact minimum
// amount minus one.
func Test_NoMpp_AmtBelowMinimum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{amt: defaultMinViableAmount - 1}))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment succeeds with the exact
// maximum amount.
func Test_NoMpp_AmtAtMaximum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{amt: defaultConfig().MaxPaymentSizeMsat}))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment fails with the exact
// maximum amount plus one.
func Test_NoMpp_AmtAboveMaximum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{amt: defaultConfig().MaxPaymentSizeMsat + 1}))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment fails when the cltv delta is
// less than cltv delta.
func Test_NoMpp_CltvDeltaBelowMinimum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{cltvDelta: 143}))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_INCORRECT_CLTV_EXPIRY, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment succeeds when the cltv delta
// is higher than expected.
func Test_NoMpp_HigherCltvDelta(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, nil)

	res := i.Intercept(createPart(&part{cltvDelta: 1000}))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assertEmpty(t, i)
}

// Asserts that a no-MPP+var-invoice mode payment fails if the opening params
// have expired.
func Test_NoMpp_ParamsExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := defaultStore()
	store.registrations[defaultScid].OpeningFeeParams.ValidUntil = time.Now().
		UTC().Add(-time.Nanosecond).Format(lsps0.TIME_FORMAT)
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

func Test_NoMpp_ChannelAlreadyOpened_NotComplete_Forwards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := defaultStore()
	store.registrations[defaultScid].ChannelPoint = defaultOutPoint
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assertEmpty(t, i)
}

func Test_NoMpp_ChannelAlreadyOpened_Complete_Fails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := defaultStore()
	store.registrations[defaultScid].ChannelPoint = defaultOutPoint
	store.registrations[defaultScid].IsComplete = true
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment succeeds in the happy flow
// case.
func Test_Mpp_SinglePart_Happyflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, &interceptP{store: mppStore()})

	res := i.Intercept(createPart(&part{amt: defaultPaymentSizeMsat}))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assert.Equal(t, defaultPaymentSizeMsat-defaultFee, res.AmountMsat)
	assert.Equal(t, defaultFee, *res.FeeMsat)
	assert.Equal(t, defaultChannelScid, uint64(res.Scid))
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment times out when it receives only
// a single part below payment_size_msat.
func Test_Mpp_SinglePart_AmtTooSmall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := defaultConfig()
	config.MppTimeout = time.Millisecond * 500
	i := setupInterceptor(ctx, &interceptP{store: mppStore(), config: config})

	start := time.Now()
	res := i.Intercept(createPart(&part{amt: defaultPaymentSizeMsat - 1}))
	end := time.Now()
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_TEMPORARY_CHANNEL_FAILURE, res.FailureCode)
	assert.GreaterOrEqual(t, end.Sub(start).Milliseconds(), config.MppTimeout.Milliseconds())
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment finalizes after it receives the
// second part that finalizes the payment.
func Test_Mpp_TwoParts_FinalizedOnSecond(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := defaultConfig()
	config.MppTimeout = time.Millisecond * 500
	i := setupInterceptor(ctx, &interceptP{store: mppStore(), config: config})

	var wg sync.WaitGroup
	wg.Add(2)
	var res1 common.InterceptResult
	var res2 common.InterceptResult
	var t1 time.Time
	var t2 time.Time
	start := time.Now()
	go func() {
		res1 = i.Intercept(createPart(&part{
			id:  "first",
			amt: defaultPaymentSizeMsat - defaultConfig().HtlcMinimumMsat,
		}))
		t1 = time.Now()
		wg.Done()
	}()

	<-time.After(time.Millisecond * 250)

	go func() {
		res2 = i.Intercept(createPart(&part{
			id:  "second",
			amt: defaultConfig().HtlcMinimumMsat,
		}))
		t2 = time.Now()
		wg.Done()
	}()

	wg.Wait()
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res1.Action)
	assert.Equal(t, defaultPaymentSizeMsat-defaultConfig().HtlcMinimumMsat-defaultFee, res1.AmountMsat)
	assert.Equal(t, defaultFee, *res1.FeeMsat)

	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res2.Action)
	assert.Equal(t, defaultConfig().HtlcMinimumMsat, res2.AmountMsat)
	assert.Nil(t, res2.FeeMsat)

	assert.LessOrEqual(t, int64(250), t1.Sub(start).Milliseconds())
	assert.LessOrEqual(t, int64(250), t2.Sub(start).Milliseconds())
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment with the following parts
// 1) payment size - htlc minimum
// 2) htlc minimum - 1
// 3) htlc minimum
// still succeeds. The second part is dropped, but the third part completes the
// payment.
func Test_Mpp_BadSecondPart_ThirdPartCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := defaultConfig()
	config.MppTimeout = time.Millisecond * 500
	i := setupInterceptor(ctx, &interceptP{store: mppStore(), config: config})

	var wg sync.WaitGroup
	wg.Add(2)
	var res1 common.InterceptResult
	var res2 common.InterceptResult
	var res3 common.InterceptResult
	var t1 time.Time
	var t2 time.Time
	var t3 time.Time
	start := time.Now()
	go func() {
		res1 = i.Intercept(createPart(&part{
			id:  "first",
			amt: defaultPaymentSizeMsat - defaultConfig().HtlcMinimumMsat,
		}))
		t1 = time.Now()
		wg.Done()
	}()

	<-time.After(time.Millisecond * 100)
	res2 = i.Intercept(createPart(&part{
		id:  "second",
		amt: defaultConfig().HtlcMinimumMsat - 1,
	}))
	t2 = time.Now()

	<-time.After(time.Millisecond * 100)
	go func() {
		res3 = i.Intercept(createPart(&part{
			id:  "third",
			amt: defaultConfig().HtlcMinimumMsat,
		}))
		t3 = time.Now()
		wg.Done()
	}()

	wg.Wait()
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res1.Action)
	assert.Equal(t, defaultPaymentSizeMsat-defaultConfig().HtlcMinimumMsat-defaultFee, res1.AmountMsat)
	assert.Equal(t, defaultFee, *res1.FeeMsat)

	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res2.Action)
	assert.Equal(t, common.FAILURE_AMOUNT_BELOW_MINIMUM, res2.FailureCode)

	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res3.Action)
	assert.Equal(t, defaultConfig().HtlcMinimumMsat, res3.AmountMsat)
	assert.Nil(t, res3.FeeMsat)

	assert.LessOrEqual(t, int64(200), t1.Sub(start).Milliseconds())
	assert.Greater(t, int64(200), t2.Sub(start).Milliseconds())
	assert.LessOrEqual(t, int64(200), t3.Sub(start).Milliseconds())

	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment fails when the cltv delta is
// less than cltv delta.
func Test_Mpp_CltvDeltaBelowMinimum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, &interceptP{store: mppStore()})

	res := i.Intercept(createPart(&part{cltvDelta: 143}))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_INCORRECT_CLTV_EXPIRY, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment succeeds when the cltv delta
// is higher than expected.
func Test_Mpp_HigherCltvDelta(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	i := setupInterceptor(ctx, &interceptP{store: mppStore()})

	res := i.Intercept(createPart(&part{cltvDelta: 1000}))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment fails if the opening params
// have expired.
func Test_Mpp_ParamsExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := mppStore()
	store.registrations[defaultScid].OpeningFeeParams.ValidUntil = time.Now().
		UTC().Add(-time.Nanosecond).Format(lsps0.TIME_FORMAT)
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode payment fails if the opening params
// expire while the part is in-flight.
func Test_Mpp_ParamsExpireInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := mppStore()
	i := setupInterceptor(ctx, &interceptP{store: store})

	start := time.Now()
	store.registrations[defaultScid].OpeningFeeParams.ValidUntil = start.
		UTC().Add(time.Millisecond * 250).Format(lsps0.TIME_FORMAT)

	var res1 common.InterceptResult
	var res2 common.InterceptResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		res1 = i.Intercept(createPart(&part{
			id:  "first",
			amt: defaultPaymentSizeMsat - defaultConfig().HtlcMinimumMsat,
		}))
		wg.Done()
	}()

	<-time.After(time.Millisecond * 300)
	res2 = i.Intercept(createPart(&part{
		id:  "second",
		amt: defaultConfig().HtlcMinimumMsat,
	}))

	wg.Wait()
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res1.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res1.FailureCode)
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res2.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res2.FailureCode)

	assertEmpty(t, i)
}

// Asserts that a MPP+fixed-invoice mode replacement of a part ignores that
// part, and the replacement is used for completing the payment
func Test_Mpp_PartReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i := setupInterceptor(ctx, &interceptP{store: mppStore()})

	var wg sync.WaitGroup
	wg.Add(3)
	var res1 common.InterceptResult
	var res2 common.InterceptResult
	var res3 common.InterceptResult
	var t1 time.Time
	var t2 time.Time
	var t3 time.Time
	start := time.Now()
	go func() {
		res1 = i.Intercept(createPart(&part{
			id:  "first",
			amt: defaultPaymentSizeMsat - defaultConfig().HtlcMinimumMsat,
		}))
		t1 = time.Now()
		wg.Done()
	}()

	<-time.After(time.Millisecond * 100)
	go func() {
		res2 = i.Intercept(createPart(&part{
			id:  "first",
			amt: defaultPaymentSizeMsat - defaultConfig().HtlcMinimumMsat,
		}))
		t2 = time.Now()
		wg.Done()
	}()

	<-time.After(time.Millisecond * 100)
	go func() {
		res3 = i.Intercept(createPart(&part{
			id:  "second",
			amt: defaultConfig().HtlcMinimumMsat,
		}))
		t3 = time.Now()
		wg.Done()
	}()

	wg.Wait()
	assert.Equal(t, common.INTERCEPT_IGNORE, res1.Action)

	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res2.Action)
	assert.Equal(t, defaultPaymentSizeMsat-defaultConfig().HtlcMinimumMsat-defaultFee, res2.AmountMsat)
	assert.Equal(t, defaultFee, *res2.FeeMsat)

	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res3.Action)
	assert.Equal(t, defaultConfig().HtlcMinimumMsat, res3.AmountMsat)
	assert.Nil(t, res3.FeeMsat)

	assert.LessOrEqual(t, int64(100), t1.Sub(start).Milliseconds())
	assert.LessOrEqual(t, int64(200), t2.Sub(start).Milliseconds())
	assert.LessOrEqual(t, int64(200), t3.Sub(start).Milliseconds())

	assertEmpty(t, i)
}

func Test_Mpp_ChannelAlreadyOpened_NotComplete_Forwards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := mppStore()
	store.registrations[defaultScid].ChannelPoint = defaultOutPoint
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
	assertEmpty(t, i)
}

func Test_Mpp_ChannelAlreadyOpened_Complete_Fails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := mppStore()
	store.registrations[defaultScid].ChannelPoint = defaultOutPoint
	store.registrations[defaultScid].IsComplete = true
	i := setupInterceptor(ctx, &interceptP{store: store})

	res := i.Intercept(createPart(nil))
	assert.Equal(t, common.INTERCEPT_FAIL_HTLC_WITH_CODE, res.Action)
	assert.Equal(t, common.FAILURE_UNKNOWN_NEXT_PEER, res.FailureCode)
	assertEmpty(t, i)
}

func Test_Mpp_Performance(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	paymentCount := 100
	partCount := 10
	store := &mockLsps2Store{
		delay:         time.Millisecond * 500,
		registrations: make(map[uint64]*BuyRegistration),
	}

	client := &mockLightningClient{}
	for paymentNo := 0; paymentNo < paymentCount; paymentNo++ {
		scid := uint64(paymentNo + 1_000_000)
		client.getChanResponses = append(client.getChanResponses, defaultChanResult)
		client.openResponses = append(client.openResponses, defaultOutPoint)
		store.registrations[scid] = &BuyRegistration{
			PeerId:           strconv.FormatUint(scid, 10),
			Scid:             lightning.ShortChannelID(scid),
			Mode:             OpeningMode_MppFixedInvoice,
			OpeningFeeParams: defaultOpeningFeeParams(),
			PaymentSizeMsat:  &defaultPaymentSizeMsat,
		}
	}
	i := setupInterceptor(ctx, &interceptP{store: store, client: client})
	var wg sync.WaitGroup
	wg.Add(partCount * paymentCount)
	start := time.Now()
	for paymentNo := 0; paymentNo < paymentCount; paymentNo++ {
		for partNo := 0; partNo < partCount; partNo++ {
			scid := paymentNo + 1_000_000
			id := fmt.Sprintf("%d|%d", paymentNo, partNo)
			var a [8]byte
			binary.BigEndian.PutUint64(a[:], uint64(scid))
			ph := sha256.Sum256(a[:])

			go func() {
				res := i.Intercept(createPart(&part{
					scid: uint64(scid),
					id:   id,
					ph:   ph[:],
					amt:  defaultPaymentSizeMsat / uint64(partCount),
				}))

				assert.Equal(t, common.INTERCEPT_RESUME_WITH_ONION, res.Action)
				wg.Done()
			}()
		}
	}
	wg.Wait()
	end := time.Now()

	assert.LessOrEqual(t, end.Sub(start).Milliseconds(), int64(1000))
	assertEmpty(t, i)
}
