package lsps0

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0/codes"
	"github.com/breez/lspd/lsps0/jsonrpc"
	"github.com/breez/lspd/lsps0/status"
	"golang.org/x/exp/slices"
)

var ErrAlreadyServing = errors.New("lsps0: already serving")
var ErrServerStopped = errors.New("lsps0: the server has been stopped")
var Lsps0MessageType uint32 = 37913
var BadMessageFormatError string = "bad message format"
var InternalError string = "internal error"
var MaxSimultaneousRequests = 25

// ServiceDesc and is constructed from it for internal purposes.
type serviceInfo struct {
	// Contains the implementation for the methods in this service.
	serviceImpl interface{}
	methods     map[string]*MethodDesc
}

type methodInfo struct {
	service *serviceInfo
	method  *MethodDesc
}

type Server struct {
	mu       sync.Mutex
	serve    bool
	services map[string]*serviceInfo
	methods  map[string]*methodInfo
}

func NewServer() *Server {
	return &Server{
		services: make(map[string]*serviceInfo),
		methods:  make(map[string]*methodInfo),
	}
}

func (s *Server) Serve(lis lightning.CustomMsgClient) error {
	s.mu.Lock()
	if s.serve {
		return ErrAlreadyServing
	}
	s.serve = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.serve = false
		s.mu.Unlock()
	}()

	guard := make(chan struct{}, MaxSimultaneousRequests)
	for {
		msg, err := lis.Recv()
		if err != nil {
			if err == context.Canceled {
				log.Printf("lsps0: lis got canceled, stopping.")
				return err
			}

			log.Printf("lsps0 Serve(): Recv() err != nil: %v", err)
			<-time.After(time.Second)
			continue
		}

		// Ignore any message that is not an lsps0 message
		if msg.Type != Lsps0MessageType {
			continue
		}

		// Make sure there are no 0 bytes
		if slices.Contains(msg.Data, 0x00) {
			log.Printf("UNUSUAL: Got custom message containing 0 bytes from peer '%s'.", msg.PeerId)
			go sendError(lis, msg, nil, status.New(codes.ParseError, BadMessageFormatError))
			continue
		}

		req := new(jsonrpc.Request)
		err = json.Unmarshal(msg.Data, req)
		if err != nil {
			log.Printf("UNUSUAL: Failed to unmarshal custom message from peer '%s': %v", msg.PeerId, err)
			go sendError(lis, msg, nil, status.New(codes.ParseError, BadMessageFormatError))
			continue
		}

		if req == nil {
			log.Printf("UNUSUAL: req == nil after unmarshal custom message from peer '%s': %v", msg.PeerId, err)
			go sendError(lis, msg, nil, status.New(codes.ParseError, BadMessageFormatError))
			continue
		}

		if req.JsonRpc != jsonrpc.Version {
			log.Printf("UNUSUAL: jsonrpc version is '%s' in custom message from peer '%s': %v", req.JsonRpc, msg.PeerId, err)
			go sendError(lis, msg, req, status.Newf(codes.InvalidRequest, "Expected jsonrpc %s, found %s", jsonrpc.Version, req.JsonRpc))
			continue
		}

		m, ok := s.methods[req.Method]
		if !ok {
			log.Printf("UNUSUAL: peer '%s' requested method '%s', but it does not exist.", msg.PeerId, req.Method)
			go sendError(lis, msg, req, status.New(codes.MethodNotFound, "method not found"))
			continue
		}

		// Deserialization step of the request params. This function is called
		// by method handlers of service implementations to deserialize the
		// typed request object.
		df := func(v interface{}) error {
			if err := json.Unmarshal(req.Params, v); err != nil {
				return status.Newf(codes.InvalidParams, "invalid params").Err()
			}

			return nil
		}

		// Will block if the guard queue is already filled to ensure
		// MaxSimultaneousRequests is not exceeded.
		guard <- struct{}{}

		// NOTE: The handler is being called asynchonously. This may cause the
		// order of messages handled to be different from the order in which
		// they were received.
		go func() {
			// Releases a queued item in the guard, to release a spot for
			// another simultaneous request.
			defer func() { <-guard }()

			// Call the method handler for the requested method.
		r, err := m.method.Handler(m.service.serviceImpl, context.TODO(), df)
		if err != nil {
			s, ok := status.FromError(err)
			if !ok {
				log.Printf("Internal error when processing custom message '%s' from peer '%s': %v", string(msg.Data), msg.PeerId, err)
				s = status.New(codes.InternalError, InternalError)
			}

				sendError(lis, msg, req, s)
				return
		}

			sendResponse(lis, msg, req, r)
		}()
	}
}

