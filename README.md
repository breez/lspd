# lspd simple server
lspd is a simple deamon that provides [LSP](https://medium.com/breez-technology/introducing-lightning-service-providers-fe9fb1665d5f) services to [Breez clients](https://github.com/breez/breezmobile).   

This is a simple example of an lspd that works with an [lnd](https://github.com/lightningnetwork/lnd) node.

## Installation
1. git clone https://github.com/breez/lspd (or fork)
1. Compile lspd using `go build .`
1. Create a random token (for instance using the command `openssl rand -base64 48`)
1. Define the environment variables as described in sample.env. If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.
1. Run lspd
1. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)

## Implement your own lspd
You can create your own lsdp by implementing the grpc methods described [here](https://github.com/breez/lspd/blob/master/rpc/lspd.md).

## Use a smaller channel reserve
You can apply the PR from https://github.com/lightningnetwork/lnd/pull/2708 to be able to create channels with a channel reserve smaller than 1% of the channel capacity.
Then add the field `RemoteChanReserveSat` in the `lnrpc.OpenChannelRequest` struct when opening a channel.

## Flow for creating channels
When Alice wants Bob to pay her an amount and Alice doesn't have a channel with sufficient capacity, she calls the lspd function RegisterPayment() and sending the paymentHash, paymentSecret (for mpp payments), destination (Alice pubkey), and two amounts.
The first amount (incoming from the lsp point of view) is the amount BOB will pay. The second amount (outgoing from the lsp point of view) is the amount Alice will receive. The difference between these two amounts is the fees for the lsp.
In order to open the channel on the fly, the lsp is connecting to lnd using the interceptor api.

## Probing support
The lsp supports probing non-mpp payments if the payment hash for probing is sha256('probing-01:' || payment_hash) when payment_hash is the hash of the real payment.
