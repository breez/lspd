#!/bin/bash
protoc -I . lspd.proto --go_out=plugins=grpc:.
