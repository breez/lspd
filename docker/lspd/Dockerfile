ARG USER=lspd
ARG DIR=/data

FROM golang:bookworm AS builder

WORKDIR /app

COPY ../../go.mod ../../go.sum ./

RUN go mod download

COPY ../.. .

RUN make release-lspd


FROM debian:bookworm-slim AS final

ARG USER
ARG DIR

LABEL maintainer="Jesse de Wit (@JssDWt)"

COPY --from=builder /app/lspd /usr/local/bin/

RUN adduser --disabled-password \
            --home "$DIR/" \
            --gecos "" \
            "$USER"

USER $USER

ENV LISTEN_ADDRESS=0.0.0.0:8888

# Grpc port
EXPOSE 8888

ENTRYPOINT ["lspd"]