package lsps2

import (
	"context"

	"github.com/breez/lspd/lsps0"
)

type GetVersionsRequest struct {
}

type GetVersionsResponse struct {
	Versions []int32 `json:"versions"`
}

type Lsps2Server interface {
	GetVersions(ctx context.Context, request *GetVersionsRequest) (*GetVersionsResponse, error)
}
type server struct{}

func NewLsps2Server() Lsps2Server {
	return &server{}
}

func (s *server) GetVersions(
	ctx context.Context,
	request *GetVersionsRequest,
) (*GetVersionsResponse, error) {
	return &GetVersionsResponse{
		Versions: []int32{1},
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
			},
		},
		l,
	)
}
