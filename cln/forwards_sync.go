package cln

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/breez/lspd/history"
	"github.com/breez/lspd/lightning"
	"github.com/elementsproject/glightning/glightning"
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
		forwards, newCreatedOffset, endReached, err = s.listForwards("created", s.createdOffset, limit)
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
		forwards, newUpdatedOffset, endReached, err = s.listForwards("updated", s.updatedOffset, limit)
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

func (s *ForwardSync) listForwards(index string, offset uint64, limit uint32) ([]*history.Forward, uint64, bool, error) {
	var response struct {
		Forwards []Forward `json:"forwards"`
	}

	client, err := s.client.getClient()
	if err != nil {
		return nil, 0, false, err
	}

	err = client.Request(&glightning.ListForwardsRequest{
		Status: "settled",
		Index:  index,
		Start:  offset,
		Limit:  limit * 2,
	}, &response)
	if err != nil {
		return nil, 0, false, err
	}

	var result []*history.Forward
	endReached := len(response.Forwards) < int(limit)
	var lastIndex *uint64
	for _, forward := range response.Forwards {
		in, err := lightning.NewShortChannelIDFromString(forward.InChannel)
		if err != nil {
			return nil, 0, false, fmt.Errorf("NewShortChannelIDFromString(%s) error: %w", forward.InChannel, err)
		}
		out, err := lightning.NewShortChannelIDFromString(forward.OutChannel)
		if err != nil {
			return nil, 0, false, fmt.Errorf("NewShortChannelIDFromString(%s) error: %w", forward.OutChannel, err)
		}

		sec, dec := math.Modf(forward.ResolvedTime)
		result = append(result, &history.Forward{
			Identifier:   strconv.FormatUint(forward.CreatedIndex, 10),
			InChannel:    *in,
			OutChannel:   *out,
			InMsat:       forward.InMsat.MSat(),
			OutMsat:      forward.OutMsat.MSat(),
			ResolvedTime: time.Unix(int64(sec), int64(dec*(1e9))),
		})
		if index == "created" {
			lastIndex = &forward.CreatedIndex
		} else {
			lastIndex = &forward.UpdatedIndex
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

type Forward struct {
	CreatedIndex uint64            `json:"created_index"`
	UpdatedIndex uint64            `json:"updated_index"`
	InChannel    string            `json:"in_channel"`
	OutChannel   string            `json:"out_channel"`
	InMsat       glightning.Amount `json:"in_msat"`
	OutMsat      glightning.Amount `json:"out_msat"`
	Status       string            `json:"status"`
	PaymentHash  string            `json:"payment_hash"`
	ReceivedTime float64           `json:"received_time"`
	ResolvedTime float64           `json:"resolved_time"`
}
