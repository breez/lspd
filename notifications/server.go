package notifications

import (
	context "context"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/breez/lspd/lightning"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
)

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

	pubkey, err := lightning.VerifyMessage([]byte(request.Url), request.Signature)
	if err != nil {
		return nil, err
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
