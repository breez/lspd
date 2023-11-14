package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
)

type NotificationService struct {
	store Store
}

func NewNotificationService(store Store) *NotificationService {
	return &NotificationService{
		store: store,
	}
}

type PaymentReceivedPayload struct {
	Template string `json:"template" binding:"required,eq=payment_received"`
	Data     struct {
		PaymentHash string `json:"payment_hash" binding:"required"`
	} `json:"data"`
}

func (s *NotificationService) Notify(
	pubkey string,
	paymenthash string,
) (bool, error) {
	registrations, err := s.store.GetRegistrations(context.Background(), pubkey)
	if err != nil {
		log.Printf("Failed to get notification registrations for %s: %v", pubkey, err)
		return false, err
	}

	req := &PaymentReceivedPayload{
		Template: "payment_received",
		Data: struct {
			PaymentHash string "json:\"payment_hash\" binding:\"required\""
		}{
			PaymentHash: paymenthash,
		},
	}

	notified := false
	for _, r := range registrations {
		var buf bytes.Buffer
		err = json.NewEncoder(&buf).Encode(req)
		if err != nil {
			log.Printf("Failed to encode payment notification for %s: %v", pubkey, err)
			return false, err
		}

		resp, err := http.DefaultClient.Post(r, "application/json", &buf)
		if err != nil {
			log.Printf("Failed to send payment notification for %s to %s: %v", pubkey, r, err)
			// TODO: Remove subscription?
			continue
		}

		if resp.StatusCode != 200 {
			log.Printf("Got non 200 status code (%s) for payment notification for %s to %s: %v", resp.Status, pubkey, r, err)
			// TODO: Remove subscription?
			continue
		}

		notified = true
	}

	return notified, nil
}
