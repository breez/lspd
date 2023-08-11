package lsps0

import "context"

type ProtocolServer interface {
	ListProtocols(
		ctx context.Context,
		req *ListProtocolsRequest,
	) (*ListProtocolsResponse, error)
}
type protocolServer struct {
	protocols []uint32
}

type ListProtocolsRequest struct{}
type ListProtocolsResponse struct {
	Protocols []uint32 `json:"protocols"`
}

func NewProtocolServer(supportedProtocols []uint32) ProtocolServer {
	return &protocolServer{
		protocols: supportedProtocols,
	}
}

func (s *protocolServer) ListProtocols(
	ctx context.Context,
	req *ListProtocolsRequest,
) (*ListProtocolsResponse, error) {
	return &ListProtocolsResponse{
		Protocols: s.protocols,
	}, nil
}

func RegisterProtocolServer(s ServiceRegistrar, p ProtocolServer) {
	s.RegisterService(
		&ServiceDesc{
			ServiceName: "lsps0",
			HandlerType: (*ProtocolServer)(nil),
			Methods: []MethodDesc{
				{
					MethodName: "lsps0.list_protocols",
					Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
						in := new(ListProtocolsRequest)
						if err := dec(in); err != nil {
							return nil, err
						}
						return srv.(ProtocolServer).ListProtocols(ctx, in)
					},
				},
			},
		},
		p,
	)
}
