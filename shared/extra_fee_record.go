package shared

import "github.com/lightningnetwork/lnd/tlv"

var ExtraFeeTlvType tlv.Type = 65536

func NewExtraFeeRecord(feeMsat *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(ExtraFeeTlvType, feeMsat)
}
