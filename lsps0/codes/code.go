package codes

type Code int32

const (
	OK             Code = 0
	Canceled       Code = 1
	Unknown        Code = 2
	ParseError     Code = -32700
	InvalidRequest Code = -32600
	MethodNotFound Code = -32601
	InvalidParams  Code = -32602
	InternalError  Code = -32603
)
