module github.com/breez/lspd

go 1.14

require (
	github.com/aws/aws-sdk-go v1.30.20
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/caddyserver/certmagic v0.11.2
	github.com/golang/protobuf v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0
	github.com/jackc/pgtype v1.4.2
	github.com/jackc/pgx/v4 v4.8.1
	github.com/lightningnetwork/lightning-onion v1.0.1
	github.com/lightningnetwork/lnd v0.10.0-beta
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	google.golang.org/grpc v1.31.0
)

replace github.com/lightningnetwork/lnd v0.10.0-beta => github.com/breez/lnd v0.10.0-beta.rc6.0.20200727142715-f67a1052c0e0
