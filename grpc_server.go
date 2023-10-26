package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/breez/lspd/common"
	"github.com/breez/lspd/notifications"
	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/caddyserver/certmagic"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type grpcServer struct {
	nodesService    common.NodesService
	address         string
	certmagicDomain string
	lis             net.Listener
	s               *grpc.Server
	c               lspdrpc.ChannelOpenerServer
	n               notifications.NotificationsServer
}

type nodeContext struct {
	token string
	node  *common.Node
}

func NewGrpcServer(
	nodesService common.NodesService,
	address string,
	certmagicDomain string,
	c lspdrpc.ChannelOpenerServer,
	n notifications.NotificationsServer,
) (*grpcServer, error) {

	return &grpcServer{
		nodesService:    nodesService,
		address:         address,
		certmagicDomain: certmagicDomain,
		c:               c,
		n:               n,
	}, nil
}

func (s *grpcServer) Start() error {
	var lis net.Listener
	if s.certmagicDomain == "" {
		var err error
		lis, err = net.Listen("tcp", s.address)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
	} else {
		tlsConfig, err := certmagic.TLS([]string{s.certmagicDomain})
		if err != nil {
			log.Fatalf("failed to run certmagic: %v", err)
		}
		lis, err = tls.Listen("tcp", s.address, tlsConfig)
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
	}

	srv := grpc.NewServer(
		grpc_middleware.WithUnaryServerChain(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				for _, auth := range md.Get("authorization") {
					if !strings.HasPrefix(auth, "Bearer ") {
						continue
					}

					token := strings.Replace(auth, "Bearer ", "", 1)
					node, err := s.nodesService.GetNode(token)
					if err != nil {
						continue
					}

					return handler(context.WithValue(ctx, contextKey("node"), &nodeContext{
						token: token,
						node:  node,
					}), req)
				}
			}
			return nil, status.Errorf(codes.PermissionDenied, "Not authorized")
		}),
	)
	lspdrpc.RegisterChannelOpenerServer(srv, s.c)
	notifications.RegisterNotificationsServer(srv, s.n)

	s.s = srv
	s.lis = lis
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %v", err)
	}

	return nil
}

func (s *grpcServer) Stop() {
	srv := s.s
	if srv != nil {
		srv.GracefulStop()
	}
}
