package lsps2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
)

var ErrNotImplemented = errors.New("not implemented")

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
	menu    []*shared.OpeningFeeParams
	err     error
	invalid bool
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
	return !m.invalid
}

type mockLsps2Store struct {
	err           error
	req           *RegisterBuy
	registrations map[uint64]*BuyRegistration
	delay         time.Duration
}

func (s *mockLsps2Store) RegisterBuy(ctx context.Context, req *RegisterBuy) error {
	s.req = req
	return s.err
}

func (s *mockLsps2Store) GetBuyRegistration(ctx context.Context, scid lightning.ShortChannelID) (*BuyRegistration, error) {
	if s.delay.Nanoseconds() != 0 {
		<-time.After(s.delay)
	}
	reg, ok := s.registrations[uint64(scid)]
	if !ok {
		return nil, ErrNotFound
	}
	return reg, nil
}

func (s *mockLsps2Store) SetChannelOpened(ctx context.Context, channelOpened *ChannelOpened) error {
	return s.err
}

func (s *mockLsps2Store) SetCompleted(ctx context.Context, registrationId uint64) error {
	return nil
}

func (s *mockLsps2Store) SavePromises(ctx context.Context, req *SavePromises) error {
	return nil
}

type mockLightningClient struct {
	openResponses    []*wire.OutPoint
	openRequests     []*lightning.OpenChannelRequest
	getChanRequests  []*wire.OutPoint
	getChanResponses []*lightning.GetChannelResult
}

func (c *mockLightningClient) GetInfo() (*lightning.GetInfoResult, error) {
	return nil, ErrNotImplemented
}
func (c *mockLightningClient) IsConnected(destination []byte) (bool, error) {
	return false, ErrNotImplemented
}
func (c *mockLightningClient) OpenChannel(req *lightning.OpenChannelRequest) (*wire.OutPoint, error) {
	c.openRequests = append(c.openRequests, req)
	if len(c.openResponses) < len(c.openRequests) {
		return nil, fmt.Errorf("no responses available")
	}

	res := c.openResponses[len(c.openRequests)-1]
	if res == nil {
		return nil, fmt.Errorf("no response available")
	}
	return res, nil
}

func (c *mockLightningClient) GetChannel(peerID []byte, channelPoint wire.OutPoint) (*lightning.GetChannelResult, error) {
	c.getChanRequests = append(c.getChanRequests, &channelPoint)
	if len(c.getChanResponses) < len(c.getChanRequests) {
		return nil, fmt.Errorf("no responses available")
	}

	res := c.getChanResponses[len(c.getChanRequests)-1]
	if res == nil {
		return nil, fmt.Errorf("no response available")
	}
	return res, nil
}

func (c *mockLightningClient) GetPeerId(scid *lightning.ShortChannelID) ([]byte, error) {
	return nil, ErrNotImplemented
}
func (c *mockLightningClient) GetNodeChannelCount(nodeID []byte) (int, error) {
	return 0, ErrNotImplemented
}
func (c *mockLightningClient) GetClosedChannels(nodeID string, channelPoints map[string]uint64) (map[string]uint64, error) {
	return nil, ErrNotImplemented
}
func (c *mockLightningClient) WaitOnline(peerID []byte, deadline time.Time) error {
	return ErrNotImplemented
}
func (c *mockLightningClient) WaitChannelActive(peerID []byte, deadline time.Time) error {
	return ErrNotImplemented
}

type mockFeeEstimator struct {
}

func (f *mockFeeEstimator) EstimateFeeRate(context.Context, chain.FeeStrategy) (*chain.FeeEstimation, error) {
	return nil, ErrNotImplemented
}
