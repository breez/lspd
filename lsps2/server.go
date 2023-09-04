package lsps2

import (
	"context"
	"log"

	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps0/codes"
	"github.com/breez/lspd/lsps0/status"
	"github.com/breez/lspd/shared"
)

var SupportedVersion uint32 = 1

type GetVersionsRequest struct {
}

type GetVersionsResponse struct {
	Versions []uint32 `json:"versions"`
}

type GetInfoRequest struct {
	Version uint32  `json:"version"`
	Token   *string `json:"token,omitempty"`
}

type GetInfoResponse struct {
	OpeningFeeParamsMenu []*OpeningFeeParams `json:"opening_fee_params_menu"`
	MinPaymentSizeMsat   uint64              `json:"min_payment_size_msat,string"`
	MaxPaymentSizeMsat   uint64              `json:"max_payment_size_msat,string"`
}

type OpeningFeeParams struct {
	MinFeeMsat           uint64 `json:"min_fee_msat,string"`
	Proportional         uint32 `json:"proportional"`
	ValidUntil           string `json:"valid_until"`
	MinLifetime          uint32 `json:"min_lifetime"`
	MaxClientToSelfDelay uint32 `json:"max_client_to_self_delay"`
	Promise              string `json:"promise"`
}

type BuyRequest struct {
	Version          uint32           `json:"version"`
	OpeningFeeParams OpeningFeeParams `json:"opening_fee_params"`
	PaymentSizeMsat  *uint64          `json:"payment_size_msat,string,omitempty"`
}

type BuyResponse struct {
	JitChannelScid     string `json:"jit_channel_scid"`
	LspCltvExpiryDelta uint32 `json:"lsp_cltv_expiry_delta"`
	ClientTrustsLsp    bool   `json:"client_trusts_lsp"`
}

type Lsps2Server interface {
	GetVersions(ctx context.Context, request *GetVersionsRequest) (*GetVersionsResponse, error)
	GetInfo(ctx context.Context, request *GetInfoRequest) (*GetInfoResponse, error)
	Buy(ctx context.Context, request *BuyRequest) (*BuyResponse, error)
}
type server struct {
	openingService shared.OpeningService
	nodesService   shared.NodesService
	node           *shared.Node
	store          Lsps2Store
}

type OpeningMode int

const (
	OpeningMode_NoMppVarInvoice OpeningMode = 1
	OpeningMode_MppFixedInvoice OpeningMode = 2
)

func NewLsps2Server(
	openingService shared.OpeningService,
	nodesService shared.NodesService,
	node *shared.Node,
	store Lsps2Store,
) Lsps2Server {
	return &server{
		openingService: openingService,
		nodesService:   nodesService,
		node:           node,
		store:          store,
	}
}

func (s *server) GetVersions(
	ctx context.Context,
	request *GetVersionsRequest,
) (*GetVersionsResponse, error) {
	return &GetVersionsResponse{
		Versions: []uint32{SupportedVersion},
	}, nil
}

func (s *server) GetInfo(
	ctx context.Context,
	request *GetInfoRequest,
) (*GetInfoResponse, error) {
	if request.Version != uint32(SupportedVersion) {
		return nil, status.New(codes.Code(1), "unsupported_version").Err()
	}

	if request.Token == nil || *request.Token == "" {
		return nil, status.New(codes.Code(2), "unrecognized_or_stale_token").Err()
	}

	node, err := s.nodesService.GetNode(*request.Token)
	if err == shared.ErrNodeNotFound {
		return nil, status.New(codes.Code(2), "unrecognized_or_stale_token").Err()
	}
	if err != nil {
		log.Printf("Lsps2Server.GetInfo: nodesService.GetNode(%s) err: %v", *request.Token, err)
		return nil, status.New(codes.InternalError, "internal error").Err()
	}
	if node.NodeConfig.NodePubkey != s.node.NodeConfig.NodePubkey {
		log.Printf(
			"Lsps2Server.GetInfo: Got token '%s' on node '%s', but was meant for node '%s'",
			*request.Token,
			s.node.NodeConfig.NodePubkey,
			node.NodeConfig.NodePubkey,
		)
		return nil, status.New(codes.Code(2), "unrecognized_or_stale_token").Err()
	}

	m, err := s.openingService.GetFeeParamsMenu(*request.Token, node.PrivateKey)
	if err == shared.ErrNodeNotFound {
		return nil, status.New(codes.Code(2), "unrecognized_or_stale_token").Err()
	}
	if err != nil {
		log.Printf("Lsps2Server.GetInfo: openingService.GetFeeParamsMenu(%s) err: %v", *request.Token, err)
		return nil, status.New(codes.InternalError, "internal error").Err()
	}

	err = s.store.SavePromises(ctx, &SavePromises{
		Menu:  m,
		Token: *request.Token,
	})
	if err != nil {
		log.Printf("Lsps2Server.GetInfo: store.SavePromises(%+v, %s) err: %v", m, *request.Token, err)
		return nil, status.New(codes.InternalError, "internal error").Err()
	}

	menu := []*OpeningFeeParams{}
	for _, p := range m {
		menu = append(menu, &OpeningFeeParams{
			MinFeeMsat:           p.MinFeeMsat,
			Proportional:         p.Proportional,
			ValidUntil:           p.ValidUntil,
			MinLifetime:          p.MinLifetime,
			MaxClientToSelfDelay: p.MaxClientToSelfDelay,
			Promise:              p.Promise,
		})
	}
	return &GetInfoResponse{
		OpeningFeeParamsMenu: menu,
		MinPaymentSizeMsat:   node.NodeConfig.MinPaymentSizeMsat,
		MaxPaymentSizeMsat:   node.NodeConfig.MaxPaymentSizeMsat,
	}, nil
}

