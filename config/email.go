package config

import (
	"encoding/json"
	"os"
)

type Email struct {
	To   []*string
	Cc   []*string
	From string
}

func addresses(a string) (addr []*string) {
	json.Unmarshal([]byte(a), &addr)
	return
}

func loadOpenChannelEmailSettings() *Email {
	return &Email{
		To:   addresses(os.Getenv("OPENCHANNEL_NOTIFICATION_TO")),
		Cc:   addresses(os.Getenv("OPENCHANNEL_NOTIFICATION_CC")),
		From: os.Getenv("OPENCHANNEL_NOTIFICATION_FROM"),
	}
}
