package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/cln"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lnd"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps2"
	"github.com/breez/lspd/mempool"
	"github.com/breez/lspd/notifications"
	"github.com/breez/lspd/postgresql"
	"github.com/breez/lspd/shared"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
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
	var nodeConfigs []*config.NodeConfig
	err := json.Unmarshal([]byte(n), &nodeConfigs)
	if err != nil {
		log.Fatalf("failed to unmarshal NODES env: %v", err)
	}

	if len(nodeConfigs) == 0 {
		log.Fatalf("need at least one node configured in NODES.")
	}

	nodes, err := initializeNodes(nodeConfigs)
	if err != nil {
		log.Fatalf("failed to initialize nodes: %v", err)
	}

	nodesService, err := shared.NewNodesService(nodes)
	if err != nil {
		log.Fatalf("failed to create nodes service: %v", err)
	}

	mempoolUrl := os.Getenv("MEMPOOL_API_BASE_URL")
	if mempoolUrl == "" {
		log.Fatalf("No mempool url configured.")
	}

	feeEstimator, err := mempool.NewMempoolClient(mempoolUrl)
	if err != nil {
		log.Fatalf("failed to initialize mempool client: %v", err)
	}

	var feeStrategy chain.FeeStrategy
	envFeeStrategy := os.Getenv("MEMPOOL_PRIORITY")
	switch strings.ToLower(envFeeStrategy) {
	case "minimum":
		feeStrategy = chain.FeeStrategyMinimum
	case "economy":
		feeStrategy = chain.FeeStrategyEconomy
	case "hour":
		feeStrategy = chain.FeeStrategyHour
	case "halfhour":
		feeStrategy = chain.FeeStrategyHalfHour
	case "fastest":
		feeStrategy = chain.FeeStrategyFastest
	default:
		feeStrategy = chain.FeeStrategyEconomy
	}
	log.Printf("using mempool api for fee estimation: %v, fee strategy: %v:%v", mempoolUrl, envFeeStrategy, feeStrategy)

	databaseUrl := os.Getenv("DATABASE_URL")
	pool, err := postgresql.PgConnect(databaseUrl)
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}

	interceptStore := postgresql.NewPostgresInterceptStore(pool)
	forwardingStore := postgresql.NewForwardingEventStore(pool)
	notificationsStore := postgresql.NewNotificationsStore(pool)
	lsps2Store := postgresql.NewLsps2Store(pool)
	notificationService := notifications.NewNotificationService(notificationsStore)

	openingService := shared.NewOpeningService(interceptStore, nodesService)
	var interceptors []interceptor.HtlcInterceptor
	for _, node := range nodes {
		var htlcInterceptor interceptor.HtlcInterceptor
		if node.NodeConfig.Lnd != nil {
			client, err := lnd.NewLndClient(node.NodeConfig.Lnd)
			if err != nil {
				log.Fatalf("failed to initialize LND client: %v", err)
			}

			client.StartListeners()
			fwsync := lnd.NewForwardingHistorySync(client, interceptStore, forwardingStore)
			interceptor := interceptor.NewInterceptor(client, node.NodeConfig, interceptStore, feeEstimator, feeStrategy, notificationService)
			htlcInterceptor, err = lnd.NewLndHtlcInterceptor(node.NodeConfig, client, fwsync, interceptor)
			if err != nil {
				log.Fatalf("failed to initialize LND interceptor: %v", err)
			}
		}

		if node.NodeConfig.Cln != nil {
			client, err := cln.NewClnClient(node.NodeConfig.Cln.SocketPath)
			if err != nil {
				log.Fatalf("failed to initialize CLN client: %v", err)
			}

			interceptor := interceptor.NewInterceptor(client, node.NodeConfig, interceptStore, feeEstimator, feeStrategy, notificationService)
			htlcInterceptor, err = cln.NewClnHtlcInterceptor(node.NodeConfig, client, interceptor)
			if err != nil {
				log.Fatalf("failed to initialize CLN interceptor: %v", err)
			}

			msgClient := cln.NewCustomMsgClient(node.NodeConfig.Cln, client)
			go msgClient.Start()
			msgServer := lsps0.NewServer()
			protocolServer := lsps0.NewProtocolServer([]uint32{2})
			lsps2Server := lsps2.NewLsps2Server(openingService, nodesService, node, lsps2Store)
			lsps0.RegisterProtocolServer(msgServer, protocolServer)
			lsps2.RegisterLsps2Server(msgServer, lsps2Server)
			msgClient.WaitStarted()
			defer msgClient.Stop()
			go msgServer.Serve(msgClient)
		}

		if htlcInterceptor == nil {
			log.Fatalf("node has to be either cln or lnd")
		}

		interceptors = append(interceptors, htlcInterceptor)
	}

	address := os.Getenv("LISTEN_ADDRESS")
	certMagicDomain := os.Getenv("CERTMAGIC_DOMAIN")
	cs := NewChannelOpenerServer(interceptStore, openingService)
	ns := notifications.NewNotificationsServer(notificationsStore)
	s, err := NewGrpcServer(nodesService, address, certMagicDomain, cs, ns)
	if err != nil {
		log.Fatalf("failed to initialize grpc server: %v", err)
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

func initializeNodes(configs []*config.NodeConfig) ([]*shared.Node, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no nodes supplied")
	}

	nodes := []*shared.Node{}
	for _, config := range configs {
		pk, err := hex.DecodeString(config.LspdPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("hex.DecodeString(config.lspdPrivateKey=%v) error: %v", config.LspdPrivateKey, err)
		}

		eciesPrivateKey := ecies.NewPrivateKeyFromBytes(pk)
		eciesPublicKey := eciesPrivateKey.PublicKey
		privateKey, publicKey := btcec.PrivKeyFromBytes(pk)
		node := &shared.Node{
			NodeConfig:      config,
			PrivateKey:      privateKey,
			PublicKey:       publicKey,
			EciesPrivateKey: eciesPrivateKey,
			EciesPublicKey:  eciesPublicKey,
		}

		if config.Lnd == nil && config.Cln == nil {
			return nil, fmt.Errorf("node has to be either cln or lnd")
		}

		if config.Lnd != nil && config.Cln != nil {
			return nil, fmt.Errorf("node cannot be both cln and lnd")
		}

		if config.Lnd != nil {
			node.Client, err = lnd.NewLndClient(config.Lnd)
			if err != nil {
				return nil, err
			}
		}

		if config.Cln != nil {
			node.Client, err = cln.NewClnClient(config.Cln.SocketPath)
			if err != nil {
				return nil, err
			}
		}

		// Make sure the nodes is available and set name and pubkey if not set
		// in config.
		info, err := node.Client.GetInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to get info from host %s", node.NodeConfig.Host)
		}

		if node.NodeConfig.Name == "" {
			node.NodeConfig.Name = info.Alias
		}

		if node.NodeConfig.NodePubkey == "" {
			node.NodeConfig.NodePubkey = info.Pubkey
		}

		node.Tokens = config.Tokens
		nodes = append(nodes, node)
	}

	return nodes, nil
}
