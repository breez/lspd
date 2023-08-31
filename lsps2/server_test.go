package lsps2

import (
	"context"
	"fmt"
	"testing"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps0/status"
	"github.com/breez/lspd/shared"
	"github.com/stretchr/testify/assert"
)

var token = "blah"
var node = func() *shared.Node {
	return &shared.Node{
		NodeConfig: &config.NodeConfig{
			MinPaymentSizeMsat: 1000,
			MaxPaymentSizeMsat: 10000,
			TimeLockDelta:      143,
		},
	}
}

func Test_GetInfo_UnsupportedVersion(t *testing.T) {
	n := &mockNodesService{}
	o := &mockOpeningService{}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, nil, st)
	_, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 2,
		Token:   &token,
	})

	status := status.Convert(err)
	assert.Equal(t, uint32(1), uint32(status.Code))
	assert.Equal(t, "unsupported_version", status.Message)
}

func Test_GetInfo_InvalidToken(t *testing.T) {
	n := &mockNodesService{
		err: shared.ErrNodeNotFound,
	}
	o := &mockOpeningService{}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, nil, st)
	_, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 1,
		Token:   &token,
	})

	status := status.Convert(err)
	assert.Equal(t, uint32(2), uint32(status.Code))
	assert.Equal(t, "unrecognized_or_stale_token", status.Message)
}

func Test_GetInfo_EmptyMenu(t *testing.T) {
	node := node()
	n := &mockNodesService{node: node}
	o := &mockOpeningService{menu: []*shared.OpeningFeeParams{}}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, node, st)
	resp, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 1,
		Token:   &token,
	})

	assert.Nil(t, err)
	assert.Equal(t, []*OpeningFeeParams{}, resp.OpeningFeeParamsMenu)
	assert.Equal(t, node.NodeConfig.MinPaymentSizeMsat, resp.MinPaymentSizeMsat)
	assert.Equal(t, node.NodeConfig.MaxPaymentSizeMsat, resp.MaxPaymentSizeMsat)
}

func Test_GetInfo_PopulatedMenu_Ordered(t *testing.T) {
	node := node()
	n := &mockNodesService{node: node}
	o := &mockOpeningService{menu: []*shared.OpeningFeeParams{
		{
			MinFeeMsat:           1,
			Proportional:         2,
			ValidUntil:           "a",
			MinLifetime:          3,
			MaxClientToSelfDelay: 4,
			Promise:              "b",
		},
		{
			MinFeeMsat:           5,
			Proportional:         6,
			ValidUntil:           "c",
			MinLifetime:          7,
			MaxClientToSelfDelay: 8,
			Promise:              "d",
		},
	}}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, node, st)
	resp, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 1,
		Token:   &token,
	})

	assert.Nil(t, err)
	assert.Len(t, resp.OpeningFeeParamsMenu, 2)

	assert.Equal(t, uint64(1), resp.OpeningFeeParamsMenu[0].MinFeeMsat)
	assert.Equal(t, uint32(2), resp.OpeningFeeParamsMenu[0].Proportional)
	assert.Equal(t, "a", resp.OpeningFeeParamsMenu[0].ValidUntil)
	assert.Equal(t, uint32(3), resp.OpeningFeeParamsMenu[0].MinLifetime)
	assert.Equal(t, uint32(4), resp.OpeningFeeParamsMenu[0].MaxClientToSelfDelay)
	assert.Equal(t, "b", resp.OpeningFeeParamsMenu[0].Promise)

	assert.Equal(t, uint64(5), resp.OpeningFeeParamsMenu[1].MinFeeMsat)
	assert.Equal(t, uint32(6), resp.OpeningFeeParamsMenu[1].Proportional)
	assert.Equal(t, "c", resp.OpeningFeeParamsMenu[1].ValidUntil)
	assert.Equal(t, uint32(7), resp.OpeningFeeParamsMenu[1].MinLifetime)
	assert.Equal(t, uint32(8), resp.OpeningFeeParamsMenu[1].MaxClientToSelfDelay)
	assert.Equal(t, "d", resp.OpeningFeeParamsMenu[1].Promise)

	assert.Equal(t, node.NodeConfig.MinPaymentSizeMsat, resp.MinPaymentSizeMsat)
	assert.Equal(t, node.NodeConfig.MaxPaymentSizeMsat, resp.MaxPaymentSizeMsat)
}

func Test_Buy_UnsupportedVersion(t *testing.T) {
	n := &mockNodesService{}
	o := &mockOpeningService{}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, nil, st)
	_, err := s.Buy(context.Background(), &BuyRequest{
		Version: 2,
	})

	status := status.Convert(err)
	assert.Equal(t, uint32(1), uint32(status.Code))
	assert.Equal(t, "unsupported_version", status.Message)
}

