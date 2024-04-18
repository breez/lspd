package common

import (
	"fmt"
	"strconv"

	"github.com/breez/lspd/lightning"
	"github.com/btcsuite/btcd/wire"
)

type InterceptAction int

const (
	INTERCEPT_RESUME              InterceptAction = 0
	INTERCEPT_RESUME_WITH_ONION   InterceptAction = 1
	INTERCEPT_FAIL_HTLC_WITH_CODE InterceptAction = 2
	INTERCEPT_IGNORE              InterceptAction = 3
)

type InterceptFailureCode uint16

var (
	FAILURE_TEMPORARY_CHANNEL_FAILURE            InterceptFailureCode = 0x1007
	FAILURE_AMOUNT_BELOW_MINIMUM                 InterceptFailureCode = 0x100B
	FAILURE_INCORRECT_CLTV_EXPIRY                InterceptFailureCode = 0x100D
	FAILURE_TEMPORARY_NODE_FAILURE               InterceptFailureCode = 0x2002
	FAILURE_UNKNOWN_NEXT_PEER                    InterceptFailureCode = 0x400A
	FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS InterceptFailureCode = 0x400F
)

type InterceptRequest struct {
	// Identifier that uniquely identifies this htlc.
	// For cln, that's hash of the next onion or the shared secret.
	Identifier         string
	Scid               lightning.ShortChannelID
	PaymentHash        []byte
	IncomingAmountMsat uint64
	OutgoingAmountMsat uint64
	IncomingExpiry     uint32
	OutgoingExpiry     uint32
}

func (r *InterceptRequest) PaymentId() string {
	return fmt.Sprintf("%s|%x", r.Scid.ToString(), r.PaymentHash)
}

func (r *InterceptRequest) HtlcId() string {
	return r.Identifier
}

func (r *InterceptRequest) String() string {
	return fmt.Sprintf(
		"id: %s, paymenthash: %x, scid: %s, amt_in: %d, amt_out: %d, expiry_in: %d, expiry_out: %d",
		r.Identifier,
		r.PaymentHash,
		r.Scid.ToString(),
		r.IncomingAmountMsat,
		r.OutgoingAmountMsat,
		r.IncomingExpiry,
		r.OutgoingExpiry,
	)
}

type InterceptResult struct {
	Action             InterceptAction
	FailureCode        InterceptFailureCode
	Destination        []byte
	AmountMsat         uint64
	FeeMsat            *uint64
	TotalAmountMsat    uint64
	ChannelPoint       *wire.OutPoint
	Scid               lightning.ShortChannelID
	PaymentSecret      []byte
	UseLegacyOnionBlob bool
}

func (r *InterceptResult) String() string {
	var feeMsat string
	if r.FeeMsat != nil {
		feeMsat = strconv.FormatUint(*r.FeeMsat, 10)
	}
	var cp string
	if r.ChannelPoint != nil {
		cp = r.ChannelPoint.String()
	}
	return fmt.Sprintf(
		"action: %v, code: %v, destination: %x, amt: %d, fee: %s, total_amt: %d, channelpoint: %s, scid: %s, use_legacy_blob: %t",
		r.Action,
		r.FailureCode,
		r.Destination,
		r.AmountMsat,
		feeMsat,
		r.TotalAmountMsat,
		cp,
		r.Scid.ToString(),
		r.UseLegacyOnionBlob,
	)
}

type InterceptHandler interface {
	Intercept(req InterceptRequest) InterceptResult
}
