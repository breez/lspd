package lightning

import (
	"log"

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
