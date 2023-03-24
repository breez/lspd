package lightning

import (
	"sync"

	"github.com/breez/lspd/basetypes"
)

type ScidCacheReader interface {
	ContainsScid(scid basetypes.ShortChannelID) bool
}

type ScidCacheWriter interface {
	AddScids(scids ...basetypes.ShortChannelID)
	RemoveScids(scids ...basetypes.ShortChannelID)
	ReplaceScids(scids []basetypes.ShortChannelID)
}

type ScidCache struct {
	activeScids map[uint64]bool
	mtx         sync.RWMutex
}

func NewScidCache() *ScidCache {
	return &ScidCache{
		activeScids: make(map[uint64]bool),
	}
}

func (s *ScidCache) ContainsScid(
	scid basetypes.ShortChannelID,
) bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	_, ok := s.activeScids[uint64(scid)]
	return ok
}

func (s *ScidCache) AddScids(
	scids ...basetypes.ShortChannelID,
) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, scid := range scids {
		s.activeScids[uint64(scid)] = true
	}
}

func (s *ScidCache) RemoveScids(
	scids ...basetypes.ShortChannelID,
) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, scid := range scids {
		delete(s.activeScids, uint64(scid))
	}
}

func (s *ScidCache) ReplaceScids(
	scids []basetypes.ShortChannelID,
) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.activeScids = make(map[uint64]bool)
	for _, scid := range scids {
		s.activeScids[uint64(scid)] = true
	}
}
