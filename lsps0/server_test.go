package lsps0

import (
	"encoding/json"
	"testing"

	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0/jsonrpc"
	"github.com/stretchr/testify/assert"
)

type MockLightningImpl struct {
	lightning.CustomMsgClient
	req  chan *lightning.CustomMessage
	err  error
	resp chan *lightning.CustomMessage
}

func newMock(err error) *MockLightningImpl {
	return &MockLightningImpl{
		req:  make(chan *lightning.CustomMessage, 100),
		err:  err,
		resp: make(chan *lightning.CustomMessage, 100),
	}
}

func (m *MockLightningImpl) Recv() (*lightning.CustomMessage, error) {
	msg := <-m.req
	return msg, m.err
}

func (m *MockLightningImpl) Send(c *lightning.CustomMessage) error {
	m.resp <- c
	return nil
}

func TestSuccess(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {}
	  }`
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}

	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Response)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Equal(t, "example#3cad6a54d302edba4c9ade2f7ffac098", r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)

	result := new(ListProtocolsResponse)
	err = json.Unmarshal(r.Result, result)
	assert.NoError(t, err)

	assert.Equal(t, []uint32{1, 2}, result.Protocols)
}

func TestInvalidRequest(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {}
	  `
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}
	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Error)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Nil(t, r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)
	assert.Equal(t, r.Error.Code, int32(-32700))
	assert.Equal(t, r.Error.Message, "bad message format")
}

func TestInvalidJsonRpcVersion(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "1.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {}
	  }`
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}
	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Error)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Equal(t, "example#3cad6a54d302edba4c9ade2f7ffac098", *r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)
	assert.Equal(t, r.Error.Code, int32(-32600))
	assert.Equal(t, r.Error.Message, "Expected jsonrpc 2.0, found 1.0")
}

func TestInvalidJsonRpcVersionNoId(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "1.0",
		"params": {}
	  }`
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}
	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Error)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Nil(t, r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)
	assert.Equal(t, r.Error.Code, int32(-32600))
	assert.Equal(t, r.Error.Message, "Expected jsonrpc 2.0, found 1.0")
}

func TestUnknownMethod(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.unknown",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": {}
	  }`
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}
	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Error)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Equal(t, "example#3cad6a54d302edba4c9ade2f7ffac098", *r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)
	assert.Equal(t, r.Error.Code, int32(-32601))
	assert.Equal(t, r.Error.Message, "method not found")
}

func TestInvalidParams(t *testing.T) {
	srv := NewServer()
	pSrv := &protocolServer{
		protocols: []uint32{1, 2},
	}
	RegisterProtocolServer(srv, pSrv)
	rawMsg := `{
		"method": "lsps0.list_protocols",
		"jsonrpc": "2.0",
		"id": "example#3cad6a54d302edba4c9ade2f7ffac098",
		"params": []
	  }`
	mock := newMock(nil)
	mock.req <- &lightning.CustomMessage{
		PeerId: "AAA",
		Type:   37913,
		Data:   []byte(rawMsg),
	}
	go srv.Serve(mock)

	resp := <-mock.resp

	r := new(jsonrpc.Error)
	err := json.Unmarshal(resp.Data, r)
	assert.NoError(t, err)

	assert.Equal(t, uint32(37913), resp.Type)
	assert.Equal(t, "AAA", resp.PeerId)
	assert.Equal(t, "example#3cad6a54d302edba4c9ade2f7ffac098", *r.Id)
	assert.Equal(t, "2.0", r.JsonRpc)
	assert.Equal(t, r.Error.Code, int32(-32602))
	assert.Equal(t, r.Error.Message, "invalid params")
}
