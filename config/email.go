package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Email struct {
	To   []*string
	Cc   []*string
	From string
}

func addresses(a string) ([]*string, error) {
	var addr []*string
	err := json.Unmarshal([]byte(a), &addr)
	return addr, err
}

func loadOpenChannelEmailSettings() (*Email, error) {
	from := os.Getenv("OPENCHANNEL_NOTIFICATION_FROM")
	to := os.Getenv("OPENCHANNEL_NOTIFICATION_TO")
	cc := os.Getenv("OPENCHANNEL_NOTIFICATION_CC")

	if from == "" && to == "" && cc == "" {
		return nil, nil
	}

	t, err := addresses(to)
	if err != nil {
		return nil, fmt.Errorf("invalid OPENCHANNEL_NOTIFICATION_TO: %v", err)
	}

	c, err := addresses(cc)
	if err != nil {
		return nil, fmt.Errorf("invalid OPENCHANNEL_NOTIFICATION_CC: %v", err)
	}

	return &Email{
		To:   t,
		Cc:   c,
		From: from,
	}, nil
}
