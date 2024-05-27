package lntest

import (
	"context"
	"encoding/hex"
	"os"
)

type MacaroonCredential struct {
	Macaroon []byte
}

func NewMacaroonCredential(m []byte) *MacaroonCredential {
	return &MacaroonCredential{
		Macaroon: m,
	}
}

func NewMacaroonCredentialFromFile(path string) (*MacaroonCredential, error) {
	m, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return NewMacaroonCredential(m), nil
}

func (m *MacaroonCredential) RequireTransportSecurity() bool {
	return true
}

func (m *MacaroonCredential) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	md := make(map[string]string)
	md["macaroon"] = hex.EncodeToString(m.Macaroon)
	return md, nil
}
