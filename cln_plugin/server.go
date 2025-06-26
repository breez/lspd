package cln_plugin

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/breez/lspd/cln_plugin/proto"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"golang.org/x/sys/unix"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// Internal htlc_accepted message meant for the sendQueue.
type htlcAcceptedMsg struct {
	id      string
	htlc    *HtlcAccepted
	timeout time.Time
}

// Internal htlc result message meant for the recvQueue.
type htlcResultMsg struct {
	id     string
	result interface{}
}

type server struct {
	proto.ClnPluginServer
	ctx               context.Context
	cancel            context.CancelFunc
	subscriberTimeout time.Duration
	grpcServer        *grpc.Server
	mtx               sync.Mutex
	started           chan struct{}
	startError        chan error
	htlcnewSubscriber chan struct{}
	htlcStream        proto.ClnPlugin_HtlcStreamServer
	htlcSendQueue     chan *htlcAcceptedMsg
	htlcRecvQueue     chan *htlcResultMsg
	inflightHtlcs     *orderedmap.OrderedMap[string, *htlcAcceptedMsg]
}

// Creates a new grpc server
func NewServer() *server {
	// TODO: Set a sane max queue size
	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(1) * time.Second,
			Timeout: time.Duration(10) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: time.Duration(1) * time.Second,
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	return &server{
		ctx:    ctx,
		cancel: cancel,
		// The send queue exists to buffer messages until a subscriber is active.
		htlcSendQueue: make(chan *htlcAcceptedMsg, 10000),
		// The receive queue exists mainly to allow returning timeouts to the
		// cln plugin. If there is no subscriber active within the subscriber
		// timeout period these results can be put directly on the receive queue.
		htlcRecvQueue:     make(chan *htlcResultMsg, 10000),
		inflightHtlcs:     orderedmap.New[string, *htlcAcceptedMsg](),
		started:           make(chan struct{}),
		startError:        make(chan error, 1),
		grpcServer:        grpcServer,
		htlcnewSubscriber: make(chan struct{}),
	}
}

// Starts the grpc server. Blocks until the server is stopped. WaitStarted can
// be called to ensure the server is started without errors if this function
// is run as a goroutine. This function can only be called once.
func (s *server) Start(address string, subscriberTimeout time.Duration) error {
	lis, err := s.initialize(address, subscriberTimeout)
	if err != nil {
		s.startError <- err
		return err
	}
	go func() {
		<-s.ctx.Done()
		s.grpcServer.Stop()
	}()
	close(s.started)
	err = s.grpcServer.Serve(lis)
	return err
}

func (s *server) initialize(address string, subscriberTimeout time.Duration) (net.Listener, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.ctx.Err() != nil {
		return nil, s.ctx.Err()
	}

	s.subscriberTimeout = subscriberTimeout

	go s.logHtlcStates()
	go s.listenHtlcRequests()
	go s.listenHtlcResponses()
	log.Printf("Server starting to listen on %s.", address)
	config := &net.ListenConfig{Control: reusePort}
	lis, err := config.Listen(s.ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("grpc server failed to listen on address '%s': %w",
			address, err)
	}

	proto.RegisterClnPluginServer(s.grpcServer, s)
	return lis, nil
}

func reusePort(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		syscall.SetsockoptInt(int(descriptor), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})
}

// Waits until the server has started, or errored during startup.
func (s *server) WaitStarted() error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.started:
		return nil
	case err := <-s.startError:
		return err
	}
}

// Stops all work from the grpc server immediately.
func (s *server) Stop() {
	log.Printf("Server Stop() called.")
	s.cancel()
}

// Grpc method that is called when a new client subscribes. There can only be
// one subscriber active at a time. If there is an error receiving or sending
// from or to the subscriber, the subscription is closed.
func (s *server) HtlcStream(stream proto.ClnPlugin_HtlcStreamServer) error {
	err := s.newHtlcStream(stream)
	if err != nil {
		return err
	}

	<-stream.Context().Done()
	log.Printf("HtlcStream context is done. Return: %v", stream.Context().Err())

	// Remove the subscriber.
	s.mtx.Lock()
	s.htlcStream = nil
	s.mtx.Unlock()

	return stream.Context().Err()
}

func (s *server) newHtlcStream(stream proto.ClnPlugin_HtlcStreamServer) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.htlcStream == nil {
		log.Printf("Got a new HTLC stream subscription request.")
	} else {
		log.Printf("Got a HTLC stream subscription request, but subscription " +
			"was already active.")
		return fmt.Errorf("already subscribed")
	}

	newTimeout := time.Now().Add(s.subscriberTimeout)
	// Replay in-flight htlcs in fifo order
	for pair := s.inflightHtlcs.Oldest(); pair != nil; pair = pair.Next() {
		err := sendHtlcAccepted(stream, pair.Value)
		if err != nil {
			return err
		}

		// Reset the subscriber timeout for this htlc.
		pair.Value.timeout = newTimeout
	}

	s.htlcStream = stream

	// Notify listeners that a new subscriber is active. Replace the chan with
	// a new one immediately in case this subscriber is dropped later.
	close(s.htlcnewSubscriber)
	s.htlcnewSubscriber = make(chan struct{})
	return nil
}

