package common

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

func MapChainHash(network string) *chainhash.Hash {
	switch network {
	case "bitcoin":
		return chaincfg.MainNetParams.GenesisHash
	case "testnet":
		return chaincfg.TestNet3Params.GenesisHash
	case "regtest":
		return chaincfg.RegressionNetParams.GenesisHash
	default:
		return chaincfg.MainNetParams.GenesisHash
	}
}
