package lsps2

import (
	"context"
	"testing"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lsps0/status"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/stretchr/testify/assert"
)

type mockNodesService struct {
	node *shared.Node
	err  error
}

func (m *mockNodesService) GetNode(token string) (*shared.Node, error) {
	return m.node, m.err
}

func (m *mockNodesService) GetNodes() []*shared.Node {
	return []*shared.Node{m.node}
}

type mockOpeningService struct {
	menu  []*shared.OpeningFeeParams
	err   error
	valid bool
}

func (m *mockOpeningService) GetFeeParamsMenu(
	token string,
	privateKey *btcec.PrivateKey,
) ([]*shared.OpeningFeeParams, error) {
	return m.menu, m.err
}

func (m *mockOpeningService) ValidateOpeningFeeParams(
	params *shared.OpeningFeeParams,
	publicKey *btcec.PublicKey,
) bool {
	return m.valid
}

var token = "blah"
var node = &shared.Node{
	NodeConfig: &config.NodeConfig{
		MinPaymentSizeMsat: 123,
		MaxPaymentSizeMsat: 456,
	},
}

func Test_GetInfo_UnsupportedVersion(t *testing.T) {
	n := &mockNodesService{}
	o := &mockOpeningService{}
	s := NewLsps2Server(o, n, nil)
	_, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 2,
		Token:   &token,
	})

	st := status.Convert(err)
	assert.Equal(t, uint32(1), uint32(st.Code))
	assert.Equal(t, "unsupported_version", st.Message)
}

func Test_GetInfo_InvalidToken(t *testing.T) {
	n := &mockNodesService{
		err: shared.ErrNodeNotFound,
	}
	o := &mockOpeningService{}
	s := NewLsps2Server(o, n, nil)
	_, err := s.GetInfo(context.Background(), &GetInfoRequest{
		Version: 1,
		Token:   &token,
	})

	st := status.Convert(err)
	assert.Equal(t, uint32(2), uint32(st.Code))
	assert.Equal(t, "unrecognized_or_stale_token", st.Message)
}

func Test_GetInfo_EmptyMenu(t *testing.T) {
	n := &mockNodesService{node: node}
	o := &mockOpeningService{menu: []*shared.OpeningFeeParams{}}
	s := NewLsps2Server(o, n, node)
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
	s := NewLsps2Server(o, n, node)
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
