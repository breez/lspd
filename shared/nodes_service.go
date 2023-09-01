package shared

import (
	"errors"
	"fmt"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
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
	Tokens              []string
}

type NodesService interface {
	GetNode(token string) (*Node, error)
	GetNodes() []*Node
}
type nodesService struct {
	nodes      []*Node
	nodeLookup map[string]*Node
}

func NewNodesService(nodes []*Node) (NodesService, error) {
	nodeLookup := make(map[string]*Node)
	for _, node := range nodes {
		for _, token := range node.Tokens {
			_, exists := nodeLookup[token]
			if exists {
				return nil, fmt.Errorf("cannot have multiple nodes with the same token")
			}

			nodeLookup[token] = node
		}
	}

	return &nodesService{
		nodes:      nodes,
		nodeLookup: nodeLookup,
	}, nil
}

var ErrNodeNotFound = errors.New("node not found")

func (s *nodesService) GetNode(token string) (*Node, error) {
	node, ok := s.nodeLookup[token]
	if !ok {
		return nil, ErrNodeNotFound
	}

	return node, nil
}

func (s *nodesService) GetNodes() []*Node {
	return s.nodes
}
