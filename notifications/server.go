package notifications

import (
	context "context"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/breez/lspd/lightning"
	lspdrpc "github.com/breez/lspd/rpc"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/protobuf/proto"
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
		log.Printf(
			"failed to register for notifications: ecies.Decrypt error: %v",
			err,
		)
		return nil, fmt.Errorf("ecies.Decrypt(%x) error: %w", in.Blob, err)
	}

	var request SubscribeNotificationsRequest
	err = proto.Unmarshal(data, &request)
	if err != nil {
		log.Printf(
			"failed to register for notifications on url %v: proto.Unmarshal(%x) error: %v",
			request.Url,
			data,
			err,
		)
		return nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", data, err)
	}

	pubkey, err := lightning.VerifyMessage([]byte(request.Url), request.Signature)
	if err != nil {
		log.Printf(
			"failed to register %x for notifications on url %s: lightning.VerifyMessage error: %v",
			pubkey.SerializeCompressed(),
			request.Url,
			err,
		)

		return nil, err
	}

	pubkeyStr := hex.EncodeToString(pubkey.SerializeCompressed())
	err = s.store.Register(ctx, pubkeyStr, request.Url)
	if err != nil {
		log.Printf(
			"failed to register %x for notifications on url %s: %v",
			pubkey.SerializeCompressed(),
			request.Url,
			err,
		)

		return nil, ErrInternal
	}

	log.Printf("%s was successfully registered for notifications on url %s", pubkeyStr, request.Url)
	return &SubscribeNotificationsReply{}, nil
}

func (s *server) UnsubscribeNotifications(
	ctx context.Context,
	in *EncryptedNotificationRequest,
) (*UnsubscribeNotificationsReply, error) {
	node, _, err := lspdrpc.GetNode(ctx)
	if err != nil {
		return nil, err
	}

	data, err := ecies.Decrypt(node.EciesPrivateKey, in.Blob)
	if err != nil {
		log.Printf(
			"failed to unsubscribe: ecies.Decrypt error: %v",
			err,
		)
		return nil, fmt.Errorf("ecies.Decrypt(%x) error: %w", in.Blob, err)
	}

	var request UnsubscribeNotificationsRequest
	err = proto.Unmarshal(data, &request)
	if err != nil {
		log.Printf(
			"failed to unsubscribe: proto.Unmarshal(%x) error: %v",
			data,
			err,
		)
		return nil, fmt.Errorf("proto.Unmarshal(%x) error: %w", data, err)
	}

	pubkey, err := lightning.VerifyMessage([]byte(request.Url), request.Signature)
	if err != nil {
		log.Printf(
			"failed to unsubscribe on url %s: lightning.VerifyMessage error: %v",
			request.Url,
			err,
		)

		return nil, err
	}

	err = s.store.Unsubscribe(ctx, hex.EncodeToString(pubkey.SerializeCompressed()), request.Url)

	if err != nil {
		log.Printf(
			"failed to unsubscribe %x on url %s: %v",
			pubkey.SerializeCompressed(),
			request.Url,
			err,
		)

		return nil, ErrInternal
	}

	return &UnsubscribeNotificationsReply{}, nil
}
