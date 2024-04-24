ARG USER=lspd

FROM golang:bookworm AS builder

WORKDIR /lspd/

RUN mkdir -p bin

COPY ../.. .

RUN OUT_DIR=bin make release-all


FROM debian:bookworm-slim AS final

ARG USER

LABEL maintainer="Jesse de Wit (@JssDWt)"

COPY --from=builder /lspd/bin/ /usr/local/bin/

RUN adduser --disabled-password \
            --home "$DIR/" \
            --gecos "" \
            "$USER"

USER $USER

ENV LISTEN_ADDRESS=0.0.0.0:8888

# Grpc port
EXPOSE 8888

ENTRYPOINT ["lspd"]