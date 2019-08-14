# lspd simple server
This sample server works with a lnd node
## Installation
1. git clone https://github.com/breez/lspd (or fork)
1. Modify the code in server.go if you use different values than the default when opening channels
1. Compile lspd using `go build .`
1. Define the environment variables as described in sample.env
1. Run lspd
1. Share with breez the TOKEN and the LISTEN_ADDRESS you defined
## Implement your own server
The grpc methods are described in https://github.com/breez/lspd/blob/master/rpc/lspd.md
