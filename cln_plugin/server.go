package cln_plugin

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/breez/lspd/cln_plugin/proto"
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
	listenAddress     string
	subscriberTimeout time.Duration
	grpcServer        *grpc.Server
	mtx               sync.Mutex
	stream            proto.ClnPlugin_HtlcStreamServer
	newSubscriber     chan struct{}
	started           chan struct{}
	done              chan struct{}
	startError        chan error
	sendQueue         chan *htlcAcceptedMsg
	recvQueue         chan *htlcResultMsg
}

// Creates a new grpc server
func NewServer(listenAddress string, subscriberTimeout time.Duration) *server {
	// TODO: Set a sane max queue size
	return &server{
		listenAddress:     listenAddress,
		subscriberTimeout: subscriberTimeout,
		// The send queue exists to buffer messages until a subscriber is active.
		sendQueue: make(chan *htlcAcceptedMsg, 10000),
		// The receive queue exists mainly to allow returning timeouts to the
		// cln plugin. If there is no subscriber active within the subscriber
		// timeout period these results can be put directly on the receive queue.
		recvQueue:  make(chan *htlcResultMsg, 10000),
		started:    make(chan struct{}),
		startError: make(chan error, 1),
	}
}

// Starts the grpc server. Blocks until the servver is stopped. WaitStarted can
// be called to ensure the server is started without errors if this function
// is run as a goroutine.
func (s *server) Start() error {
	s.mtx.Lock()
	if s.grpcServer != nil {
		s.mtx.Unlock()
		return nil
	}

	lis, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		log.Printf("ERROR Server failed to listen: %v", err)
		s.startError <- err
		s.mtx.Unlock()
		return err
	}

	s.done = make(chan struct{})
	s.newSubscriber = make(chan struct{})
	s.grpcServer = grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(1) * time.Second,
			Timeout: time.Duration(10) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: time.Duration(1) * time.Second,
		}),
	)
	s.mtx.Unlock()
	proto.RegisterClnPluginServer(s.grpcServer, s)

	log.Printf("Server starting to listen on %s.", s.listenAddress)
	go s.listenHtlcRequests()
	go s.listenHtlcResponses()
	close(s.started)
	return s.grpcServer.Serve(lis)
}

// Waits until the server has started, or errored during startup.
func (s *server) WaitStarted() error {
	select {
	case <-s.started:
		return nil
	case err := <-s.startError:
		return err
	}
}

// Stops all work from the grpc server immediately.
func (s *server) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	log.Printf("Server Stop() called.")
	if s.grpcServer == nil {
		return
	}

	s.grpcServer.Stop()
	s.grpcServer = nil

	close(s.done)
	log.Printf("Server stopped.")
}

// Grpc method that is called when a new client subscribes. There can only be
// one subscriber active at a time. If there is an error receiving or sending
// from or to the subscriber, the subscription is closed.
func (s *server) HtlcStream(stream proto.ClnPlugin_HtlcStreamServer) error {
	s.mtx.Lock()
	if s.stream == nil {
		log.Printf("Got a new HTLC stream subscription request.")
	} else {
		s.mtx.Unlock()
		log.Printf("Got a HTLC stream subscription request, but subscription " +
			"was already active.")
		return fmt.Errorf("already subscribed")
	}

	s.stream = stream

	// Notify listeners that a new subscriber is active. Replace the chan with
	// a new one immediately in case this subscriber is dropped later.
	close(s.newSubscriber)
	s.newSubscriber = make(chan struct{})
	s.mtx.Unlock()

	<-stream.Context().Done()
	log.Printf("HtlcStream context is done. Return: %v", stream.Context().Err())

	// Remove the subscriber.
	s.mtx.Lock()
	s.stream = nil
	s.mtx.Unlock()

	return stream.Context().Err()
}

// Enqueues a htlc_accepted message for send to the grpc client.
func (s *server) Send(id string, h *HtlcAccepted) {
	s.sendQueue <- &htlcAcceptedMsg{
		id:      id,
		htlc:    h,
		timeout: time.Now().Add(s.subscriberTimeout),
	}
}

// Receives the next htlc resolution message from the grpc client. Returns id
// and message. Blocks until a message is available. Returns a nil message if
// the server is done. This function effectively waits until a subscriber is
// active and has sent a message.
func (s *server) Receive() (string, interface{}) {
	select {
	case <-s.done:
		return "", nil
	case msg := <-s.recvQueue:
		return msg.id, msg.result
	}
}

// Listens to sendQueue for htlc_accepted requests from cln. The message will be
// held until a subscriber is active, or the subscriber timeout expires. The
// messages are sent to the grpc client in fifo order.
func (s *server) listenHtlcRequests() {
	for {
		select {
		case <-s.done:
			log.Printf("listenHtlcRequests received done. Stop listening.")
			return
		case msg := <-s.sendQueue:
			s.handleHtlcAccepted(msg)
		}
	}
}

// Attempts to send a htlc_accepted message to the grpc client. The message will
// be held until a subscriber is active, or the subscriber timeout expires.
func (s *server) handleHtlcAccepted(msg *htlcAcceptedMsg) {
	for {
		s.mtx.Lock()
		stream := s.stream
		ns := s.newSubscriber
		s.mtx.Unlock()

		// If there is no active subscription, wait until there is a new
		// subscriber, or the message times out.
		if stream == nil {
			select {
			case <-s.done:
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
				s.recvQueue <- &htlcResultMsg{
					id:     msg.id,
					result: s.defaultResult(),
				}

				return
			}
		}

		// There is a subscriber. Attempt to send the htlc_accepted message.
		err := stream.Send(&proto.HtlcAccepted{
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

		// If there is no error, we're done.
		if err == nil {
			return
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
		case <-s.done:
			log.Printf("listenHtlcResponses received done. Stopping listening.")
			return
		default:
			resp := s.recv()
			s.recvQueue <- &htlcResultMsg{
				id:     resp.Correlationid,
				result: s.mapResult(resp.Outcome),
			}
		}
	}
}

// Helper function that blocks until a message from a grpc client is received
// or the server stops. Either returns a received message, or nil if the server
// has stopped.
func (s *server) recv() *proto.HtlcResolution {
	for {
		// make a copy of the used fields, to make sure state updates don't
		// surprise us. The newSubscriber chan is swapped whenever a new
		// subscriber arrives.
		s.mtx.Lock()
		stream := s.stream
		ns := s.newSubscriber
		s.mtx.Unlock()

		if stream == nil {
			log.Printf("Got no subscribers for receive. Waiting for subscriber.")
			select {
			case <-s.done:
				log.Printf("Done signalled, stopping receive.")
				return nil
			case <-ns:
				log.Printf("New subscription available for receive, continue receive.")
				continue
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
func (s *server) mapResult(outcome interface{}) interface{} {
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
