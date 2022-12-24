package cln_plugin

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	grpc "google.golang.org/grpc"
)

var receiveWaitDelay = time.Millisecond * 200

type subscription struct {
	stream ClnPlugin_HtlcStreamServer
	done   chan struct{}
}
type server struct {
	ClnPluginServer
	listenAddress string
	grpcServer    *grpc.Server
	startMtx      sync.Mutex
	corrMtx       sync.Mutex
	subscription  *subscription
	newSubscriber chan struct{}
	done          chan struct{}
	correlations  map[uint64]chan *HtlcResolution
	index         uint64
}

func NewServer(listenAddress string) *server {
	return &server{
		listenAddress: listenAddress,
		newSubscriber: make(chan struct{}, 1),
		correlations:  make(map[uint64]chan *HtlcResolution),
		index:         0,
	}
}

func (s *server) Start() error {
	s.startMtx.Lock()
	if s.grpcServer != nil {
		s.startMtx.Unlock()
		return nil
	}

	lis, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		log.Printf("ERROR Server failed to listen: %v", err)
		s.startMtx.Unlock()
		return err
	}

	s.done = make(chan struct{})
	s.grpcServer = grpc.NewServer()
	s.startMtx.Unlock()
	RegisterClnPluginServer(s.grpcServer, s)

	log.Printf("Server starting to listen on %s.", s.listenAddress)
	go s.listenHtlcResponses()
	return s.grpcServer.Serve(lis)
}

func (s *server) Stop() {
	s.startMtx.Lock()
	defer s.startMtx.Unlock()
	log.Printf("Server Stop() called.")
	if s.grpcServer == nil {
		return
	}

	close(s.done)
	s.grpcServer.Stop()
	s.grpcServer = nil
}

func (s *server) HtlcStream(stream ClnPlugin_HtlcStreamServer) error {
	log.Printf("Got HTLC stream subscription request.")
	s.startMtx.Lock()
	if s.subscription != nil {
		s.startMtx.Unlock()
		return fmt.Errorf("already subscribed")
	}

	sb := &subscription{
		stream: stream,
		done:   make(chan struct{}),
	}
	s.subscription = sb
	s.newSubscriber <- struct{}{}
	s.startMtx.Unlock()

	defer func() {
		s.startMtx.Lock()
		s.subscription = nil
		close(sb.done)
		s.startMtx.Unlock()
	}()

	go func() {
		<-stream.Context().Done()
		log.Printf("HtlcStream context is done. Removing subscriber: %v", stream.Context().Err())
		s.startMtx.Lock()
		s.subscription = nil
		close(sb.done)
		s.startMtx.Unlock()
	}()

	select {
	case <-s.done:
		log.Printf("HTLC server signalled done. Return EOF.")
		return io.EOF
	case <-sb.done:
		log.Printf("HTLC stream signalled done. Return EOF.")
		return io.EOF
	}
}

func (s *server) Send(h *HtlcAccepted) *HtlcResolution {
	sb := s.subscription
	if sb == nil {
		log.Printf("No subscribers available. Ignoring HtlcAccepted %+v", h)
		return s.defaultResolution()
	}

	c := make(chan *HtlcResolution)
	s.corrMtx.Lock()
	s.index++
	index := s.index
	s.correlations[index] = c
	s.corrMtx.Unlock()

	h.Correlationid = index

	defer func() {
		s.corrMtx.Lock()
		delete(s.correlations, index)
		s.corrMtx.Unlock()
		close(c)
	}()

	log.Printf("Sending HtlcAccepted: %+v", h)
	err := sb.stream.Send(h)
	if err != nil {
		// TODO: Close the connection? Reset the subscriber?
		log.Printf("Send() errored, Correlationid: %d: %v", index, err)
		return s.defaultResolution()
	}

	select {
	case <-s.done:
		log.Printf("Signalled done while waiting for htlc resolution, Correlationid: %d, Ignoring: %+v", index, h)
		return s.defaultResolution()
	case resolution := <-c:
		log.Printf("Got resolution, Correlationid: %d: %+v", index, h)
		return resolution
	}
}

func (s *server) recv() *HtlcResolution {
	for {
		sb := s.subscription
		if sb == nil {
			log.Printf("Got no subscribers for receive. Waiting for subscriber.")
			select {
			case <-s.done:
				log.Printf("Done signalled, stopping receive.")
				return s.defaultResolution()
			case <-s.newSubscriber:
				log.Printf("New subscription available for receive, continue receive.")
				continue
			}
		}

		r, err := sb.stream.Recv()
		if err == nil {
			log.Printf("Received HtlcResolution %+v", r)
			return r
		}

		// TODO: close the subscription??
		log.Printf("Recv() errored, waiting %v: %v", receiveWaitDelay, err)
		select {
		case <-s.done:
			log.Printf("Done signalled, stopping receive.")
			return s.defaultResolution()
		case <-time.After(receiveWaitDelay):
		}
	}
}

func (s *server) listenHtlcResponses() {
	for {
		select {
		case <-s.done:
			log.Printf("listenHtlcResponses received done. Stopping listening.")
			return
		default:
			response := s.recv()
			s.corrMtx.Lock()
			correlation, ok := s.correlations[response.Correlationid]
			s.corrMtx.Unlock()
			if ok {
				correlation <- response
			} else {
				log.Printf("Got HTLC resolution that could not be correlated: %+v", response)
			}
		}
	}
}

func (s *server) defaultResolution() *HtlcResolution {
	return &HtlcResolution{
		Outcome: &HtlcResolution_Continue{
			Continue: &HtlcContinue{},
		},
	}
}
