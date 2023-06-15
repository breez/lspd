# lspd simple server
lspd is a simple deamon that provides [LSP](https://medium.com/breez-technology/introducing-lightning-service-providers-fe9fb1665d5f) services to [Breez clients](https://github.com/breez/breezmobile).   

This is a simple example of an lspd that works with an [lnd](https://github.com/lightningnetwork/lnd) node or a [cln](https://github.com/ElementsProject/lightning) node.

## Installation
### Build
1. git clone https://github.com/breez/lspd (or fork)
1. Compile lspd using `go build .`

### Before running
1. Create a random token (for instance using the command `openssl rand -base64 48`, or `./lspd genkey`)
1. Define the environment variables as described in sample.env. If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.

### Running lspd on LND
1. Run LND with the following options set:
   - `--protocol.zero-conf`: for being able to open zero conf channels
   - `--protocol.option-scid-alias`: required for zero conf channels
	- `--requireinterceptor`: to make sure all htlcs are intercepted by lspd
   - `--bitcoin.chanreservescript="0"` to allow the client to have zero reserve on their side
1. Run lspd

### Running lspd on CLN
In order to run lspd on top of CLN, you need to run the lspd process and run cln with the provided cln plugin.

The cln plugin (go build -o lspd_plugin cln_plugin/cmd) is best started with a bash script to pass environment variables (note this LISTEN_ADDRESS is the listen address for communication between lspd and the plugin, this is not the listen address mentioned in the 'final step')

```bash
#!/bin/bash

export LISTEN_ADDRESS=<listen address>
/path/to/lspd_plugin
```

1. Run cln with the following options set:
    - `--plugin=/path/to/shell/script.sh`: to use lspd as plugin
    - `--max-concurrent-htlcs=30`: In order to use zero reserve channels on the client side, (local max_accepted_htlcs + remote max_accepted_htlcs + 2) * dust limit must be lower than the channel capacity. Reduce max-concurrent-htlcs or increase channel capacity accordingly.
    - `--dev-allowdustreserve=true`: In order to allow zero reserve on the client side, you'll need to enable developer mode on cln (`./configure --enable-developer`)
1. Run lspd

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
- A development build of lightningd v23.05.1
- lnd v0.16.2 lsp version https://github.com/breez/lnd/commit/cebcdf1b17fdedf7d69207d98c31cf8c3b257531
- lnd v0.16.2 breez client version https://github.com/breez/lnd/commit/9d744cd396af707d77473d58c97947b8e0a25d08
- bitcoind (tested with v23.0)
- bitcoin-cli (tested with v23.0)
- build of lspd (go build .)
- build of lspd cln plugin (go build -o lspd_plugin cln_plugin/cmd)

To run the integration tests, run the following command from the lspd root directory (replacing the appropriate paths). 

```
go test -timeout 20m -v ./itest \
  --lightningdexec /full/path/to/lightningd \
  --lndexec /full/path/to/lnd \
  --lndmobileexec /full/path/to/lnd \
  --clnpluginexec /full/path/to/lspd_plugin \
  --lspdexec /full/path/to/lspd \
  --lspdmigrationsdir /full/path/to/lspd/postgresql/migrations
```

- Required: `--lightningdexec` Full path to lightningd development build executable. Defaults to `lightningd` in `$PATH`.
- Required: `--lndexec` Full path to LSP LND executable. Defaults to `lnd` in `$PATH`. 
- Required: `--lndmobileexec` Full path to Breez mobile client LND executable. No default.
- Required: `--lspdexec` Full path to `lspd` executable to test. Defaults to `lspd` in `$PATH`.
- Required: `--clnpluginexec` Full path to the lspd cln plugin executable. No default.
- Required: `--lspdmigrationsdir` Path to directory containing postgres migrations for lspd. (Should be `./postgresql/migrations`)
- Recommended: `--bitcoindexec` Full path to `bitcoind`. Defaults to `bitcoind` in `$PATH`.
- Recommended: `--bitcoincliexec` Full path to `bitcoin-cli`. Defaults to `bitcoin-cli` in `$PATH`.
- Recommended: `--testdir` uses the testdir as root directory for test files. Recommended because the CLN `lightning-rpc` socket max path length is 104-108 characters. Defaults to a temp directory (which has a long path length usually).
- Optional: `--preservelogs` persists only the logs in the testing directory.
- Optional: `--preservestate` preserves all artifacts from the lightning nodes, miners, postgres container and startup scripts.
- Optional: `--dumplogs` dumps all logs to the console after a test is complete.

Unfortunately the tests cannot be cancelled with CTRL+C without having to clean 
up some artefacts. Here's where to look:
- lspd process
- lightningd process
- lnd process
- bitcoind process
- docker container for postgres with default name

It may be a good idea to clean your testdir every once in a while if you're 
using the `preservelogs` or `preservestate` flags.