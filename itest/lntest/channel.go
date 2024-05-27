package lntest

import (
	"bytes"
)

type ChannelInfo struct {
	From            LightningNode
	To              LightningNode
	FundingTxId     []byte
	FundingTxOutnum uint32
}

func (c *ChannelInfo) WaitForChannelReady() ShortChannelID {
	c.From.WaitForChannelReady(c)
	return c.To.WaitForChannelReady(c)
}

func (c *ChannelInfo) GetPeer(this LightningNode) LightningNode {
	if bytes.Equal(this.NodeId(), c.From.NodeId()) {
		return c.To
	} else {
		return c.From
	}
}