func Test_Buy_InvalidFeeParams(t *testing.T) {
	node := node()
	n := &mockNodesService{}
	o := &mockOpeningService{
		invalid: true,
	}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, node, st)
	_, err := s.Buy(context.Background(), &BuyRequest{
		Version: 1,
		OpeningFeeParams: OpeningFeeParams{
			MinFeeMsat:           1,
			Proportional:         2,
			ValidUntil:           "2023-08-18T13:39:00.000Z",
			MinLifetime:          3,
			MaxClientToSelfDelay: 4,
			Promise:              "fake",
		},
	})

	status := status.Convert(err)
	assert.Equal(t, uint32(2), uint32(status.Code))
	assert.Equal(t, "invalid_opening_fee_params", status.Message)
}

func Test_Buy_PaymentSize(t *testing.T) {
	tests := []struct {
		minFeeMsat  uint64
		paymentSize uint64
		success     bool
		code        uint32
		message     string
	}{
		{
			minFeeMsat:  0,
			paymentSize: 999,
			success:     false,
			code:        3,
			message:     "payment_size_too_small",
		},
		{
			minFeeMsat:  0,
			paymentSize: 1000,
			success:     true,
		},
		{
			minFeeMsat:  0,
			paymentSize: 1001,
			success:     true,
		},
		{
			minFeeMsat:  0,
			paymentSize: 9999,
			success:     true,
		},
		{
			minFeeMsat:  0,
			paymentSize: 10000,
			success:     true,
		},
		{
			minFeeMsat:  0,
			paymentSize: 10001,
			success:     false,
			code:        4,
			message:     "payment_size_too_large",
		},
		{
			minFeeMsat:  2000,
			paymentSize: 1999,
			success:     false,
			code:        3,
			message:     "payment_size_too_small",
		},
		{
			minFeeMsat:  2000,
			paymentSize: 2000,
			success:     false,
			code:        3,
			message:     "payment_size_too_small",
		},
		{
			minFeeMsat:  2000,
			paymentSize: 2001,
			success:     true,
		},
	}

	for _, c := range tests {
		t.Run(
			fmt.Sprintf("paymentsize_%d", c.paymentSize),
			func(t *testing.T) {
				node := node()
				n := &mockNodesService{}
				o := &mockOpeningService{}
				st := &mockLsps2Store{}
				s := NewLsps2Server(o, n, node, st)
				ctx := context.WithValue(context.Background(), lsps0.PeerContextKey, "peer id")
				_, err := s.Buy(ctx, &BuyRequest{
					Version: 1,
					OpeningFeeParams: OpeningFeeParams{
						MinFeeMsat:           c.minFeeMsat,
						Proportional:         2,
						ValidUntil:           "2023-08-18T13:39:00.000Z",
						MinLifetime:          3,
						MaxClientToSelfDelay: 4,
						Promise:              "fake",
					},
					PaymentSizeMsat: &c.paymentSize,
				})

				if c.success {
					assert.NoError(t, err)
				} else {
					assert.Error(t, err)
					status := status.Convert(err)
					assert.Equal(t, uint32(c.code), uint32(status.Code))
					assert.Equal(t, c.message, status.Message)
				}
			},
		)
	}
}

func Test_Buy_Registered(t *testing.T) {
	node := node()
	n := &mockNodesService{}
	o := &mockOpeningService{}
	st := &mockLsps2Store{}
	s := NewLsps2Server(o, n, node, st)
	paymentSize := uint64(1000)
	peerid := "peer id"
	ctx := context.WithValue(context.Background(), lsps0.PeerContextKey, peerid)
	resp, _ := s.Buy(ctx, &BuyRequest{
		Version: 1,
		OpeningFeeParams: OpeningFeeParams{
			MinFeeMsat:           1,
			Proportional:         2,
			ValidUntil:           "2023-08-18T13:39:00.000Z",
			MinLifetime:          3,
			MaxClientToSelfDelay: 4,
			Promise:              "fake",
		},
		PaymentSizeMsat: &paymentSize,
	})

	assert.NotNil(t, st.req)
	assert.Equal(t, node.NodeConfig.NodePubkey, st.req.LspId)
	assert.Equal(t, peerid, st.req.PeerId)
	assert.Equal(t, OpeningMode_MppFixedInvoice, st.req.Mode)
	assert.Equal(t, &paymentSize, st.req.PaymentSizeMsat)
	assert.NotZero(t, uint64(st.req.Scid))
	assert.Equal(t, uint64(1), st.req.OpeningFeeParams.MinFeeMsat)
	assert.Equal(t, uint32(2), st.req.OpeningFeeParams.Proportional)
	assert.Equal(t, "2023-08-18T13:39:00.000Z", st.req.OpeningFeeParams.ValidUntil)
	assert.Equal(t, uint32(3), st.req.OpeningFeeParams.MinLifetime)
	assert.Equal(t, uint32(4), st.req.OpeningFeeParams.MaxClientToSelfDelay)
	assert.Equal(t, "fake", st.req.OpeningFeeParams.Promise)

	assert.Equal(t, st.req.Scid.ToString(), resp.JitChannelScid)
	assert.Equal(t, false, resp.ClientTrustsLsp)
	assert.Equal(t, uint32(143), resp.LspCltvExpiryDelta)
}
