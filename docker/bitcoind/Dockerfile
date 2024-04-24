# $USER name, and data $DIR to be used in the `final` image
ARG USER=bitcoind
ARG DIR=/data
ARG VERSION=v26.1
ARG REPOSITORY=https://github.com/bitcoin/bitcoin.git


FROM debian:bookworm-slim AS downloader

ARG VERSION
ARG REPOSITORY

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        ca-certificates \
        git \
        gnupg

ENV BUILDER_KEYS 982A193E3CE0EED535E09023188CBB2648416AD5 101598DC823C1B5F9A6624ABA5E0907A0380E6C3 9EDAFF80E080659604F4A76B2EBB056FD847F8A7 \
                 ED9BDF7AD6A55E232E84524257FF9BDBCC301009 A8FC55F3B04BA3146F3492E79303B33A305224CB 152812300785C96444D3334D17565732E08E5E41 \
                 0AD83877C1F0CD1EE9BD660AD7CC770B81FD22A8 C060A6635913D98A3587D7DB1C2491FFEB0EF770 590B7292695AFFA5B672CBB2E13FC145CD3F4304 \
                 948444FCE03B05BA5AB0591EC37B1C1D44C786EE E777299FC265DD04793070EB944D35F9AC3DB76A 6B002C6EA3F91B1B0DF0C9BC8F617F1200A6D25C \
                 F4FC70F07310028424EFC20A8E4256593F177720 D1DBF2C4B96F2DEBF4C16654410108112E7EA81F 287AE4CA1187C68C08B49CB2D11BD4F33F1DB499 \
                 616516B8EB6ED02882FC4A7A8ADCB558C4F33D65 71A3B16735405025D447E8F274810B012346C9A6 2F78ACF677029767C8736F13747A7AE2FB0FD25B \
                 133EAC179436F14A5CF1B794860FEB804E669320 9ED99C7A355AE46098103E74476E74C8529A9006 6A8F9C266528E25AEB1D7731C2371D91CB716EA7 \
                 28E72909F1717FE9607754F8A7BEB2621678D37D 67AA5B46E7AF78053167FE343B8F814A784218F8 79D00BAC68B56D422F945A8F8E3A8F3247DBCBBF \
                 C388F6961FB972A95678E327F62711DBDCA8AE56 9DEAE0DC7063249FB05474681E4AED62986CD25D

RUN gpg --keyserver keyserver.ubuntu.com --recv-keys $BUILDER_KEYS
RUN git clone -b "$VERSION" --depth=1 "$REPOSITORY" "/bitcoin/"
RUN cd "/bitcoin/" && \
    git verify-tag "$VERSION"


FROM debian:bookworm-slim AS builder

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        build-essential \
        libtool \
        autotools-dev \
        automake \
        pkg-config \
        bsdmainutils \
        python3 \
        libevent-dev \
        libboost-dev \
        libsqlite3-dev \
        libzmq3-dev

WORKDIR /bitcoin/
COPY  --from=downloader /bitcoin/  ./

RUN ./autogen.sh

RUN ./configure \
    --prefix="/opt/bitcoin" \
    --disable-ccache \
    --disable-tests \
    --without-gui \
    --with-sqlite=yes

RUN make -j$(nproc) && \
    make install


FROM debian:bookworm-slim AS final

ARG USER
ARG DIR

LABEL maintainer="Jesse de Wit (@JssDWt)"

RUN apt-get update -qq && \
    apt-get install -qq -y --no-install-recommends \
        libevent-2.1-7 \
        libevent-pthreads-2.1-7 \
        libsqlite3-0 \
        libzmq5

COPY --from=builder /opt/bitcoin/ /opt/bitcoin/

RUN adduser --disabled-password \
            --home "$DIR/" \
            --gecos "" \
            "$USER"

USER $USER

# Prevents `VOLUME $DIR/.bitcoind/` being created as owned by `root`
RUN mkdir -p "$DIR/.bitcoin/"

# Expose volume containing all `bitcoind` data
VOLUME $DIR/.bitcoin/

# REST interface
EXPOSE 8080

# P2P network (mainnet, testnet & regtest respectively)
EXPOSE 8333 18333 18444

# RPC interface (mainnet, testnet & regtest respectively)
EXPOSE 8332 18332 18443

# ZMQ ports (for transactions & blocks respectively)
EXPOSE 28332 28333

ENTRYPOINT ["/opt/bitcoin/bin/bitcoind"]

CMD ["-zmqpubrawblock=tcp://0.0.0.0:28332", "-zmqpubrawtx=tcp://0.0.0.0:28333"]