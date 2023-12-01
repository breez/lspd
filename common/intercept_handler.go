package common

import (
	"fmt"

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

type InterceptFailureCode []byte

var (
	FAILURE_TEMPORARY_CHANNEL_FAILURE            InterceptFailureCode = []byte{0x10, 0x07}
	FAILURE_AMOUNT_BELOW_MINIMUM                 InterceptFailureCode = []byte{0x10, 0x0B}
	FAILURE_INCORRECT_CLTV_EXPIRY                InterceptFailureCode = []byte{0x10, 0x0D}
	FAILURE_TEMPORARY_NODE_FAILURE               InterceptFailureCode = []byte{0x20, 0x02}
	FAILURE_UNKNOWN_NEXT_PEER                    InterceptFailureCode = []byte{0x40, 0x0A}
	FAILURE_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS InterceptFailureCode = []byte{0x40, 0x0F}
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

type InterceptResult struct {
	Action             InterceptAction
	FailureMessage     []byte
	Destination        []byte
	AmountMsat         uint64
	FeeMsat            *uint64
	TotalAmountMsat    uint64
	ChannelPoint       *wire.OutPoint
	Scid               lightning.ShortChannelID
	PaymentSecret      []byte
	UseLegacyOnionBlob bool
}

type InterceptHandler interface {
	Intercept(req InterceptRequest) InterceptResult
}
