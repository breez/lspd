package lspd

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/breez/lspd/build"
	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/cln"
	"github.com/breez/lspd/common"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/history"
	"github.com/breez/lspd/interceptor"
	"github.com/breez/lspd/lnd"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps2"
	"github.com/breez/lspd/mempool"
	"github.com/breez/lspd/notifications"
	"github.com/breez/lspd/postgresql"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
)

func Main(ctx context.Context, config *config.Config) error {
	log.Printf(`Starting lspd, tag='%s', revision='%s'`, build.GetTag(), build.GetRevision())

	nodes, err := initializeNodes(config.Nodes)
	if err != nil {
		log.Fatalf("failed to initialize nodes: %v", err)
	}

	nodesService, err := common.NewNodesService(nodes)
	if err != nil {
		log.Fatalf("failed to create nodes service: %v", err)
	}

	var mempoolClient *mempool.MempoolClient
	if config.MempoolApiBaseUrl != nil {
		mempoolClient, err = mempool.NewMempoolClient(*config.MempoolApiBaseUrl)
		if err != nil {
			log.Fatalf("failed to initialize mempool client: %v", err)
		}

		log.Printf("using mempool api for fee estimation: %v, fee strategy: %v", *config.MempoolApiBaseUrl, config.FeeStrategy)
	}

	if config.AutoMigrateDb {
		err = postgresql.Migrate(config.DatabaseUrl)
		if err != nil {
			log.Fatalf("Failed to migrate postgres database: %v", err)
		}
	}
	pool, err := postgresql.PgConnect(config.DatabaseUrl)
	if err != nil {
		log.Fatalf("pgConnect() error: %v", err)
	}

	interceptor.OpenChannelEmailConfig = config.OpenChannelEmail
	interceptStore := postgresql.NewPostgresInterceptStore(pool)
	openingStore := postgresql.NewPostgresOpeningStore(pool)
	notificationsStore := postgresql.NewNotificationsStore(pool)
	lsps2Store := postgresql.NewLsps2Store(pool)
	historyStore := postgresql.NewHistoryStore(pool)

	ctx, cancel := context.WithCancel(ctx)
	notificationService := notifications.NewNotificationService(notificationsStore)
	go notificationService.Start(ctx)
	openingService := common.NewOpeningService(openingStore, nodesService)
	lsps2CleanupService := lsps2.NewCleanupService(lsps2Store)
	go lsps2CleanupService.Start(ctx)
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

			client.StartListeners(ctx)

			forwardSync := lnd.NewForwardSync(node.NodeId, client, historyStore)
			go forwardSync.ForwardsSynchronize(ctx)
			interceptor := interceptor.NewInterceptHandler(client, node.NodeConfig, interceptStore, historyStore, openingService, feeEstimator, config.FeeStrategy, notificationService)
			combinedHandler := common.NewCombinedHandler(interceptor)
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
			legacyHandler := interceptor.NewInterceptHandler(client, node.NodeConfig, interceptStore, historyStore, openingService, feeEstimator, config.FeeStrategy, notificationService)
			lsps2Handler := lsps2.NewInterceptHandler(lsps2Store, historyStore, openingService, client, feeEstimator, &lsps2.InterceptorConfig{
				NodeId:                       node.NodeId,
				AdditionalChannelCapacitySat: uint64(node.NodeConfig.AdditionalChannelCapacity),
				MinConfs:                     node.NodeConfig.MinConfs,
				TargetConf:                   node.NodeConfig.TargetConf,
				FeeStrategy:                  config.FeeStrategy,
				MinPaymentSizeMsat:           node.NodeConfig.MinPaymentSizeMsat,
				MaxPaymentSizeMsat:           node.NodeConfig.MaxPaymentSizeMsat,
				TimeLockDelta:                node.NodeConfig.TimeLockDelta,
				HtlcMinimumMsat:              node.NodeConfig.MinHtlcMsat,
				MppTimeout:                   time.Second * 90,
			})
			go lsps2Handler.Start(ctx)
			combinedHandler := common.NewCombinedHandler(lsps2Handler, legacyHandler)
			htlcInterceptor, err = cln.NewClnHtlcInterceptor(node.NodeConfig, client, combinedHandler)
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

	cs := NewChannelOpenerServer(interceptStore, openingService)
	ns := notifications.NewNotificationsServer(notificationsStore)
	s, err := NewGrpcServer(nodesService, config.ListenAddress, config.CertmagicDomain, cs, ns)
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

	go func() {
		<-ctx.Done()
		log.Printf("Received stop signal. Stopping.")

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
			if tlsCert, err := os.ReadFile(config.Lnd.Cert); err == nil {
				config.Lnd.Cert = string(tlsCert)
			}
			if macaroon, err := os.ReadFile(config.Lnd.Macaroon); err == nil {
				config.Lnd.Macaroon = hex.EncodeToString(macaroon)
			}
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
			return nil, fmt.Errorf("failed to get info from host %s", node.NodeConfig.Host)
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
