package cln

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/cln_plugin/proto"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

type CustomMsgClient struct {
	lightning.CustomMsgClient
	pluginAddress string
	client        *ClnClient
	pluginClient  proto.ClnPluginClient
	initWg        sync.WaitGroup
	stopRequested bool
	ctx           context.Context
	cancel        context.CancelFunc
	recvQueue     chan *lightning.CustomMessage
}

func NewCustomMsgClient(conf *config.ClnConfig, client *ClnClient) *CustomMsgClient {
	c := &CustomMsgClient{
		pluginAddress: conf.PluginAddress,
		client:        client,
		recvQueue:     make(chan *lightning.CustomMessage, 10000),
	}

	c.initWg.Add(1)
	return c
}

func (c *CustomMsgClient) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	log.Printf("Dialing cln plugin on '%s'", c.pluginAddress)
	conn, err := grpc.DialContext(
		ctx,
		c.pluginAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    time.Duration(10) * time.Second,
			Timeout: time.Duration(10) * time.Second,
		}),
	)
	if err != nil {
		log.Printf("grpc.Dial error: %v", err)
		cancel()
		return err
	}

	c.pluginClient = proto.NewClnPluginClient(conn)
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

		log.Printf("Connecting CLN msg stream.")
		msgClient, err := i.pluginClient.CustomMsgStream(i.ctx, &proto.CustomMessageRequest{})
		if err != nil {
			log.Printf("pluginClient.CustomMsgStream(): %v", err)
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

			payload, err := hex.DecodeString(request.Payload)
			if err != nil {
				log.Printf("Error hex decoding cln custom msg payload from peer '%s': %v", request.PeerId, err)
				continue
			}

			if len(payload) < 3 {
				log.Printf("UNUSUAL: Custom msg payload from peer '%s' is too small", request.PeerId)
				continue
			}

			t := binary.BigEndian.Uint16(payload)
			payload = payload[2:]
			i.recvQueue <- &lightning.CustomMessage{
				PeerId: request.PeerId,
				Type:   uint32(t),
				Data:   payload,
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
	var t [2]byte
	binary.BigEndian.PutUint16(t[:], uint16(msg.Type))

	m := hex.EncodeToString(t[:]) + hex.EncodeToString(msg.Data)
	client, err := c.client.getClient()
	if err != nil {
		return err
	}
	_, err = client.SendCustomMessage(msg.PeerId, m)
	return err
}

func (i *CustomMsgClient) Stop() error {
	// Setting stopRequested to true will make the interceptor stop receiving.
	i.stopRequested = true

	// Close the grpc connection.
	i.cancel()
	return nil
}
