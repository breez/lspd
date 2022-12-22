# lspd simple server
lspd is a simple deamon that provides [LSP](https://medium.com/breez-technology/introducing-lightning-service-providers-fe9fb1665d5f) services to [Breez clients](https://github.com/breez/breezmobile).   

This is a simple example of an lspd that works with an [lnd](https://github.com/lightningnetwork/lnd) node or a [cln](https://github.com/ElementsProject/lightning) node.

## Installation
### Build
1. git clone https://github.com/breez/lspd (or fork)
1. Compile lspd using `go build .`

### Before running
1. Create a random token (for instance using the command `openssl rand -base64 48`)
1. Define the environment variables as described in sample.env. If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.

### Running lspd on LND
1. Run LND with the following options set:
    - `--protocol.zero-conf`: for being able to open zero conf channels
	- `--protocol.option-scid-alias`: required for zero conf channels
	- `--requireinterceptor`: to make sure all htlcs are intercepted by lspd
    - `--bitcoin.chanreservescript="0"` to allow the client to have zero reserve on their side
1. Run lspd

### Running lspd on CLN
lspd on core lightning is run as a plugin. In order to load the environment veriables in the plugin runtime, they either have to be set on the machine, or the plugin should be started with a shell script:

```bash
#!/bin/bash

export TOKEN=<TOKEN>
export LSPD_PRIVATE_KEY=<LSPD PRIVATE KEY>
export DATABASE_URL=<DATABASE_URL>
export RUN_CLN=true

# Etc. for all env variables in sample.env

/path/to/lspd
```

Run cln with the following options set:
- `--plugin=/path/to/shell/script.sh`: to use lspd as plugin
- `--max-concurrent-htlcs=30`: In order to use zero reserve channels on the client side, (local max_accepted_htlcs + remote max_accepted_htlcs + 2) * dust limit must be lower than the channel capacity. Reduce max-concurrent-htlcs or increase channel capacity accordingly.
- `--dev-allowdustreserve=true`: In order to allow zero reserve on the client side, you'll need to enable developer mode on cln (`./configure --enable-developer`)

### Final step
1. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)

## Implement your own lspd
You can create your own lsdp by implementing the grpc methods described [here](https://github.com/breez/lspd/blob/master/rpc/lspd.md).

## Flow for creating channels
When Alice wants Bob to pay her an amount and Alice doesn't have a channel with sufficient capacity, she calls the lspd function RegisterPayment() and sending the paymentHash, paymentSecret (for mpp payments), destination (Alice pubkey), and two amounts.
The first amount (incoming from the lsp point of view) is the amount BOB will pay. The second amount (outgoing from the lsp point of view) is the amount Alice will receive. The difference between these two amounts is the fees for the lsp.
In order to open the channel on the fly, the lsp is connecting to lnd using the interceptor api.

## Probing support
The lsp supports probing non-mpp payments if the payment hash for probing is sha256('probing-01:' || payment_hash) when payment_hash is the hash of the real payment.

## Integration tests
In order to run the integration tests, you need:
- Docker running
- python3 installed
- A development build of lightningd v22.11
- lnd v0.15.4 lsp version https://github.com/breez/lnd/commit/6ee6b89e3a4e3f2776643a75a9100d13a2090725
- lnd v0.15.3 breez client version https://github.com/breez/lnd/commit/e1570b327b5de52d03817ad516d0bdfa71797c64
- bitcoind (tested with v23.0)
- bitcoin-cli (tested with v23.0)
- build of lspd

To run the integration tests, run the following command from the lspd root directory (replacing the appropriate paths). 

```
go test -v ./itest \
  --lightningdexec /full/path/to/lightningd \
  --lndexec /full/path/to/lnd \
  --lndmobileexec /full/path/to/lnd \
  --lspdexec /full/path/to/lspd \
  --lspdmigrationsdir /full/path/to/lspd/postgresql/migrations
```

- Required: `--lightningdexec` Full path to lightningd development build executable. Defaults to `lightningd` in `$PATH`.
- Required: `--lndexec` Full path to LSP LND executable. Defaults to `lnd` in `$PATH`. 
- Required: `--lndmobileexec` Full path to Breez mobile client LND executable. No default.
- Required: `--lspdexec` Full path to `lspd` executable to test. Defaults to `lspd` in `$PATH`.
- Required: `--lspdmigrationsdir` Path to directory containing postgres migrations for lspd. (Should be `./postgresql/migrations`)
- Recommended: `--bitcoindexec` Full path to `bitcoind`. Defaults to `bitcoind` in `$PATH`.
- Recommended: `--bitcoincliexec` Full path to `bitcoin-cli`. Defaults to `bitcoin-cli` in `$PATH`.
- Recommended: `--testdir` uses the testdir as root directory for test files. Recommended because the CLN `lightning-rpc` socket max path length is 104-108 characters. Defaults to a temp directory (which has a long path length usually).
- Optional: `--preservelogs` persists only the logs in the testing directory.
- Optional: `--preservestate` preserves all artifacts from the lightning nodes, miners, postgres container and startup scripts.
- Optional: `--dumplogs` dumps all logs to the console after a test is complete.
