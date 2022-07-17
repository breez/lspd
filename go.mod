module github.com/breez/lspd

go 1.14

require (
	github.com/aws/aws-sdk-go v1.30.20
	github.com/btcsuite/btcd v0.23.1
	github.com/btcsuite/btcd/btcec/v2 v2.2.0
	github.com/btcsuite/btcd/chaincfg/chainhash v1.0.1
	github.com/caddyserver/certmagic v0.11.2
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1
	github.com/golang/protobuf v1.5.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/jackc/pgtype v1.8.1
	github.com/jackc/pgx/v4 v4.13.0
	github.com/lightningnetwork/lightning-onion v1.0.2-0.20220211021909-bb84a1ccb0c5
	github.com/lightningnetwork/lnd v0.15.0-beta
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/grpc v1.38.0
)

replace github.com/lightningnetwork/lnd v0.15.0-beta => github.com/breez/lnd v0.15.0-beta.rc6.0.20220715110145-7f7cfa410adc
