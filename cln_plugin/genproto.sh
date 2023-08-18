#!/bin/bash
SCRIPTDIR=$(dirname $0)
PROTO_ROOT=$SCRIPTDIR/proto

protoc --go_out=$PROTO_ROOT --go_opt=paths=source_relative --go-grpc_out=$PROTO_ROOT --go-grpc_opt=paths=source_relative -I=$PROTO_ROOT $PROTO_ROOT/*.proto 