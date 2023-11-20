package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

var minNotificationInterval time.Duration = time.Second * 30
var cleanInterval time.Duration = time.Minute * 2

type NotificationService struct {
	mtx                 sync.Mutex
	recentNotifications map[string]time.Time
	store               Store
}

func NewNotificationService(store Store) *NotificationService {
	return &NotificationService{
		store:               store,
		recentNotifications: make(map[string]time.Time),
	}
}

func (s *NotificationService) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cleanInterval):
		}

		s.mtx.Lock()
		for id, lastTime := range s.recentNotifications {
			if lastTime.Add(minNotificationInterval).Before(time.Now()) {
				delete(s.recentNotifications, id)
			}
		}
		s.mtx.Unlock()
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
	s.mtx.Lock()
	lastTime, ok := s.recentNotifications[paymentIdentifier(pubkey, paymenthash)]
	s.mtx.Unlock()
	if ok && lastTime.Add(minNotificationInterval).After(time.Now()) {
		// Treat as if we notified if the notification was recent.
		return true, nil
	}

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

	if notified {
		s.mtx.Lock()
		s.recentNotifications[paymentIdentifier(pubkey, paymenthash)] = time.Now()
		s.mtx.Unlock()
	}

	return notified, nil
}

func paymentIdentifier(pubkey string, paymentHash string) string {
	return pubkey + "|" + paymentHash
}
