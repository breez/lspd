FROM golang:bookworm AS builder

WORKDIR /app

COPY ../../go.mod ../../go.sum ./

RUN go mod download

COPY ../.. .

RUN make release-plugin


FROM lightningd AS final

COPY --from=builder /app/lspd_cln_plugin /usr/local/bin/

# lightningd grpc port
EXPOSE 8888

# cln_plugin grpc port
EXPOSE 12312

CMD [ \
    "--grpc-port=8888", \
    "--plugin=lspd_cln_plugin", \
    "--lsp-listen=0.0.0.0:12312", \
    "--developer", \
    "--dev-allowdustreserve=true" \
]