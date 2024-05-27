package cln

import (
	"github.com/breez/lspd/cln/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewClnClient(address string) (rpc.NodeClient, error) {
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := rpc.NewNodeClient(conn)
	return client, nil
}
