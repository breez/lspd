SCRIPTDIR=$(dirname $0)
PROTO_ROOT=$SCRIPTDIR/proto

protoc --go_out=$SCRIPTDIR --go_opt=paths=source_relative --go-grpc_out=$SCRIPTDIR --go-grpc_opt=paths=source_relative -I=$PROTO_ROOT $PROTO_ROOT/* 