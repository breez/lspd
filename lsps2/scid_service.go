package lsps2

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/breez/lspd/lightning"
)

const maxScidAttempts = 5

var two = big.NewInt(2)
var sixtyfour = big.NewInt(64)
var maxUint64 = two.Exp(two, sixtyfour, nil)

func newScid() (*basetypes.ShortChannelID, error) {
	s, err := rand.Int(rand.Reader, maxUint64)
	if err != nil {
		return nil, err
	}

	scid := basetypes.ShortChannelID(s.Uint64())
	return &scid, nil
}

type ScidService struct {
	lspId []byte
	store ScidStore
	cache lightning.ScidCacheReader
}

func NewScidService(
	lspId []byte,
	store ScidStore,
	cache lightning.ScidCacheReader,
) *ScidService {
	return &ScidService{lspId: lspId, store: store, cache: cache}
}

func (s *ScidService) ReserveNewScid(
	ctx context.Context,
	expiry time.Time,
) (*basetypes.ShortChannelID, error) {
	for attempts := 0; attempts < maxScidAttempts; attempts++ {
		scid, err := newScid()
		if err != nil {
			log.Printf("NewScid() error: %v", err)
			continue
		}

		if s.cache.ContainsScid(*scid) {
			log.Printf(
				"Collision with existing channel when generating new scid %s",
				scid.ToString(),
			)
			continue
		}

		err = s.store.AddScid(ctx, s.lspId, *scid, expiry)
		if err == ErrScidExists {
			log.Printf(
				"Collision on inserting random new scid %s",
				scid.ToString(),
			)
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("failed to insert scid reservation: %w", err)
		}

		return scid, nil
	}

	return nil, fmt.Errorf(
		"failed to reserve scid after %v attempts",
		maxScidAttempts,
	)
}
