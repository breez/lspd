package cln

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/breez/lspd/cln/rpc"
	"github.com/breez/lspd/history"
	"github.com/breez/lspd/lightning"
)

type ForwardSync struct {
	createdOffset uint64
	updatedOffset uint64
	nodeid        []byte
	client        *ClnClient
	store         history.Store
}

func NewForwardSync(nodeid []byte, client *ClnClient, store history.Store) *ForwardSync {
	return &ForwardSync{
		nodeid: nodeid,
		client: client,
		store:  store,
	}
}

var forwardSyncInterval time.Duration = time.Minute * 5

func (s *ForwardSync) ForwardsSynchronize(ctx context.Context) {
	s.createdOffset, s.updatedOffset, _ = s.store.FetchClnForwardOffsets(ctx, s.nodeid)
	s.forwardsSynchronizeOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(forwardSyncInterval):
		}

		s.forwardsSynchronizeOnce(ctx)
	}
}

func (s *ForwardSync) forwardsSynchronizeOnce(ctx context.Context) {
	s.forwardCreatedSynchronizeOnce(ctx)
	s.forwardUpdatedSynchronizeOnce(ctx)
}

func (s *ForwardSync) forwardCreatedSynchronizeOnce(ctx context.Context) {
	log.Printf("forwardCreatedSynchronizeOnce(%x) - Begin", s.nodeid)
	var limit uint32 = 10000
	endReached := false
	round := 0
	for !endReached {
		log.Printf("forwardCreatedSynchronizeOnce(%x) - round %v, offset %v", s.nodeid, round, s.createdOffset)
		var forwards []*history.Forward
		var newCreatedOffset uint64
		var err error
		forwards, newCreatedOffset, endReached, err = s.listForwards(
			rpc.ListforwardsRequest_CREATED, s.createdOffset, limit)
		if err != nil {
			log.Printf("forwardCreatedSynchronizeOnce(%x)- ListForwards error: %v", s.nodeid, err)
			return
		}

		log.Printf("forwardCreatedSynchronizeOnce(%x) - round %v, offset %v yielded %v forwards", s.nodeid, round, s.createdOffset, len(forwards))
		if len(forwards) == 0 {
			break
		}

		err = s.store.InsertForwards(ctx, forwards, s.nodeid)
		if err != nil {
			log.Printf("forwardCreatedSynchronizeOnce(%x) - store.InsertForwards error: %v", s.nodeid, err)
			return
		}

		err = s.store.SetClnForwardOffsets(ctx, s.nodeid, newCreatedOffset, s.updatedOffset)
		if err != nil {
			log.Printf("forwardCreatedSynchronizeOnce(%x) - store.SetClnForwardOffsets error: %v", s.nodeid, err)
			return
		}

		s.createdOffset = newCreatedOffset
	}

	log.Printf("forwardCreatedSynchronizeOnce(%x) - Done", s.nodeid)
}

func (s *ForwardSync) forwardUpdatedSynchronizeOnce(ctx context.Context) {
	log.Printf("forwardUpdatedSynchronizeOnce(%x) - Begin", s.nodeid)
	var limit uint32 = 10000
	endReached := false
	round := 0
	for !endReached {
		log.Printf("forwardUpdatedSynchronizeOnce(%x) - round %v, offset %v", s.nodeid, round, s.updatedOffset)
		var forwards []*history.Forward
		var newUpdatedOffset uint64
		var err error
		forwards, newUpdatedOffset, endReached, err = s.listForwards(
			rpc.ListforwardsRequest_UPDATED, s.updatedOffset, limit)
		if err != nil {
			log.Printf("forwardUpdatedSynchronizeOnce(%x)- ListForwards error: %v", s.nodeid, err)
			return
		}

		log.Printf("forwardUpdatedSynchronizeOnce(%x) - round %v, offset %v yielded %v forwards", s.nodeid, round, s.updatedOffset, len(forwards))
		if len(forwards) == 0 {
			break
		}

		err = s.store.UpdateForwards(ctx, forwards, s.nodeid)
		if err != nil {
			log.Printf("forwardUpdatedSynchronizeOnce(%x) - store.InsertForwards error: %v", s.nodeid, err)
			return
		}

		err = s.store.SetClnForwardOffsets(ctx, s.nodeid, s.createdOffset, newUpdatedOffset)
		if err != nil {
			log.Printf("forwardUpdatedSynchronizeOnce(%x) - store.SetClnForwardOffsets error: %v", s.nodeid, err)
			return
		}

		s.updatedOffset = newUpdatedOffset
	}

	log.Printf("forwardUpdatedSynchronizeOnce(%x) - Done", s.nodeid)
}

func (s *ForwardSync) listForwards(
	index rpc.ListforwardsRequest_ListforwardsIndex,
	offset uint64,
	limit uint32,
) ([]*history.Forward, uint64, bool, error) {
	rLimit := limit * 2
	status := rpc.ListforwardsRequest_SETTLED
	response, err := s.client.client.ListForwards(
		context.Background(),
		&rpc.ListforwardsRequest{
			Status: &status,
			Index:  &index,
			Start:  &offset,
			Limit:  &rLimit,
		},
	)
	if err != nil {
		return nil, 0, false, err
	}

	var result []*history.Forward
	endReached := len(response.Forwards) < int(limit)
	var lastIndex *uint64
	for _, forward := range response.Forwards {
		if forward.OutChannel == nil {
			continue
		}
		if forward.CreatedIndex == nil {
			continue
		}
		if forward.InMsat == nil {
			continue
		}
		if forward.OutMsat == nil {
			continue
		}

		in, err := lightning.NewShortChannelIDFromString(forward.InChannel)
		if err != nil {
			return nil, 0, false, fmt.Errorf("NewShortChannelIDFromString(%s) error: %w", forward.InChannel, err)
		}
		out, err := lightning.NewShortChannelIDFromString(*forward.OutChannel)
		if err != nil {
			return nil, 0, false, fmt.Errorf("NewShortChannelIDFromString(%s) error: %w", *forward.OutChannel, err)
		}

		sec, dec := math.Modf(forward.ReceivedTime)
		result = append(result, &history.Forward{
			Identifier:   strconv.FormatUint(*forward.CreatedIndex, 10),
			InChannel:    *in,
			OutChannel:   *out,
			InMsat:       forward.InMsat.Msat,
			OutMsat:      forward.OutMsat.Msat,
			ResolvedTime: time.Unix(int64(sec), int64(dec*(1e9))),
		})
		if index == rpc.ListforwardsRequest_CREATED {
			lastIndex = forward.CreatedIndex
		} else {
			lastIndex = forward.UpdatedIndex
		}
	}

	var newOffset uint64
	if lastIndex == nil {
		newOffset = 0
	} else {
		newOffset = *lastIndex + 1
	}
	return result, newOffset, endReached, nil
}
