package interceptor

import (
	"time"

	"github.com/btcsuite/btcd/wire"
)

type OpeningFeeParams struct {
	MinMsat              uint64 `json:"min_msat,string"`
	Proportional         uint32 `json:"proportional"`
	ValidUntil           string `json:"valid_until"`
	MaxIdleTime          uint32 `json:"max_idle_time"`
	MaxClientToSelfDelay uint32 `json:"max_client_to_self_delay"`
	Promise              string `json:"promise"`
}

type InterceptStore interface {
	PaymentInfo(htlcPaymentHash []byte) (*OpeningFeeParams, []byte, []byte, []byte, int64, int64, *wire.OutPoint, error)
	SetFundingTx(paymentHash []byte, channelPoint *wire.OutPoint) error
	RegisterPayment(params *OpeningFeeParams, destination, paymentHash, paymentSecret []byte, incomingAmountMsat, outgoingAmountMsat int64, tag string) error
	InsertChannel(initialChanID, confirmedChanId uint64, channelPoint string, nodeID []byte, lastUpdate time.Time) error
}
