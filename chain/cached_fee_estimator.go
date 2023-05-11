package chain

import (
	"context"
	"sync"
	"time"
)

var cacheDuration time.Duration = time.Minute * 5

type feeCache struct {
	time       time.Time
	estimation *FeeEstimation
}

type CachedFeeEstimator struct {
	cache map[FeeStrategy]*feeCache
	inner FeeEstimator
	mtx   sync.Mutex
}

func NewCachedFeeEstimator(inner FeeEstimator) *CachedFeeEstimator {
	return &CachedFeeEstimator{
		inner: inner,
		cache: make(map[FeeStrategy]*feeCache),
	}
}

func (e *CachedFeeEstimator) EstimateFeeRate(
	ctx context.Context,
	strategy FeeStrategy,
) (*FeeEstimation, error) {

	// Make sure we're in a lock, because we're reading/writing a map.
	e.mtx.Lock()
	defer e.mtx.Unlock()

	// See if there's a cached value first.
	cached, ok := e.cache[strategy]

	// If there is and it's still valid, return that.
	if ok && cached.time.Add(cacheDuration).After(time.Now()) {
		return cached.estimation, nil
	}

	// There was no valid cache.
	// Fetch the new fee estimate.
	now := time.Now()
	estimation, err := e.inner.EstimateFeeRate(ctx, strategy)
	if err != nil {
		return nil, err
	}

	// Cache it.
	e.cache[strategy] = &feeCache{
		time:       now,
		estimation: estimation,
	}

	return estimation, nil
}
