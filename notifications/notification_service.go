package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

	var mtx sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(registrations))
	notified := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*7)

	for _, r := range registrations {
		go func(r string) {
			currentNotified := s.notifyOnce(ctx, req, pubkey, r)
			if currentNotified {
				mtx.Lock()
				notified = true
				mtx.Unlock()
			}
			wg.Done()
		}(r)
	}

	wg.Wait()
	cancel()

	if notified {
		s.mtx.Lock()
		s.recentNotifications[paymentIdentifier(pubkey, paymenthash)] = time.Now()
		s.mtx.Unlock()
	}
	return notified, nil
}

func (s *NotificationService) notifyOnce(ctx context.Context, req *PaymentReceivedPayload, pubkey string, url string) bool {
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(req)
	if err != nil {
		log.Printf("Failed to encode payment notification for %s: %v", pubkey, err)
		return false
	}

	resp, err := http.DefaultClient.Post(url, "application/json", &buf)
	if err != nil {
		log.Printf("Failed to send payment notification for %s to %s: %v", pubkey, url, err)
		// TODO: Remove subscription?
		return false
	}

	if resp.StatusCode != 200 {
		buf := make([]byte, 1000)
		bytesRead, _ := io.ReadFull(resp.Body, buf)
		respBody := buf[:bytesRead]
		log.Printf("Got non 200 status code (%s) for payment notification for %s to %s: %s", resp.Status, pubkey, url, respBody)
		// TODO: Remove subscription?
		return false
	}

	return true
}

func paymentIdentifier(pubkey string, paymentHash string) string {
	return pubkey + "|" + paymentHash
}
