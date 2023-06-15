package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/breez/lspd/cln"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lnd"
	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/caddyserver/certmagic"
	ecies "github.com/ecies/go/v2"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type grpcServer struct {
	address         string
	certmagicDomain string
	lis             net.Listener
	s               *grpc.Server
	nodes           map[string]*node
	c               lspdrpc.ChannelOpenerServer
}

type nodeContext struct {
	token string
	node  *node
}

type node struct {
	client              lightning.Client
	nodeConfig          *config.NodeConfig
	privateKey          *btcec.PrivateKey
	publicKey           *btcec.PublicKey
	eciesPrivateKey     *ecies.PrivateKey
	eciesPublicKey      *ecies.PublicKey
	openChannelReqGroup singleflight.Group
}

func NewGrpcServer(
	configs []*config.NodeConfig,
	address string,
	certmagicDomain string,
	c lspdrpc.ChannelOpenerServer,
) (*grpcServer, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no nodes supplied")
	}

	nodes := make(map[string]*node)
	for _, config := range configs {
		pk, err := hex.DecodeString(config.LspdPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("hex.DecodeString(config.lspdPrivateKey=%v) error: %v", config.LspdPrivateKey, err)
		}

		eciesPrivateKey := ecies.NewPrivateKeyFromBytes(pk)
		eciesPublicKey := eciesPrivateKey.PublicKey
		privateKey, publicKey := btcec.PrivKeyFromBytes(pk)

		node := &node{
			nodeConfig:      config,
			privateKey:      privateKey,
			publicKey:       publicKey,
			eciesPrivateKey: eciesPrivateKey,
			eciesPublicKey:  eciesPublicKey,
		}

		if config.Lnd == nil && config.Cln == nil {
			return nil, fmt.Errorf("node has to be either cln or lnd")
		}

		if config.Lnd != nil && config.Cln != nil {
			return nil, fmt.Errorf("node cannot be both cln and lnd")
		}

		if config.Lnd != nil {
			node.client, err = lnd.NewLndClient(config.Lnd)
			if err != nil {
				return nil, err
			}
		}

		if config.Cln != nil {
			node.client, err = cln.NewClnClient(config.Cln.SocketPath)
			if err != nil {
				return nil, err
			}
		}

		for _, token := range config.Tokens {
			_, exists := nodes[token]
			if exists {
				return nil, fmt.Errorf("cannot have multiple nodes with the same token")
			}

			nodes[token] = node
		}
	}

	return &grpcServer{
		address:         address,
		certmagicDomain: certmagicDomain,
		nodes:           nodes,
		c:               c,
	}, nil
}

func (s *grpcServer) Start() error {
	// Make sure all nodes are available and set name and pubkey if not set
	// in config.
	for _, n := range s.nodes {
		info, err := n.client.GetInfo()
		if err != nil {
			return fmt.Errorf("failed to get info from host %s", n.nodeConfig.Host)
		}

		if n.nodeConfig.Name == "" {
			n.nodeConfig.Name = info.Alias
		}

		if n.nodeConfig.NodePubkey == "" {
			n.nodeConfig.NodePubkey = info.Pubkey
		}
	}

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
					node, ok := s.nodes[token]
					if !ok {
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