// Enqueues a htlc_accepted message for send to the grpc client.
func (s *server) SendHtlcAccepted(id string, h *HtlcAccepted) {
	select {
	case <-s.ctx.Done():
	case s.htlcSendQueue <- &htlcAcceptedMsg{
		id:      id,
		htlc:    h,
		timeout: time.Now().Add(s.subscriberTimeout),
	}:
	}
}

// Receives the next htlc resolution message from the grpc client. Returns id
// and message. Blocks until a message is available. Returns a nil message if
// the server is done. This function effectively waits until a subscriber is
// active and has sent a message.
func (s *server) ReceiveHtlcResolution() (string, interface{}) {
	select {
	case <-s.ctx.Done():
		return "", nil
	case msg := <-s.htlcRecvQueue:
		s.mtx.Lock()
		s.inflightHtlcs.Delete(msg.id)
		s.mtx.Unlock()
		return msg.id, msg.result
	}
}

func (s *server) logHtlcStates() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Minute * 5):
		}

		s.mtx.Lock()
		sendQueueLen := len(s.htlcSendQueue)
		recvQueueLen := len(s.htlcRecvQueue)
		inflightLen := s.inflightHtlcs.Len()
		s.mtx.Unlock()

		log.Printf(
			"Htlc status: inflight: %d, send queue: %d, receive queue: %d",
			inflightLen, sendQueueLen, recvQueueLen)
	}
}

// Listens to sendQueue for htlc_accepted requests from cln. The message will be
// held until a subscriber is active, or the subscriber timeout expires. The
// messages are sent to the grpc client in fifo order.
func (s *server) listenHtlcRequests() {
	for {
		select {
		case <-s.ctx.Done():
			log.Printf("listenHtlcRequests received done. Stop listening.")
			return
		case msg := <-s.htlcSendQueue:
			s.handleHtlcAccepted(msg)
		}
	}
}

// Attempts to send a htlc_accepted message to the grpc client. The message will
// be held until a subscriber is active, or the subscriber timeout expires.
func (s *server) handleHtlcAccepted(msg *htlcAcceptedMsg) {
	for {
		s.mtx.Lock()
		stream := s.htlcStream
		ns := s.htlcnewSubscriber
		s.mtx.Unlock()

		// If there is no active subscription, wait until there is a new
		// subscriber, or the message times out.
		if stream == nil {
			select {
			case <-s.ctx.Done():
				log.Printf("handleHtlcAccepted received server done. Stop processing.")
				return
			case <-ns:
				log.Printf("got a new subscriber. continue handleHtlcAccepted.")
				continue
			case <-time.After(time.Until(msg.timeout)):
				log.Printf(
					"WARNING: htlc with id '%s' timed out after '%v' waiting "+
						"for grpc subscriber: %+v",
					msg.id,
					s.subscriberTimeout,
					msg.htlc,
				)

				// If the subscriber timeout expires while holding the htlc
				// we short circuit the htlc by sending the default result
				// (continue) to cln.
				s.htlcRecvQueue <- &htlcResultMsg{
					id:     msg.id,
					result: s.defaultResult(),
				}

				return
			}
		}

		// Add the htlc to in-flight htlcs
		s.mtx.Lock()
		s.inflightHtlcs.Set(msg.id, msg)

		// There is a subscriber. Attempt to send the htlc_accepted message.
		err := sendHtlcAccepted(stream, msg)

		// If there is no error, we're done.
		if err == nil {
			s.mtx.Unlock()
			return
		} else {
			// Remove the htlc from inflight htlcs again on error, so it won't
			// get replayed twice in a row.
			s.inflightHtlcs.Delete(msg.id)
			s.mtx.Unlock()
		}

		// If we end up here, there was an error sending the message to the
		// grpc client.
		// TODO: If the Send errors, but the context is not done, this will
		// currently retry immediately. Check whether the context is really
		// done on an error!
		log.Printf("Error sending htlc_accepted message to subscriber. Retrying: %v", err)
	}
}

// Listens to htlc responses from the grpc client and appends them to the
// receive queue. The messages from the receive queue are read in the Receive
// function.
func (s *server) listenHtlcResponses() {
	for {
		select {
		case <-s.ctx.Done():
			log.Printf("listenHtlcResponses received done. Stopping listening.")
			return
		default:
			resp := s.recvHtlcResolution()
			if resp == nil {
				continue
			}

			s.htlcRecvQueue <- &htlcResultMsg{
				id:     resp.Correlationid,
				result: s.mapHtlcResult(resp.Outcome),
			}
		}
	}
}

