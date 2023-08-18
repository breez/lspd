
## Installation instructions for lnd and lspd
### Requirements 
- lnd
- lspd
- postgresql

### Installation
#### LND
Follow LND installation instructions [here](https://github.com/lightningnetwork/lnd/blob/master/docs/INSTALL.md). 

#### lspd
Needs to be build from source:
```
git clone https://github.com/breez/lspd 
cd lspd
go build .                      # compile lspd
go build -o lspd_plugin         # compile lspd cln plugin
```
### Postgresql
Lspd supports postgresql backend. Create new database and user for lspd:
```
CREATE ROLE <username>;
ALTER ROLE <username> WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS PASSWORD '<password>';
CREATE DATABASE <dbname> WITH TEMPLATE = template0 ENCODING = 'UTF8' LC_COLLATE = 'en_US.UTF-8' LC_CTYPE = 'en_US.UTF-8';
ALTER DATABASE <dbname> OWNER TO <username>;
``````


### Configure
1. Create a random token (for instance using the command `openssl rand -base64 48`, or `./lspd genkey`)
1. Define the environment variables as described in [sample.env](./sample.env). If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.

ENV variables:
- `LISTEN_ADDRESS` defines the host:port for the lspd grpc server
- `CERTMAGIC_DOMAIN` domain on which lspd will be accessible
- `DATABASE_URL` postgresql db url
- `AWS_REGION`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `MEMPOOL_API_BASE_URL` uses fee estimation for opening new channels (default: https://mempool.space)
- `MEMPOOL_PRIORITY` priority with which open new channels using mempool api (default: economy)

### Running lspd on LND
1. Run LND with the following options set:
   - `--protocol.zero-conf`: for being able to open zero conf channels
   - `--protocol.option-scid-alias`: required for zero conf channels
	- `--requireinterceptor`: to make sure all htlcs are intercepted by lspd
   - `--bitcoin.chanreservescript="0"` to allow the client to have zero reserve on their side
1. Run lspd

### Final step
1. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)
