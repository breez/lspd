FROM golang:1.22.2

WORKDIR /lspd
COPY ../.. .

RUN mkdir -p bin && OUT_DIR=./bin make release-all

ENTRYPOINT ["./lspd", "@"]
