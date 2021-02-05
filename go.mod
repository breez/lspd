module github.com/breez/lspd

go 1.14

require (
	github.com/aws/aws-sdk-go v1.30.20
	github.com/btcsuite/btcd v0.20.1-beta.0.20200730232343-1db1b6f8217f
	github.com/caddyserver/certmagic v0.11.2
	github.com/golang/protobuf v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0
	github.com/jackc/pgtype v1.4.2
	github.com/jackc/pgx/v4 v4.8.1
	github.com/lightningnetwork/lightning-onion v1.0.2-0.20200501022730-3c8c8d0b89ea
	github.com/lightningnetwork/lnd v0.11.0-beta
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	google.golang.org/grpc v1.31.0
)

replace github.com/lightningnetwork/lnd v0.11.0-beta => github.com/breez/lnd v0.11.0-beta.rc4.0.20210125150416-0c10146b223c
