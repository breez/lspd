package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/breez/lspd/build"
	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/cln"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/history"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lnd"
	"github.com/breez/lspd/mempool"
	"github.com/breez/lspd/notifications"
	"github.com/breez/lspd/postgresql"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"github.com/urfave/cli"
)

func runLspd(cliCtx *cli.Context) error {
	log.Printf(`Starting lspd, tag='%s', revision='%s'`, build.GetTag(), build.GetRevision())
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

	nodesService, err := common.NewNodesService(nodes)
	if err != nil {
		log.Fatalf("failed to create nodes service: %v", err)
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

	var mempoolClient *mempool.MempoolClient
	mempoolUrl := os.Getenv("MEMPOOL_API_BASE_URL")
	if mempoolUrl != "" {
		mempoolClient, err = mempool.NewMempoolClient(mempoolUrl)
		if err != nil {
			log.Fatalf("failed to initialize mempool client: %v", err)
		}

		log.Printf("using mempool api for fee estimation: %v, fee strategy: %v:%v", mempoolUrl, envFeeStrategy, feeStrategy)
	}

	databaseUrl := os.Getenv("DATABASE_URL")
	automigrate, _ := strconv.ParseBool(os.Getenv("AUTO_MIGRATE_DATABASE"))
	if automigrate {
		err = postgresql.Migrate(databaseUrl)
		if err != nil {
			log.Fatalf("Failed to migrate postgres database: %v", err)
		}
	}
	pool, err := postgresql.PgConnect(databaseUrl)
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}

	interceptStore := postgresql.NewPostgresInterceptStore(pool)
	openingStore := postgresql.NewPostgresOpeningStore(pool)
	notificationsStore := postgresql.NewNotificationsStore(pool)
	historyStore := postgresql.NewHistoryStore(pool)

	ctx, cancel := context.WithCancel(context.Background())
	notificationService := notifications.NewNotificationService(notificationsStore)
	go notificationService.Start(ctx)
	openingService := common.NewOpeningService(openingStore, nodesService)
	notificationCleanupService := notifications.NewCleanupService(notificationsStore)
	go notificationCleanupService.Start(ctx)
	channelSync := history.NewChannelSync(nodes, historyStore)
	go channelSync.ChannelsSynchronize(ctx)

	var interceptors []interceptor.HtlcInterceptor
	for _, node := range nodes {
		var htlcInterceptor interceptor.HtlcInterceptor
		var feeEstimator chain.FeeEstimator
		if mempoolClient == nil {
			feeEstimator = chain.NewDefaultFeeEstimator(node.NodeConfig.TargetConf)
		} else {
			feeEstimator = mempoolClient
		}

		if node.NodeConfig.Lnd != nil {
			client, err := lnd.NewLndClient(node.NodeConfig.Lnd)
			if err != nil {
				log.Fatalf("failed to initialize LND client: %v", err)
			}

			client.StartListeners()

			forwardSync := lnd.NewForwardSync(node.NodeId, client, historyStore)
			go forwardSync.ForwardsSynchronize(ctx)
			handler := interceptor.NewInterceptHandler(client, node.NodeConfig, interceptStore, historyStore, openingService, feeEstimator, feeStrategy, notificationService)
			combinedHandler := common.NewCombinedHandler(handler)
			htlcInterceptor, err = lnd.NewLndHtlcInterceptor(node.NodeConfig, client, combinedHandler)
			if err != nil {
				log.Fatalf("failed to initialize LND interceptor: %v", err)
			}
		}

		if node.NodeConfig.Cln != nil {
			client, err := cln.NewClnClient(node.NodeConfig.Cln)
			if err != nil {
				log.Fatalf("failed to initialize CLN client: %v", err)
			}

			forwardSync := cln.NewForwardSync(node.NodeId, client, historyStore)
			go forwardSync.ForwardsSynchronize(ctx)
			handler := interceptor.NewInterceptHandler(client, node.NodeConfig, interceptStore, historyStore, openingService, feeEstimator, feeStrategy, notificationService)
			combinedHandler := common.NewCombinedHandler(handler)
			htlcInterceptor, err = cln.NewClnHtlcInterceptor(node.NodeConfig, client, combinedHandler)
			if err != nil {
				log.Fatalf("failed to initialize CLN interceptor: %v", err)
			}
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

		cancel()
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
	return nil
}

func initializeNodes(configs []*config.NodeConfig) ([]*common.Node, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no nodes supplied")
	}

	nodes := []*common.Node{}
	for _, config := range configs {
		pk, err := hex.DecodeString(config.LspdPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("hex.DecodeString(config.lspdPrivateKey=%v) error: %v", config.LspdPrivateKey, err)
		}

		eciesPrivateKey := ecies.NewPrivateKeyFromBytes(pk)
		eciesPublicKey := eciesPrivateKey.PublicKey
		privateKey, publicKey := btcec.PrivKeyFromBytes(pk)
		node := &common.Node{
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
			if caCert, err := os.ReadFile(config.Cln.CaCert); err == nil {
				config.Cln.CaCert = string(caCert)
			}
			if clientCert, err := os.ReadFile(config.Cln.ClientCert); err == nil {
				config.Cln.ClientCert = string(clientCert)
			}
			if clientKey, err := os.ReadFile(config.Cln.ClientKey); err == nil {
				config.Cln.ClientKey = string(clientKey)
			}
			node.Client, err = cln.NewClnClient(config.Cln)
			if err != nil {
				return nil, err
			}
		}

		// Make sure the nodes is available and set name and pubkey if not set
		// in config.
		info, err := node.Client.GetInfo()
		if err != nil {
			decodedPubkey, decodeErr := hex.DecodeString(
				node.NodeConfig.NodePubkey)
			if node.NodeConfig.Name == "" || node.NodeConfig.NodePubkey == "" ||
				decodeErr != nil || len(decodedPubkey) != 33 {

				return nil, fmt.Errorf("failed to get info from host %s",
					node.NodeConfig.Host)
			}

			info = &lightning.GetInfoResult{
				Alias:  node.NodeConfig.Name,
				Pubkey: node.NodeConfig.NodePubkey,
			}
		}

		if node.NodeConfig.Name == "" {
			node.NodeConfig.Name = info.Alias
		}

		if node.NodeConfig.NodePubkey == "" {
			node.NodeConfig.NodePubkey = info.Pubkey
		}

		nodeId, err := hex.DecodeString(info.Pubkey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node id %s", info.Pubkey)
		}

		node.NodeId = nodeId
		node.Tokens = config.Tokens
		nodes = append(nodes, node)
	}

	return nodes, nil
}
