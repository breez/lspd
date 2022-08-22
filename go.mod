module github.com/breez/lspd

go 1.18

require (
	github.com/aws/aws-sdk-go v1.30.20
	github.com/btcsuite/btcd v0.20.1-beta.0.20200730232343-1db1b6f8217f
	github.com/caddyserver/certmagic v0.11.2
	github.com/ecies/go/v2 v2.0.4
	github.com/golang/protobuf v1.4.3
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0
	github.com/jackc/pgtype v1.4.2
	github.com/jackc/pgx/v4 v4.8.1
	github.com/lightningnetwork/lightning-onion v1.0.2-0.20200501022730-3c8c8d0b89ea
	github.com/lightningnetwork/lnd v0.11.0-beta
	github.com/niftynei/glightning v0.8.2
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	google.golang.org/grpc v1.29.1
)

require (
	github.com/aead/chacha20 v0.0.0-20180709150244-8b13a72661da // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f // indirect
	github.com/btcsuite/btcutil v1.0.2 // indirect
	github.com/btcsuite/btcutil/psbt v1.0.2 // indirect
	github.com/btcsuite/btcwallet v0.11.1-0.20200814001439-1d31f4ea6fc5 // indirect
	github.com/btcsuite/btcwallet/wallet/txauthor v1.0.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.0.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.0.0 // indirect
	github.com/btcsuite/btcwallet/walletdb v1.3.3 // indirect
	github.com/btcsuite/btcwallet/wtxmgr v1.2.0 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cenkalti/backoff/v4 v4.0.0 // indirect
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/etcd v3.3.25+incompatible // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/decred/dcrd/lru v1.0.0 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/ethereum/go-ethereum v1.10.17 // indirect
	github.com/go-acme/lego/v3 v3.7.0 // indirect
	github.com/go-errors/errors v1.0.1 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang-migrate/migrate v3.5.4+incompatible // indirect
	github.com/google/btree v1.0.0 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.14.3 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgconn v1.6.4 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.0.2 // indirect
	github.com/jackc/pgservicefile v0.0.0-20200714003250-2b9c44734f2b // indirect
	github.com/jackc/puddle v1.1.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jonboulle/clockwork v0.1.0 // indirect
	github.com/jrick/logrotate v1.0.0 // indirect
	github.com/json-iterator/go v1.1.9 // indirect
	github.com/juju/loggo v0.0.0-20190526231331-6e530bcce5d8 // indirect
	github.com/kkdai/bstream v0.0.0-20181106074824-b3251f7901ec // indirect
	github.com/klauspost/cpuid v1.2.3 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino v0.11.1-0.20200316235139-bffc52e8f200 // indirect
	github.com/lightningnetwork/lnd/clock v1.0.1 // indirect
	github.com/lightningnetwork/lnd/queue v1.0.4 // indirect
	github.com/lightningnetwork/lnd/ticker v1.0.0 // indirect
	github.com/ltcsuite/ltcd v0.0.0-20190101042124-f37f8bf35796 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/prometheus/client_golang v1.1.0 // indirect
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4 // indirect
	github.com/prometheus/common v0.6.0 // indirect
	github.com/prometheus/procfs v0.0.3 // indirect
	github.com/rogpeppe/fastuuid v1.2.0 // indirect
	github.com/sirupsen/logrus v1.4.2 // indirect
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.3.5-0.20200615073812-232d8fc87f50 // indirect
	go.uber.org/atomic v1.6.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.14.1 // indirect
	golang.org/x/crypto v0.0.0-20220507011949-2cf3adece122 // indirect
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2 // indirect
	golang.org/x/sys v0.0.0-20210816183151-1e6c022a8912 // indirect
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/genproto v0.0.0-20200305110556-506484158171 // indirect
	google.golang.org/protobuf v1.23.0 // indirect
	gopkg.in/errgo.v1 v1.0.1 // indirect
	gopkg.in/macaroon-bakery.v2 v2.0.1 // indirect
	gopkg.in/macaroon.v2 v2.0.0 // indirect
	gopkg.in/square/go-jose.v2 v2.3.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.1.0 // indirect
)

replace github.com/lightningnetwork/lnd v0.11.0-beta => github.com/breez/lnd v0.11.0-beta.rc4.0.20210125150416-0c10146b223c

replace github.com/niftynei/glightning v0.8.2 => github.com/breez/glightning v0.0.0-20220821180527-63dbb1a02c1e