// Helper function that blocks until a message from a grpc client is received
// or the server stops. Either returns a received message, or nil if the server
// has stopped.
func (s *server) recvHtlcResolution() *proto.HtlcResolution {
	for {
		if s.ctx.Err() != nil {
			return nil
		}

		// make a copy of the used fields, to make sure state updates don't
		// surprise us. The newSubscriber chan is swapped whenever a new
		// subscriber arrives.
		s.mtx.Lock()
		stream := s.htlcStream
		ns := s.htlcnewSubscriber
		oldestHtlc := s.inflightHtlcs.Oldest()
		var htlcTimeout time.Duration = 1 << 62 // practically infinite
		if oldestHtlc != nil {
			htlcTimeout = time.Until(oldestHtlc.Value.timeout)
		}
		s.mtx.Unlock()

		if stream == nil {
			log.Printf("Got no subscribers for htlc receive. Waiting for subscriber.")
			select {
			case <-s.ctx.Done():
				log.Printf("Done signalled, stopping htlc receive.")
				return nil
			case <-ns:
				log.Printf("New subscription available for htlc receive, continue receive.")
				continue
			case <-time.After(htlcTimeout):
				log.Printf(
					"WARNING: htlc with id '%s' timed out after '%v' waiting "+
						"for grpc subscriber: %+v",
					oldestHtlc.Value.id,
					s.subscriberTimeout,
					oldestHtlc.Value.htlc,
				)

				// If the subscriber timeout expires while holding a htlc
				// we short circuit the htlc by sending the default result
				// (continue) to cln.
				return &proto.HtlcResolution{
					Correlationid: oldestHtlc.Value.id,
					Outcome: &proto.HtlcResolution_Continue{
						Continue: &proto.HtlcContinue{},
					},
				}
			}
		}

		// There is a subscription active. Attempt to receive a message.
		r, err := stream.Recv()
		if err == nil {
			log.Printf("Received HtlcResolution %+v", r)
			return r
		}

		// Receiving the message failed, so the subscription is broken. Remove
		// it if it hasn't been updated already. We'll try receiving again in
		// the next iteration of the for loop.
		// TODO: If the Recv errors, but the context is not done, this will
		// currently retry immediately. Check whether the context is really
		// done on an error!
		log.Printf("Recv() errored, Retrying: %v", err)
	}
}

// Maps a grpc result to the corresponding result for cln. The cln message
// is a raw json message, so it's easiest to use a map directly.
func (s *server) mapHtlcResult(outcome interface{}) interface{} {
	// result: continue
	cont, ok := outcome.(*proto.HtlcResolution_Continue)
	if ok {
		result := map[string]interface{}{
			"result": "continue",
		}

		if cont.Continue.ForwardTo != nil {
			result["forward_to"] = *cont.Continue.ForwardTo
		}

		if cont.Continue.Payload != nil {
			result["payload"] = *cont.Continue.Payload
		}

		return result
	}

	// result: fail
	fail, ok := outcome.(*proto.HtlcResolution_Fail)
	if ok {
		result := map[string]interface{}{
			"result": "fail",
		}

		fm, ok := fail.Fail.Failure.(*proto.HtlcFail_FailureMessage)
		if ok {
			result["failure_message"] = fm.FailureMessage
		}

		fo, ok := fail.Fail.Failure.(*proto.HtlcFail_FailureOnion)
		if ok {
			result["failure_onion"] = fo.FailureOnion
		}

		return result
	}

	// result: resolve
	resolve, ok := outcome.(*proto.HtlcResolution_Resolve)
	if ok {
		result := map[string]interface{}{
			"result":      "resolve",
			"payment_key": resolve.Resolve.PaymentKey,
		}

		return result
	}

	// On an unknown result we haven't implemented all possible cases from the
	// grpc message. We don't understand what's going on, so we'll return
	// result: continue.
	log.Printf("Unexpected htlc resolution type %T: %+v", outcome, outcome)
	return s.defaultResult()
}

// Returns a result: continue message.
func (s *server) defaultResult() interface{} {
	return map[string]interface{}{
		"result": "continue",
	}
}

func sendHtlcAccepted(stream proto.ClnPlugin_HtlcStreamServer, msg *htlcAcceptedMsg) error {
	return stream.Send(&proto.HtlcAccepted{
		Correlationid: msg.id,
		Onion: &proto.Onion{
			Payload:           msg.htlc.Onion.Payload,
			ShortChannelId:    msg.htlc.Onion.ShortChannelId,
			ForwardMsat:       msg.htlc.Onion.ForwardMsat,
			OutgoingCltvValue: msg.htlc.Onion.OutgoingCltvValue,
			SharedSecret:      msg.htlc.Onion.SharedSecret,
			NextOnion:         msg.htlc.Onion.NextOnion,
		},
		Htlc: &proto.Htlc{
			ShortChannelId:     msg.htlc.Htlc.ShortChannelId,
			Id:                 msg.htlc.Htlc.Id,
			AmountMsat:         msg.htlc.Htlc.AmountMsat,
			CltvExpiry:         msg.htlc.Htlc.CltvExpiry,
			CltvExpiryRelative: msg.htlc.Htlc.CltvExpiryRelative,
			PaymentHash:        msg.htlc.Htlc.PaymentHash,
		},
		ForwardTo: msg.htlc.ForwardTo,
	})
}
