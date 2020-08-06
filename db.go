package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

var (
	pgxPool *pgxpool.Pool
)

func pgConnect() error {
	var err error
	pgxPool, err = pgxpool.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("pgxpool.Connect(%v): %w", os.Getenv("DATABASE_URL"), err)
	}
	return nil
}

func paymentInfo(paymentHash []byte) ([]byte, []byte, int64, int64, []byte, int32, error) {
	var (
		paymentSecret, destination             []byte
		incomingAmountMsat, outgoingAmountMsat int64
		fundingTxID                            []byte
		fundingTxOutnum                        pgtype.Int4
	)
	err := pgxPool.QueryRow(context.Background(),
		`SELECT payment_secret, destination, incoming_amount_msat, outgoing_amount_msat, funding_tx_id, funding_tx_outnum
			FROM payments
			WHERE payment_hash=$1`,
		paymentHash).Scan(&paymentSecret, &destination, &incomingAmountMsat, &outgoingAmountMsat, &fundingTxID, &fundingTxOutnum)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = nil
		}
		return nil, nil, 0, 0, nil, 0, err
	}
	return paymentSecret, destination, incomingAmountMsat, outgoingAmountMsat, fundingTxID, fundingTxOutnum.Int, nil
}

func setFundingTx(paymentHash, fundingTxID []byte, fundingTxOutnum int) error {
	commandTag, err := pgxPool.Exec(context.Background(),
		`UPDATE payments
			SET funding_tx_id = $2, funding_tx_outnum = $3
			WHERE payment_hash=$1`,
		paymentHash, fundingTxID, fundingTxOutnum)
	log.Printf("setFundingTx(%x, %x, %v): %s err: %v", paymentHash, fundingTxID, fundingTxOutnum, commandTag, err)
	return err
}

func registerPayment(destination, paymentHash, paymentSecret []byte, incomingAmountMsat, outgoingAmountMsat int64) error {
	commandTag, err := pgxPool.Exec(context.Background(),
		`INSERT INTO
		payments (destination, payment_hash, payment_secret, incoming_amount_msat, outgoing_amount_msat)
		VALUES ($1, $2, $3, $4, $5)`,
		destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat)
	log.Printf("registerPayment(%x, %x, %x, %v, %v) rows: %v err: %v",
		destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat, commandTag.RowsAffected(), err)
	if err != nil {
		return fmt.Errorf("registerPayment(%x, %x, %x, %v, %v) error: %w",
			destination, paymentHash, paymentSecret, incomingAmountMsat, outgoingAmountMsat, err)
	}
	return nil
}
