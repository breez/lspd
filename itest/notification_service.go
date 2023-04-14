package itest

import (
	"context"
	"net"
	"net/http"
)

type PaymentReceivedPayload struct {
	Template string `json:"template" binding:"required,eq=payment_received"`
	Data     struct {
		PaymentHash string `json:"payment_hash" binding:"required"`
	} `json:"data"`
}

type TxConfirmedPayload struct {
	Template string `json:"template" binding:"required,eq=tx_confirmed"`
	Data     struct {
		TxID string `json:"tx_id" binding:"required"`
	} `json:"data"`
}

type AddressTxsChangedPayload struct {
	Template string `json:"template" binding:"required,eq=address_txs_changed"`
	Data     struct {
		Address string `json:"address" binding:"required"`
	} `json:"data"`
}

type notificationDeliveryService struct {
	addr       string
	handleFunc func(resp http.ResponseWriter, req *http.Request)
}

func newNotificationDeliveryService(
	addr string,
	handleFunc func(resp http.ResponseWriter, req *http.Request),
) *notificationDeliveryService {
	return &notificationDeliveryService{
		addr:       addr,
		handleFunc: handleFunc,
	}
}

func (s *notificationDeliveryService) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notify", s.handleFunc)
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		lis.Close()
	}()

	return http.Serve(lis, mux)
}
