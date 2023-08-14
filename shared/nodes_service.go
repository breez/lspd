package shared

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/breez/lspd/cln"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lnd"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"golang.org/x/sync/singleflight"
)

type Node struct {
	Client              lightning.Client
	NodeConfig          *config.NodeConfig
	PrivateKey          *btcec.PrivateKey
	PublicKey           *btcec.PublicKey
	EciesPrivateKey     *ecies.PrivateKey
	EciesPublicKey      *ecies.PublicKey
	OpenChannelReqGroup singleflight.Group
}

type NodesService interface {
	GetNode(token string) (*Node, error)
}
type nodesService struct {
	nodes map[string]*Node
}

func NewNodesService(configs []*config.NodeConfig) (NodesService, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no nodes supplied")
	}

	nodes := make(map[string]*Node)
	for _, config := range configs {
		pk, err := hex.DecodeString(config.LspdPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("hex.DecodeString(config.lspdPrivateKey=%v) error: %v", config.LspdPrivateKey, err)
		}

		eciesPrivateKey := ecies.NewPrivateKeyFromBytes(pk)
		eciesPublicKey := eciesPrivateKey.PublicKey
		privateKey, publicKey := btcec.PrivKeyFromBytes(pk)
		node := &Node{
			NodeConfig:      config,
			PrivateKey:      privateKey,
			PublicKey:       publicKey,
			EciesPrivateKey: eciesPrivateKey,
			EciesPublicKey:  eciesPublicKey,
		}

		if config.Lnd == nil && config.Cln == nil {
			return nil, fmt.Errorf("node has to be either cln or lnd")
		}

		if config.Lnd != nil && config.Cln != nil {
			return nil, fmt.Errorf("node cannot be both cln and lnd")
		}

		if config.Lnd != nil {
			node.Client, err = lnd.NewLndClient(config.Lnd)
			if err != nil {
				return nil, err
			}
		}

		if config.Cln != nil {
			node.Client, err = cln.NewClnClient(config.Cln.SocketPath)
			if err != nil {
				return nil, err
			}
		}

		// Make sure the nodes is available and set name and pubkey if not set
		// in config.
		info, err := node.Client.GetInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to get info from host %s", node.NodeConfig.Host)
		}

		if node.NodeConfig.Name == "" {
			node.NodeConfig.Name = info.Alias
		}

		if node.NodeConfig.NodePubkey == "" {
			node.NodeConfig.NodePubkey = info.Pubkey
		}

		for _, token := range config.Tokens {
			_, exists := nodes[token]
			if exists {
				return nil, fmt.Errorf("cannot have multiple nodes with the same token")
			}

			nodes[token] = node
		}
	}

	return &nodesService{
		nodes: nodes,
	}, nil
}

var ErrNodeNotFound = errors.New("node not found")

func (s *nodesService) GetNode(token string) (*Node, error) {
	node, ok := s.nodes[token]
	if !ok {
		return nil, ErrNodeNotFound
	}

	return node, nil
}
