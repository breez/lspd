package lnd

import (
	"context"
)

type MacaroonCredential struct {
	MacaroonHex string
}

func NewMacaroonCredential(hex string) *MacaroonCredential {
	return &MacaroonCredential{
		MacaroonHex: hex,
	}
}

func (m *MacaroonCredential) RequireTransportSecurity() bool {
	return true
}

func (m *MacaroonCredential) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	md := make(map[string]string)
	md["macaroon"] = m.MacaroonHex
	return md, nil
}
