package common

import (
	"bytes"
	"time"

	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/lnwire"
)

func ConstructChanUpdate(
	chainhash chainhash.Hash,
	node []byte,
	destination []byte,
	scid lightning.ShortChannelID,
	timeLockDelta uint16,
	htlcMinimumMsat,
	htlcMaximumMsat uint64,
) lnwire.ChannelUpdate {
	channelFlags := lnwire.ChanUpdateChanFlags(0)
	if bytes.Compare(node, destination) > 0 {
		channelFlags = 1
	}

	return lnwire.ChannelUpdate{
		ChainHash:       chainhash,
		ShortChannelID:  scid.ToLnwire(),
		Timestamp:       uint32(time.Now().Unix()),
		TimeLockDelta:   timeLockDelta,
		HtlcMinimumMsat: lnwire.MilliSatoshi(htlcMinimumMsat),
		HtlcMaximumMsat: lnwire.MilliSatoshi(htlcMaximumMsat),
		BaseFee:         0,
		FeeRate:         0,
		MessageFlags:    lnwire.ChanUpdateRequiredMaxHtlc,
		ChannelFlags:    channelFlags,
	}
}