func (s *server) Buy(
	ctx context.Context,
	request *BuyRequest,
) (*BuyResponse, error) {
	if request.Version != uint32(SupportedVersion) {
		return nil, status.New(codes.Code(1), "unsupported_version").Err()
	}

	params := &shared.OpeningFeeParams{
		MinFeeMsat:           request.OpeningFeeParams.MinFeeMsat,
		Proportional:         request.OpeningFeeParams.Proportional,
		ValidUntil:           request.OpeningFeeParams.ValidUntil,
		MinLifetime:          request.OpeningFeeParams.MinLifetime,
		MaxClientToSelfDelay: request.OpeningFeeParams.MaxClientToSelfDelay,
		Promise:              request.OpeningFeeParams.Promise,
	}
	paramsValid := s.openingService.ValidateOpeningFeeParams(
		params,
		s.node.PublicKey,
	)
	if !paramsValid {
		return nil, status.New(codes.Code(2), "invalid_opening_fee_params").Err()
	}

	var mode OpeningMode
	if request.PaymentSizeMsat == nil || *request.PaymentSizeMsat == 0 {
		mode = OpeningMode_NoMppVarInvoice
	} else {
		mode = OpeningMode_MppFixedInvoice
		if *request.PaymentSizeMsat < s.node.NodeConfig.MinPaymentSizeMsat {
			return nil, status.New(codes.Code(3), "payment_size_too_small").Err()
		}
		if *request.PaymentSizeMsat > s.node.NodeConfig.MaxPaymentSizeMsat {
			return nil, status.New(codes.Code(4), "payment_size_too_large").Err()
		}

		openingFee, err := computeOpeningFee(
			*request.PaymentSizeMsat,
			request.OpeningFeeParams.Proportional,
			request.OpeningFeeParams.MinFeeMsat,
		)
		if err == ErrOverflow {
			return nil, status.New(codes.Code(4), "payment_size_too_large").Err()
		}
		if err != nil {
			log.Printf(
				"Lsps2Server.Buy: computeOpeningFee(%d, %d, %d) err: %v",
				*request.PaymentSizeMsat,
				request.OpeningFeeParams.Proportional,
				request.OpeningFeeParams.MinFeeMsat,
				err,
			)
			return nil, status.New(codes.InternalError, "internal error").Err()
		}

		if openingFee >= *request.PaymentSizeMsat {
			return nil, status.New(codes.Code(3), "payment_size_too_small").Err()
		}

		// NOTE: There's an option here to check for sufficient inbound liquidity as well.
	}

	// TODO: Restrict buying only one channel at a time?
	peerId, ok := ctx.Value(lsps0.PeerContextKey).(string)
	if !ok {
		log.Printf("Lsps2Server.Buy: Error: No peer id found on context.")
		return nil, status.New(codes.InternalError, "internal error").Err()
	}

	// Note, the odds to generate an already existing scid is about 10e-9 if you
	// have 100_000 channels with aliases. And about 10e-11 for 10_000 channels.
	// We call that negligable, and we'll assume the generated scid is unique.
	// (to calculate the odds, use the birthday paradox)
	scid, err := newScid()
	if err != nil {
		log.Printf("Lsps2Server.Buy: error generating new scid err: %v", err)
		return nil, status.New(codes.InternalError, "internal error").Err()
	}

	// RegisterBuy errors if the scid is not unique. But the node could have
	// 'actual' scids to collide with the newly generated scid. These are not in
	// our database.
	err = s.store.RegisterBuy(ctx, &RegisterBuy{
		LspId:            s.node.NodeConfig.NodePubkey,
		PeerId:           peerId,
		Scid:             *scid,
		OpeningFeeParams: *params,
		PaymentSizeMsat:  request.PaymentSizeMsat,
		Mode:             mode,
	})
	if err != nil {
		log.Printf("Lsps2Server.Buy: store.RegisterBuy err: %v", err)
		return nil, status.New(codes.InternalError, "internal error").Err()
	}

	return &BuyResponse{
		JitChannelScid:     scid.ToString(),
		LspCltvExpiryDelta: s.node.NodeConfig.TimeLockDelta,
		ClientTrustsLsp:    false,
	}, nil
}

func RegisterLsps2Server(s lsps0.ServiceRegistrar, l Lsps2Server) {
	s.RegisterService(
		&lsps0.ServiceDesc{
			ServiceName: "lsps2",
			HandlerType: (*Lsps2Server)(nil),
			Methods: []lsps0.MethodDesc{
				{
					MethodName: "lsps2.get_versions",
					Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
						in := new(GetVersionsRequest)
						if err := dec(in); err != nil {
							return nil, err
						}
						return srv.(Lsps2Server).GetVersions(ctx, in)
					},
				},
				{
					MethodName: "lsps2.get_info",
					Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
						in := new(GetInfoRequest)
						if err := dec(in); err != nil {
							return nil, err
						}
						return srv.(Lsps2Server).GetInfo(ctx, in)
					},
				},
				{
					MethodName: "lsps2.buy",
					Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
						in := new(BuyRequest)
						if err := dec(in); err != nil {
							return nil, err
						}
						return srv.(Lsps2Server).Buy(ctx, in)
					},
				},
			},
		},
		l,
	)
}
