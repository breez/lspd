package postgresql

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/breez/lspd/basetypes"
	"github.com/btcsuite/btcd/wire"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

type PostgresInterceptStore struct {
	pool *pgxpool.Pool
}

func NewPostgresInterceptStore(pool *pgxpool.Pool) *PostgresInterceptStore {
	return &PostgresInterceptStore{pool: pool}
}

func (s *PostgresInterceptStore) PaymentInfo(htlcPaymentHash []byte) ([]byte, []byte, []byte, int64, int64, *wire.OutPoint, error) {
	var (
		paymentHash, paymentSecret, destination []byte
		incomingAmountMsat, outgoingAmountMsat  int64
		fundingTxID                             []byte
		fundingTxOutnum                         pgtype.Int4
	)
	err := s.pool.QueryRow(context.Background(),
		`SELECT payment_hash, payment_secret, destination, incoming_amount_msat, outgoing_amount_msat, funding_tx_id, funding_tx_outnum
			FROM payments
			WHERE payment_hash=$1 OR sha256('probing-01:' || payment_hash)=$1`,
		htlcPaymentHash).Scan(&paymentHash, &paymentSecret, &destination, &incomingAmountMsat, &outgoingAmountMsat, &fundingTxID, &fundingTxOutnum)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = nil
		}
		return nil, nil, nil, 0, 0, nil, err
	}

	var cp *wire.OutPoint
	if fundingTxID != nil {
		cp, err = basetypes.NewOutPoint(fundingTxID, uint32(fundingTxOutnum.Int))
		if err != nil {
			log.Printf("invalid funding txid in database %x", fundingTxID)
		}
	}
	return paymentHash, paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, cp, nil
}

func (s *PostgresInterceptStore) SetFundingTx(paymentHash []byte, channelPoint *wire.OutPoint) error {
	commandTag, err := s.pool.Exec(context.Background(),
		`UPDATE payments
			SET funding_tx_id = $2, funding_tx_outnum = $3
			WHERE payment_hash=$1`,
		paymentHash, channelPoint.Hash[:], channelPoint.Index)
	log.Printf("setFundingTx(%x, %s, %d): %s err: %v", paymentHash, channelPoint.Hash.String(), channelPoint.Index, commandTag, err)
	return err
}

func (s *PostgresInterceptStore) RegisterPayment(destination, paymentHash, paymentSecret []byte, incomingAmountMsat, outgoingAmountMsat int64, tag string) error {
	var t *string
	if tag != "" {
		t = &tag
	}
	commandTag, err := s.pool.Exec(context.Background(),
		`INSERT INTO
		payments (destination, payment_hash, payment_secret, incoming_amount_msat, outgoing_amount_msat, tag)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat, t)
	log.Printf("registerPayment(%x, %x, %x, %v, %v, %v) rows: %v err: %v",
		destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat, tag, commandTag.RowsAffected(), err)
	if err != nil {
		return fmt.Errorf("registerPayment(%x, %x, %x, %v, %v, %v) error: %w",
			destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat, tag, err)
	}
	return nil
}

func (s *PostgresInterceptStore) InsertChannel(initialChanID, confirmedChanId uint64, channelPoint string, nodeID []byte, lastUpdate time.Time) error {

	query := `INSERT INTO
	channels (initial_chanid, confirmed_chanid, channel_point, nodeid, last_update)
	VALUES ($1, NULLIF($2, 0::int8), $3, $4, $5)
	ON CONFLICT (channel_point) DO UPDATE SET confirmed_chanid=NULLIF($2, 0::int8), last_update=$5`

	c, err := s.pool.Exec(context.Background(),
		query, int64(initialChanID), int64(confirmedChanId), channelPoint, nodeID, lastUpdate)
	if err != nil {
		log.Printf("insertChannel(%v, %v, %s, %x) error: %v",
			initialChanID, confirmedChanId, channelPoint, nodeID, err)
		return fmt.Errorf("insertChannel(%v, %v, %s, %x) error: %w",
			initialChanID, confirmedChanId, channelPoint, nodeID, err)
	}
	log.Printf("insertChannel(%v, %v, %x) result: %v",
		initialChanID, confirmedChanId, nodeID, c.String())
	return nil
}
