package lnd

type CopyFromSource interface {
	Next() bool
	Values() ([]interface{}, error)
	Err() error
}

type ForwardingEventStore interface {
	LastForwardingEvent() (int64, error)
	InsertForwardingEvents(rowSrc CopyFromSource) error
}
