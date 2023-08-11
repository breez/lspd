package jsonrpc

import "encoding/json"

var Version = "2.0"

type Request struct {
	JsonRpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Id      string          `json:"id"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JsonRpc string          `json:"jsonrpc"`
	Id      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type Error struct {
	JsonRpc string    `json:"jsonrpc"`
	Id      *string   `json:"id"`
	Error   ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    int32           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
