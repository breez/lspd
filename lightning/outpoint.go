package lightning

import (
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

func NewOutPoint(fundingTxID []byte, index uint32) (*wire.OutPoint, error) {
	var h chainhash.Hash
	err := h.SetBytes(fundingTxID)
	if err != nil {
		log.Printf("h.SetBytes(%x) error: %v", fundingTxID, err)
		return nil, err
	}

	return wire.NewOutPoint(&h, index), nil
}

func NewOutPointFromString(outpoint string) (*wire.OutPoint, error) {
	split := strings.Split(outpoint, ":")
	if len(split) != 2 {
		return nil, fmt.Errorf("invalid outpoint")
	}

	fundingTxId, err := hex.DecodeString(split[0])
	if err != nil {
		return nil, fmt.Errorf("invalid outpoint")
	}

	outnum, err := strconv.ParseUint(split[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid outpoint")
	}

	return NewOutPoint(fundingTxId, uint32(outnum))
}
