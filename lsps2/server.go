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

type Lsps2Server interface {
	GetVersions(ctx context.Context, request *GetVersionsRequest) (*GetVersionsResponse, error)
	GetInfo(ctx context.Context, request *GetInfoRequest) (*GetInfoResponse, error)
}
type server struct {
	openingService shared.OpeningService
	nodesService   shared.NodesService
	node           *shared.Node
}

func NewLsps2Server(
	openingService shared.OpeningService,
	nodesService shared.NodesService,
	node *shared.Node,
) Lsps2Server {
	return &server{
		openingService: openingService,
		nodesService:   nodesService,
		node:           node,
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
			},
		},
		l,
	)
}
