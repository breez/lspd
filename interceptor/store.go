package interceptor

import (
	"time"

	"github.com/breez/lspd/common"
	"github.com/btcsuite/btcd/wire"
)

type InterceptStore interface {
	PaymentInfo(htlcPaymentHash []byte) (string, *common.OpeningFeeParams, []byte, []byte, []byte, int64, int64, *wire.OutPoint, *string, error)
	SetFundingTx(paymentHash []byte, channelPoint *wire.OutPoint) error
	RegisterPayment(token string, params *common.OpeningFeeParams, destination, paymentHash, paymentSecret []byte, incomingAmountMsat, outgoingAmountMsat int64, tag string) error
	InsertChannel(initialChanID, confirmedChanId uint64, channelPoint string, nodeID []byte, lastUpdate time.Time) error
}
