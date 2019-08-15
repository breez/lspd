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
  *	**FeeRate**: fee rate (recommended: 0.000001).
  * **TimeLockDelta**: the minimum number of blocks this node requires to be added to the expiry of HTLCs (recommended: 144).
3. Compile lspd using `go build .`
4. Define the environment variables as described in sample.env:
5. Run lspd
6. Share with Breez the TOKEN and the LISTEN_ADDRESS you've defined (send to contact@breez.technology)

## Implement your own lspd
You can create your own lsdp by implementing the grpc methods described [here](https://github.com/breez/lspd/blob/master/rpc/lspd.md).
