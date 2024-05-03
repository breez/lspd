# $USER name, and data $DIR to be used in the `final` image
ARG USER=cln
ARG DIR=/data
ARG VERSION=v24.02.2
ARG REPOSITORY=https://github.com/ElementsProject/lightning.git


FROM debian:bookworm-slim as downloader

ARG VERSION
ARG REPOSITORY

WORKDIR /source/

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        ca-certificates \
        git \
        gnupg

# Christian Decker
ENV BUILDER_KEYS B731AAC521B013859313F674A26D6D9FE088ED58

RUN gpg --keyserver keyserver.ubuntu.com --recv-keys $BUILDER_KEYS
RUN git clone -b "$VERSION" --depth=1 "$REPOSITORY" ./
# RUN cd "/lightning/" && \
#     git verify-tag "$VERSION"


FROM debian:bookworm-slim as builder

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        curl \
        ca-certificates \
        jq \
        autoconf \
        automake \
        build-essential \
        git \
        libtool \
        libsqlite3-dev \
        python3 \
        python3-mako \
        python3-pip \
        net-tools \
        zlib1g-dev \
        libsodium-dev \
        gettext \
        protobuf-compiler

RUN pip3 install --upgrade pip --break-system-packages && \
    pip3 install grpcio-tools --break-system-packages

USER root
ENV RUST_PROFILE=release
ENV PATH=$PATH:/root/.cargo/bin/
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
RUN rustup toolchain install stable \
    --component rustfmt \
    --allow-downgrade \
    --profile minimal

WORKDIR /app/
COPY  --from=downloader /source/  ./
COPY --from=bitcoind /usr/local/bin/bitcoin-cli /usr/local/bin/

RUN ./configure \
        --prefix="/opt/lightning" \
        --enable-static && \
    make && \
    make install


FROM debian:bookworm-slim AS final

ARG USER
ARG DIR

LABEL maintainer="Jesse de Wit (@JssDWt)"

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        libevent-2.1-7 \
        python3

COPY --from=bitcoind /usr/local/bin/bitcoin-cli /usr/local/bin/
COPY --from=builder /opt/lightning/ /usr/local/

RUN adduser --disabled-password \
            --home "$DIR/" \
            --gecos "" \
            "$USER"

USER $USER

# Prevents `VOLUME $DIR/.lightning/` being created as owned by `root`
RUN mkdir -p "$DIR/.lightning/"

# Expose volume containing all `lightningd` data
VOLUME $DIR/.lightning/

# Lightning P2P port
EXPOSE 9735

ENTRYPOINT ["lightningd"]