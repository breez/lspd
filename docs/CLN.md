
## Installation instructions for core lightning and lspd
### Requirements 
- CLN
- lspd
- lspd plugin for cln
- postgresql

### Installation
#### CLN 
Download a precompiled binary of CLN [here](https://github.com/ElementsProject/lightning/releases).

Or compile CLN yourself as described in the [installation documentation](https://github.com/ElementsProject/lightning/doc/getting-started/getting-started/installation.md).

#### lspd
Needs to be built from source:
```
git clone https://github.com/breez/lspd 
cd lspd
make release-all
```
This will create two binaries, `lspd` and `lspd_cln_plugin`

#### Postgresql
Lspd supports postgresql backend. To create database and new role to access it on your postgres server use:
##### Postgresql server
```
CREATE ROLE <username>;
ALTER ROLE <username> WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS PASSWORD '<password>';
CREATE DATABASE <dbname> WITH TEMPLATE = template0 ENCODING = 'UTF8' LC_COLLATE = 'en_US.UTF-8' LC_CTYPE = 'en_US.UTF-8';
ALTER DATABASE <dbname> OWNER TO <username>;
```
##### RDS on AWS
```
CREATE ROLE <username>;
ALTER ROLE <username> WITH INHERIT NOCREATEROLE NOCREATEDB LOGIN NOBYPASSRLS PASSWORD '<password>';
CREATE DATABASE <dbname> WITH TEMPLATE = template0 ENCODING = 'UTF8' LC_COLLATE = 'en_US.UTF-8' LC_CTYPE = 'en_US.UTF-8';
ALTER DATABASE <dbname> OWNER TO <username>;
```

### Configuration
1. Create a random token (for instance using the command `openssl rand -base64 48`, or `./lspd genkey`)
1. Define the environment variables as described in [sample.env](../sample.env). If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.

ENV variables:
- `LISTEN_ADDRESS` defines the host:port for the lspd grpc server
- `CERTMAGIC_DOMAIN` domain on which lspd will be accessible
- `DATABASE_URL` postgresql db url
- `AWS_REGION` AWS region for SES emailing 
- `AWS_ACCESS_KEY_ID` API key for SES emailing 
- `AWS_SECRET_ACCESS_KEY`API secret for SES emailing 
- `MEMPOOL_API_BASE_URL` uses fee estimation for opening new channels (default: https://mempool.space/api/v1/)
- `MEMPOOL_PRIORITY` priority with which open new channels using mempool api 
  (options: minimum, economy, hour, halfhour, fastest) (default: economy)
- `NODES` which nodes are used by lspd (see below for example, multiple nodes supported and more examples can be found in [sample.env](../sample.env))

Example of NODES variable:
```
NODES='[ { "name": "${LSPName}", "nodePubkey": "$PUBKEY", "lspdPrivateKey": "$LSPD_PRIVATE_KEY", "tokens": ["$TOKEN"], "host": "$EXTERNAL_IP:9735", "targetConf": "6", "minConfs": "6", "minHtlcMsat": "600", "baseFeeMsat": "1000", "feeRate": "0.000001", "timeLockDelta": "144", "channelFeePermyriad": "40", "channelMinimumFeeMsat": "2000000", "additionalChannelCapacity": "100000", "maxInactiveDuration": "3888000",  "cln": { "pluginAddress": "127.0.0.1:12312", "socketPath": "/home/lightning/.lightning/bitcoin/lightning-rpc" } } ]'
```

### Running lspd on CLN
In order to run lspd on top of CLN, you need to run the lspd process and run cln with the provided cln plugin.

1. Run cln with the following options set:
    - `--developer`: to allow passing the `--dev-allowdustreserve` flag
    - `--plugin=/path/to/lspd_cln_plugin`: to use lspd as plugin
    - `--max-concurrent-htlcs=30`: In order to use zero reserve channels on the client side, (local max_accepted_htlcs + remote max_accepted_htlcs + 2) * dust limit must be lower than the channel capacity. Reduce max-concurrent-htlcs or increase channel capacity accordingly.
    - `--dev-allowdustreserve=true`: In order to allow zero reserve on the client side (requires developer mode turned on)
    - `--lsp-listen=127.0.0.1:<port>`: Set on which port the lspd_cln_plugin will listen for lspd communication, must be the same port that is used in pluginAddress parameter in NODES env variable.
1. Run lspd

### Final step
1. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)