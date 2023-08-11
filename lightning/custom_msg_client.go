package lightning

type CustomMessage struct {
	PeerId string
	Type   uint32
	Data   []byte
}

type CustomMsgClient interface {
	Recv() (*CustomMessage, error)
	Send(*CustomMessage) error
}
