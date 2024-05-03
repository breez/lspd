docker-all: docker-bitcoind docker-lightningd docker-lspd docker-lightningd-lsp

docker-bitcoind:
	docker build -t bitcoind -f bitcoind/Dockerfile .

docker-lightningd: docker-bitcoind
	docker build -t lightningd -f lightningd/Dockerfile .

docker-lspd:
	docker build -t lspd -f lspd/Dockerfile ..

docker-lightningd-lsp: docker-lightningd
	docker build -t lightningd-lsp -f lightningd-lsp/Dockerfile ..
