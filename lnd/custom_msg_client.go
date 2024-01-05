package lnd

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CustomMsgClient struct {
	lightning.CustomMsgClient
	client        *LndClient
	initWg        sync.WaitGroup
	stopRequested bool
	ctx           context.Context
	cancel        context.CancelFunc
	recvQueue     chan *lightning.CustomMessage
}

func NewCustomMsgClient(conf *config.ClnConfig, client *LndClient) *CustomMsgClient {
	c := &CustomMsgClient{
		client:    client,
		recvQueue: make(chan *lightning.CustomMessage, 10000),
	}

	c.initWg.Add(1)
	return c
}

func (c *CustomMsgClient) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	c.cancel = cancel
	c.stopRequested = false
	return c.listen()
}

func (i *CustomMsgClient) WaitStarted() {
	i.initWg.Wait()
}

func (i *CustomMsgClient) listen() error {
	inited := false

	defer func() {
		if !inited {
			i.initWg.Done()
		}
		log.Printf("CLN custom msg listen(): stopping.")
	}()

	for {
		if i.ctx.Err() != nil {
			return i.ctx.Err()
		}

		log.Printf("Connecting LND custom msg stream.")
		msgClient, err := i.client.client.SubscribeCustomMessages(
			i.ctx,
			&lnrpc.SubscribeCustomMessagesRequest{},
		)
		if err != nil {
			log.Printf("client.SubscribeCustomMessages(): %v", err)
			<-time.After(time.Second)
			continue
		}

		for {
			if i.ctx.Err() != nil {
				return i.ctx.Err()
			}

			if !inited {
				inited = true
				i.initWg.Done()
			}

			// Stop receiving if stop if requested.
			if i.stopRequested {
				return nil
			}

			request, err := msgClient.Recv()
			if err != nil {
				// If it is just the error result of the context cancellation
				// the we exit silently.
				status, ok := status.FromError(err)
				if ok && status.Code() == codes.Canceled {
					log.Printf("Got code canceled. Break.")
					break
				}

				// Otherwise it an unexpected error, we log.
				log.Printf("unexpected error in interceptor.Recv() %v", err)
				break
			}

			i.recvQueue <- &lightning.CustomMessage{
				PeerId: hex.EncodeToString(request.Peer),
				Type:   request.Type,
				Data:   request.Data,
			}
		}

		<-time.After(time.Second)
	}
}

func (c *CustomMsgClient) Recv() (*lightning.CustomMessage, error) {
	select {
	case msg := <-c.recvQueue:
		return msg, nil
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func (c *CustomMsgClient) Send(msg *lightning.CustomMessage) error {
	peerId, err := hex.DecodeString(msg.PeerId)
	if err != nil {
		return fmt.Errorf("hex.DecodeString(%s) err: %w", msg.PeerId, err)
	}
	_, err = c.client.client.SendCustomMessage(
		c.ctx,
		&lnrpc.SendCustomMessageRequest{
			Peer: peerId,
			Type: msg.Type,
			Data: msg.Data,
		},
	)
	return err
}

func (i *CustomMsgClient) Stop() error {
	// Setting stopRequested to true will make the interceptor stop receiving.
	i.stopRequested = true

	// Close the grpc connection.
	i.cancel()
	return nil
}
