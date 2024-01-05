# lspd
lspd is a simple deamon that provides [LSP](https://medium.com/breez-technology/introducing-lightning-service-providers-fe9fb1665d5f) services to Breez clients. 

This is a simple example of an lspd that works with an [lnd](https://github.com/lightningnetwork/lnd) node or a [cln](https://github.com/ElementsProject/lightning) node.

[Breez SDK](https://github.com/breez/breez-sdk) uses cln lsdp and [Breez mobile](https://github.com/breez/breezmobile) uses lnd lspd.

## Deployment
Installation and configuration instructions for both implementations can be found here:
### Manual install 
- [CLN](./docs/CLN.md) - step by step installation instructions for CLN
- [LND](./docs/LND.md) - step by step installation instructions for LND
### Automated deployment
- [AWS](./docs/aws.md) - automated deployment of bitcoind, CLN and lspd to AWS, together with 
- [Bash](./docs/bash.md) - install everything on any debian/ubuntu server

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
- A development build of lightningd v23.11
- lnd v0.17.2 lsp version https://github.com/breez/lnd/commit/0c939786ced78a981bd77c7da628bfcf86ada568
- lnd v0.16.4 breez client version https://github.com/breez/lnd/commit/3c0854adcfc924a6d759a6ee4640c41266b9f8b4
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

It may be a good idea to clean your testdir every once in a while if you're using the `preservelogs` or `preservestate` flags.
