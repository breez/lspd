package lightning

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lightningnetwork/lnd/lnwire"
)

type ShortChannelID uint64

func NewShortChannelIDFromString(channelID string) (*ShortChannelID, error) {
	if channelID == "" {
		c := ShortChannelID(0)
		return &c, nil
	}

	fields := strings.Split(channelID, "x")
	if len(fields) != 3 {
		return nil, fmt.Errorf("invalid short channel id %v", channelID)
	}
	var blockHeight, txIndex, txPos int64
	var err error
	if blockHeight, err = strconv.ParseInt(fields[0], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse block height %v", fields[0])
	}
	if txIndex, err = strconv.ParseInt(fields[1], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse block height %v", fields[1])
	}
	if txPos, err = strconv.ParseInt(fields[2], 10, 64); err != nil {
		return nil, fmt.Errorf("failed to parse block height %v", fields[2])
	}

	result := ShortChannelID(
		lnwire.ShortChannelID{
			BlockHeight: uint32(blockHeight),
			TxIndex:     uint32(txIndex),
			TxPosition:  uint16(txPos),
		}.ToUint64(),
	)
	return &result, nil
}

func (c *ShortChannelID) ToString() string {
	u := uint64(*c)
	blockHeight := (u >> 40) & 0xFFFFFF
	txIndex := (u >> 16) & 0xFFFFFF
	outputIndex := u & 0xFFFF
	return fmt.Sprintf("%dx%dx%d", blockHeight, txIndex, outputIndex)
}
