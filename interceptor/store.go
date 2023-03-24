package interceptor

import (
	"time"

	"github.com/btcsuite/btcd/wire"
)

type InterceptStore interface {
	PaymentInfo(htlcPaymentHash []byte) ([]byte, []byte, []byte, int64, int64, *wire.OutPoint, error)
	SetFundingTx(paymentHash []byte, channelPoint *wire.OutPoint) error
	RegisterPayment(destination, paymentHash, paymentSecret []byte, incomingAmountMsat, outgoingAmountMsat int64, tag string) error
	InsertChannel(initialChanID, confirmedChanId uint64, channelPoint string, nodeID []byte, lastUpdate time.Time) error
}
