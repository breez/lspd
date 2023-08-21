
## Installation instructions for core lightning and lspd
### Requirements 
- CLN (compiled with developer mode on)
- lspd
- lspd plugin for cln
- postgresql

### Installation
#### CLN 
Follow compilation steps for CLN [here](https://github.com/ElementsProject/lightning/blob/master/doc/getting-started/getting-started/installation.md) to enable developer mode.

#### lspd
Needs to be built from source:
```
git clone https://github.com/breez/lspd 
cd lspd
go build .                                # compile lspd
go build -o lspd_plugin ./cln_plugin/cmd  # compile lspd cln plugin
```

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
1. Define the environment variables as described in [sample.env](./sample.env). If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.

ENV variables:
- `LISTEN_ADDRESS` defines the host:port for the lspd grpc server
- `CERTMAGIC_DOMAIN` domain on which lspd will be accessible
- `DATABASE_URL` postgresql db url
- `AWS_REGION` AWS region for SES emailing 
- `AWS_ACCESS_KEY_ID` API key for SES emailing 
- `AWS_SECRET_ACCESS_KEY`API secret  for SES emailing 
- `MEMPOOL_API_BASE_URL` uses fee estimation for opening new channels (default: https://mempool.space/api/v1/)
- `MEMPOOL_PRIORITY` priority with which open new channels using mempool api (default: economy)



### Running lspd on CLN
In order to run lspd on top of CLN, you need to run the lspd process and run cln with the provided cln plugin. You also need lightningd compiled with developer mode on (`./configure --enable-developer`)

The cln plugin (go build -o lspd_plugin cln_plugin/cmd) is best started with a bash script to pass environment variables (note this LISTEN_ADDRESS is the listen address for communication between lspd and the plugin, this is not the listen address mentioned in the 'final step')

```bash
#!/bin/bash

export LISTEN_ADDRESS=<listen address>
/path/to/lspd_plugin
```

1. Run cln with the following options set:
    - `--plugin=/path/to/shell/script.sh`: to use lspd as plugin
    - `--max-concurrent-htlcs=30`: In order to use zero reserve channels on the client side, (local max_accepted_htlcs + remote max_accepted_htlcs + 2) * dust limit must be lower than the channel capacity. Reduce max-concurrent-htlcs or increase channel capacity accordingly.
    - `--dev-allowdustreserve=true`: In order to allow zero reserve on the client side (requires developer mode turned on)
1. Run lspd

### Final step
1. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)