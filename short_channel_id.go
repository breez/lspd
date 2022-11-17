package main

import (
	"fmt"
	"strconv"
	"strings"
)

type ShortChannelID uint64

func NewShortChannelIDFromString(channelID string) (*ShortChannelID, error) {
	parts := strings.Split(channelID, "x")
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected 3 parts, got %d", len(parts))
	}

	blockHeight, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, err
	}

	txIndex, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, err
	}

	outputIndex, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, err
	}

	result := ShortChannelID(
		(uint64(outputIndex) & 0xFFFF) +
			((uint64(txIndex) << 16) & 0xFFFFFF0000) +
			((uint64(blockHeight) << 40) & 0xFFFFFF0000000000),
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
