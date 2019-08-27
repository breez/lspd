# lspd simple server
lspd is a simple deamon that provides [LSP](https://medium.com/breez-technology/introducing-lightning-service-providers-fe9fb1665d5f) services to [Breez clients](https://github.com/breez/breezmobile).   

This is a simple example of an lspd that works with an [lnd](https://github.com/lightningnetwork/lnd) node.

## Installation
1. git clone https://github.com/breez/lspd (or fork)
2. Modify the code in server.go if you use different values than the recommeded values when opening channels:
  * **ChannelCapacity**: channel capacity is sats, defined in the channelAmount const (recommended: 1000000).
  *	**TargetConf**: the number of blocks that the funding transaction *should* confirm in, will be used for fee estimation (recommended: 0).
  *	**MinHtlcMsat**: the channel_reserve value in sats (recommended: 1000000).
  *	**BaseFeeMsat**: base tx fee in msats (recommended: 1000).
  *	**FeeRate**: fee rate (recommended: 0.000001). The total fee charged is BaseFeeMsat + (amount * FeeRate / 1000000)
  * **TimeLockDelta**: the minimum number of blocks this node requires to be added to the expiry of HTLCs (recommended: 144).
3. Compile lspd using `go build .`
4. Create a random token (for instance using the command `openssl rand -base64 48`)
5. Define the environment variables as described in sample.env. If `CERTMAGIC_DOMAIN` is defined, certificate for this domain is automatically obtained and renewed from Let's Encrypt. In this case, the port needs to be 443. If `CERTMAGIC_DOMAIN` is not defined, lspd needs to run behind a reverse proxy like treafik or nginx.
6. Run lspd
7. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)

## Implement your own lspd
You can create your own lsdp by implementing the grpc methods described [here](https://github.com/breez/lspd/blob/master/rpc/lspd.md).

## Use a smaller channel reserve
You can apply the PR from https://github.com/lightningnetwork/lnd/pull/2708 to be able to create channels with a channel reserve smaller than 1% of the channel capacity.
Then add the field `RemoteChanReserveSat` in the `lnrpc.OpenChannelRequest` struct when opening a channel.
