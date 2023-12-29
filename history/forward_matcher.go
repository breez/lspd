package history

import (
	"context"
	"fmt"
	"log"
	"time"
)

type ForwardMatcher struct {
	store Store
}

func NewForwardMatcher(store Store) *ForwardMatcher {
	return &ForwardMatcher{
		store: store,
	}
}

const (
	forwardMatchInterval     time.Duration = time.Hour
	perForwardMatchTimeRange time.Duration = time.Hour * 24 * 7
)

func (s *ForwardMatcher) MatchForwards(ctx context.Context) {
	s.matchForwardsOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(forwardMatchInterval):
		}

		s.matchForwardsOnce(ctx)
	}
}

func (s *ForwardMatcher) matchForwardsOnce(ctx context.Context) {
	firstMatchedForwardTime, lastMatchedForwardTime, err := s.store.GetFirstAndLastMatchedForwardTimes(ctx, true)
	if err != nil {
		log.Printf("matchForwardsOnce: could not get first and last matched internal forward time, syncing until genesis: %v", err)
	}
	s.matchForwardsOnceExpandedRange(ctx, firstMatchedForwardTime, lastMatchedForwardTime, s.store.MatchInternalForwards)

	firstMatchedForwardTime, lastMatchedForwardTime, err = s.store.GetFirstAndLastMatchedForwardTimes(ctx, false)
	if err != nil {
		log.Printf("matchForwardsOnce: could not get first and last matched external forward time, syncing until genesis: %v", err)
	}
	s.matchForwardsOnceExpandedRange(ctx, firstMatchedForwardTime, lastMatchedForwardTime, s.store.MatchExternalForwards)
}

func (s *ForwardMatcher) matchForwardsOnceExpandedRange(ctx context.Context, firstMatchedForwardTime, lastMatchedForwardTime *time.Time, matchFunc func(context.Context, time.Time, time.Time) error) error {
	log.Printf("matchForwardsOnceExpandedRange() - Begin")

	end := time.Now().Add(-perForwardMatchTimeRange)
	if lastMatchedForwardTime == nil {
		// Match from now (a week ago) until the first forward ever.
		err := s.matchInRange(ctx, nil, end, matchFunc)
		if err != nil {
			return fmt.Errorf("failed to match from now to first forward ever: %w", err)
		}
	} else {
		// First match from now until the last matched forward
		err := s.matchInRange(ctx, lastMatchedForwardTime, end, matchFunc)
		if err != nil {
			return fmt.Errorf("failed to match from now to last matched forward: %w", err)
		}

		// Then sync from the first synced forward until the first forward ever.
		// This is in case we missed matching forwards on the first ever run.
		end := (*firstMatchedForwardTime).Add(-perForwardMatchTimeRange / 2)
		err = s.matchInRange(ctx, nil, end, matchFunc)
		if err != nil {
			return fmt.Errorf("failed to match from first synced forward to first forward ever: %w", err)
		}
	}

	log.Printf("matchForwardsOnceExpandedRange() - Done")
	return nil
}

func (s *ForwardMatcher) matchInRange(ctx context.Context, absoluteStart *time.Time, absoluteEnd time.Time, matchFunc func(context.Context, time.Time, time.Time) error) error {
	end := absoluteEnd
	start := end.Add(-perForwardMatchTimeRange)

	if absoluteStart == nil {
		var err error
		absoluteStart, err = s.store.GetFirstForwardTime(ctx)
		if err != nil {
			return fmt.Errorf("could not get first ever forward time: %w", err)
		}

		if absoluteStart == nil {
			log.Printf("matchInRange: no forwards. Nothing to match.")
			return nil
		}
	}

	// Iterate the forwards backwards. Because later forwards have higher chances
	// of being matched to a channel.
	for absoluteStart == nil || end.After(*absoluteStart) {
		log.Printf("matchInRange() - Range start %v - end %v", start, end)

		// TODO: How to make sure channels and forwards are synced?
		count, err := s.store.GetForwardsWithoutChannelCount(ctx)
		if err != nil {
			return fmt.Errorf("error getting unmatched forward-channel count: %w", err)
		}

		if count > 0 {
			log.Printf("matchInRange() - There are forwards without channels in range start %v - end %v. Stopping matching.", start, end)
			break
		}

		err = matchFunc(ctx, start, end)
		if err != nil {
			return fmt.Errorf("error matching forwards: %w", err)
		}

		log.Printf("matchInRange() - Range start %v - end %v complete", start, end)

		// Use a rolling interval, to increase the odds we match all forwards
		// Also the ones that fall off the time interval.
		end = end.Add(-perForwardMatchTimeRange / 2)
		start = start.Add(-perForwardMatchTimeRange / 2)
	}

	return nil
}
