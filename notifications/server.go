package notifications

import (
	context "context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
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
	request *SubscribeNotificationsRequest,
) (*SubscribeNotificationsReply, error) {
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