func sendResponse(
	lis lightning.CustomMsgClient,
	in *lightning.CustomMessage,
	req *jsonrpc.Request,
	params interface{},
) {
	rd, err := json.Marshal(params)
	if err != nil {
		log.Printf("Failed to mashal response params '%+v'", params)
		sendError(lis, in, req, status.New(codes.InternalError, InternalError))
		return
	}

	resp := &jsonrpc.Response{
		JsonRpc: jsonrpc.Version,
		Id:      req.Id,
		Result:  rd,
	}
	res, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to mashal response '%+v'", resp)
		sendError(lis, in, req, status.New(codes.InternalError, InternalError))
		return
	}

	msg := &lightning.CustomMessage{
		PeerId: in.PeerId,
		Type:   Lsps0MessageType,
		Data:   res,
	}

	err = lis.Send(msg)
	if err != nil {
		log.Printf("Failed to send response message '%s' to request '%s' to peer '%s': %v", string(msg.Data), string(in.Data), msg.PeerId, err)
		return
	}
}

func sendError(
	lis lightning.CustomMsgClient,
	in *lightning.CustomMessage,
	req *jsonrpc.Request,
	status *status.Status,
) {
	var id *string
	if req != nil && req.Id != "" {
		id = &req.Id
	}
	resp := &jsonrpc.Error{
		JsonRpc: jsonrpc.Version,
		Id:      id,
		Error: jsonrpc.ErrorBody{
			Code:    int32(status.Code),
			Message: status.Message,
			Data:    nil,
		},
	}

	res, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to mashal response '%+v'", resp)
		return
	}

	msg := &lightning.CustomMessage{
		PeerId: in.PeerId,
		Type:   Lsps0MessageType,
		Data:   res,
	}

	err = lis.Send(msg)
	if err != nil {
		log.Printf("Failed to send message '%s' to peer '%s': %v", string(msg.Data), msg.PeerId, err)
		return
	}
}

func (s *Server) RegisterService(desc *ServiceDesc, impl interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("RegisterService(%q)", desc.ServiceName)
	if s.serve {
		log.Fatalf("lsps0: Server.RegisterService after Server.Serve for %q", desc.ServiceName)
	}
	if _, ok := s.services[desc.ServiceName]; ok {
		log.Fatalf("lsps0: Server.RegisterService found duplicate service registration for %q", desc.ServiceName)
	}
	info := &serviceInfo{
		serviceImpl: impl,
		methods:     make(map[string]*MethodDesc),
	}
	for i := range desc.Methods {
		d := &desc.Methods[i]
		if _, ok := s.methods[d.MethodName]; ok {
			log.Fatalf("lsps0: Server.RegisterService found duplicate method registration for %q", d.MethodName)
		}
		info.methods[d.MethodName] = d
		s.methods[d.MethodName] = &methodInfo{
			service: info,
			method:  d,
		}
	}
	s.services[desc.ServiceName] = info
}

type ServiceDesc struct {
	ServiceName string
	// The pointer to the service interface. Used to check whether the user
	// provided implementation satisfies the interface requirements.
	HandlerType interface{}
	Methods     []MethodDesc
}

type MethodDesc struct {
	MethodName string
	Handler    methodHandler
}

type methodHandler func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)

// ServiceRegistrar wraps a single method that supports service registration. It
// enables users to pass concrete types other than grpc.Server to the service
// registration methods exported by the IDL generated code.
type ServiceRegistrar interface {
	// RegisterService registers a service and its implementation to the
	// concrete type implementing this interface.  It may not be called
	// once the server has started serving.
	// desc describes the service and its methods and handlers. impl is the
	// service implementation which is passed to the method handlers.
	RegisterService(desc *ServiceDesc, impl interface{})
}
