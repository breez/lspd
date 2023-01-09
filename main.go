package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/btcsuite/btcd/btcec/v2"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "genkey" {
		p, err := btcec.NewPrivateKey()
		if err != nil {
			log.Fatalf("btcec.NewPrivateKey() error: %v", err)
		}
		fmt.Printf("LSPD_PRIVATE_KEY=\"%x\"\n", p.Serialize())
		return
	}

	n := os.Getenv("NODES")
	var nodes []*NodeConfig
	err := json.Unmarshal([]byte(n), &nodes)
	if err != nil {
		log.Fatalf("failed to unmarshal NODES env: %v", err)
	}

	if len(nodes) == 0 {
		log.Fatalf("need at least one node configured in NODES.")
	}

	var interceptors []HtlcInterceptor
	for _, node := range nodes {
		var interceptor HtlcInterceptor
		if node.Lnd != nil {
			interceptor, err = NewLndHtlcInterceptor(node)
			if err != nil {
				log.Fatalf("failed to initialize LND interceptor: %v", err)
			}
		}

		if node.Cln != nil {
			interceptor, err = NewClnHtlcInterceptor(node)
			if err != nil {
				log.Fatalf("failed to initialize CLN interceptor: %v", err)
			}
		}

		if interceptor == nil {
			log.Fatalf("node has to be either cln or lnd")
		}

		interceptors = append(interceptors, interceptor)
	}

	address := os.Getenv("LISTEN_ADDRESS")
	certMagicDomain := os.Getenv("CERTMAGIC_DOMAIN")
	s, err := NewGrpcServer(nodes, address, certMagicDomain)
	if err != nil {
		log.Fatalf("failed to initialize grpc server: %v", err)
	}

	databaseUrl := os.Getenv("DATABASE_URL")
	err = pgConnect(databaseUrl)
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(len(interceptors) + 1)

	stopInterceptors := func() {
		for _, interceptor := range interceptors {
			interceptor.Stop()
		}
	}

	for _, interceptor := range interceptors {
		i := interceptor
		go func() {
			err := i.Start()
			if err == nil {
				log.Printf("Interceptor stopped.")
			} else {
				log.Printf("FATAL. Interceptor stopped with error: %v", err)
			}

			wg.Done()

			// If any interceptor stops, stop everything, so we're able to restart using systemd.
			s.Stop()
			stopInterceptors()
		}()
	}

	go func() {
		err := s.Start()
		if err == nil {
			log.Printf("GRPC server stopped.")
		} else {
			log.Printf("FATAL. GRPC server stopped with error: %v", err)
		}

		wg.Done()

		// If the server stops, stop everything else, so we're able to restart using systemd.
		stopInterceptors()
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-c
		log.Printf("Received stop signal %v. Stopping.", sig)

		// Stop everything gracefully on stop signal
		s.Stop()
		stopInterceptors()
	}()

	wg.Wait()
	log.Printf("lspd exited")
}
