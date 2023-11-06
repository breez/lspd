package notifications

import (
	context "context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
)

var ErrInvalidSignature = fmt.Errorf("invalid signature")
var ErrInternal = fmt.Errorf("internal error")

type server struct {
	store Store
	NotificationsServer
}

func NewNotificationsServer(store Store) NotificationsServer {
	return &server{
		store: store,
	}
}

func (s *server) SubscribeNotifications(
	ctx context.Context,
	in *EncryptedNotificationRequest,
) (*SubscribeNotificationsReply, error) {
	node, _, err := lspdrpc.GetNode(ctx)
	if err != nil {
		return nil, err
	}

	data, err := ecies.Decrypt(node.EciesPrivateKey, in.Blob)
	if err != nil {
		return nil, fmt.Errorf("ecies.Decrypt(%x) error: %w", in.Blob, err)
	}

	var request SubscribeNotificationsRequest
	err = proto.Unmarshal(data, &request)
	if err != nil {
		log.Printf("proto.Unmarshal(%x) error: %v", data, err)
		return nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", data, err)
	}

	first := sha256.Sum256([]byte(request.Url))
	second := sha256.Sum256(first[:])
	pubkey, wasCompressed, err := ecdsa.RecoverCompact(
		request.Signature,
		second[:],
	)
	if err != nil {
		return nil, ErrInvalidSignature
	}

	if !wasCompressed {
		return nil, ErrInvalidSignature
	}

	err = s.store.Register(ctx, hex.EncodeToString(pubkey.SerializeCompressed()), request.Url)
	if err != nil {
		log.Printf(
			"failed to register %x for notifications on url %s: %v",
			pubkey.SerializeCompressed(),
			request.Url,
			err,
		)

		return nil, ErrInternal
	}

	return &SubscribeNotificationsReply{}, nil
}
