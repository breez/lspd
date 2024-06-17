package itest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"path/filepath"
	"sync"
	"time"

	"github.com/breez/lspd"
	"github.com/breez/lspd/chain"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/itest/lntest"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/lsps0"
	"github.com/breez/lspd/lsps0/jsonrpc"
	"github.com/breez/lspd/lsps2"
	"github.com/breez/lspd/notifications"
	"github.com/breez/lspd/postgresql"
	"github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	ecies "github.com/ecies/go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/tv42/zbase32"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

var (
	lspBaseFeeMsat uint32 = 1000
	lspFeeRatePpm  uint32 = 1
	lspCltvDelta   uint16 = 144
)

type Lspd struct {
	h               *lntest.TestHarness
	config          *config.Config
	name            string
	logFilePath     string
	grpcAddress     string
	clients         []*lspdClient
	postgresBackend *PostgresContainer
	runtime         *lspdRuntime
}

type lspdClient struct {
	node        lntest.LightningNode
	pubkey      *secp256k1.PublicKey
	eciesPubkey *ecies.PublicKey
}

type lspdRuntime struct {
	ctx     context.Context
	cancel  context.CancelFunc
	clients []*LspdClient
	wg      sync.WaitGroup
}

type LspdClient struct {
	index               int
	lspd                *Lspd
	info                *lspdClient
	client              rpc.ChannelOpenerClient
	notificationsClient notifications.NotificationsClient
}

type NodeWithConfig struct {
	Node   lntest.LightningNode
	Config *config.NodeConfig
}

func (l *Lspd) Harness() *lntest.TestHarness {
	return l.h
}

func (l *Lspd) Address() string {
	return l.grpcAddress
}

func (l *Lspd) Client(index int) *LspdClient {
	if l.runtime == nil || len(l.runtime.clients) < (index+1) {
		l.h.Fatalf("Requested non-existent client with index %d", index)
	}

	return l.runtime.clients[index]
}

func (l *Lspd) PostgresBackend() *PostgresContainer {
	return l.postgresBackend
}

func (c *LspdClient) NodeId() []byte {
	return c.info.node.NodeId()
}

func (c *LspdClient) Rpc() rpc.ChannelOpenerClient {
	return c.client
}

func (c *LspdClient) NotificationsRpc() notifications.NotificationsClient {
	return c.notificationsClient
}

func NewLspd(h *lntest.TestHarness, name string, mem *mempoolApi, nodes ...*NodeWithConfig) (*Lspd, error) {
	dir := h.GetDirectory(fmt.Sprintf("postgres-%s", name))
	pgLogfile := filepath.Join(dir, "postgres.log")
	h.RegisterLogfile(pgLogfile, fmt.Sprintf("%s-postgres", name))
	postgres, err := NewPostgresContainer(pgLogfile)
	if err != nil {
		return nil, err
	}
	var postgresErr error
	var postgresWg sync.WaitGroup
	postgresWg.Add(1)
	go func() {
		postgresErr = postgres.Start(h.Ctx)
		postgresWg.Done()
	}()

	lspdPort, err := lntest.GetPort()
	if err != nil {
		return nil, err
	}

	host := "localhost"
	grpcAddress := fmt.Sprintf("%s:%d", host, lspdPort)
	minConfs := uint32(1)

	var clients []*lspdClient
	var innerNodes []*config.NodeConfig
	for i, node := range nodes {
		lspdPrivateKeyBytes, err := GenerateRandomBytes(32)
		if err != nil {
			return nil, err
		}

		conf := &config.NodeConfig{
			Name:                      name,
			LspdPrivateKey:            hex.EncodeToString(lspdPrivateKeyBytes),
			Tokens:                    []string{fmt.Sprintf("hello%d", i)},
			Host:                      "host:port",
			PublicChannelAmount:       1000183,
			TargetConf:                6,
			MinConfs:                  &minConfs,
			MinHtlcMsat:               600,
			BaseFeeMsat:               1000,
			FeeRate:                   0.000001,
			TimeLockDelta:             144,
			ChannelFeePermyriad:       40,
			ChannelMinimumFeeMsat:     2000000,
			AdditionalChannelCapacity: 100000,
			MaxInactiveDuration:       3888000,
			MinPaymentSizeMsat:        600,
			MaxPaymentSizeMsat:        4000000000,
			NotificationTimeout:       "20s",
		}

		if node.Config != nil {
			if node.Config.MinConfs != nil {
				conf.MinConfs = node.Config.MinConfs
			}

			if node.Config.LegacyOnionTokens != nil {
				conf.LegacyOnionTokens = node.Config.LegacyOnionTokens
			}

			if node.Config.Lnd != nil {
				conf.Lnd = node.Config.Lnd
			}

			if node.Config.Cln != nil {
				conf.Cln = node.Config.Cln
			}
		}

		innerNodes = append(innerNodes, conf)

		_, publ := btcec.PrivKeyFromBytes(lspdPrivateKeyBytes)
		eciesPubl := ecies.NewPrivateKeyFromBytes(lspdPrivateKeyBytes).PublicKey
		clients = append(clients, &lspdClient{
			node:        node.Node,
			pubkey:      publ,
			eciesPubkey: eciesPubl,
		})
	}

	var memAddress *string
	if mem != nil {
		t := mem.Address()
		memAddress = &t
	}

	conf := &config.Config{
		Nodes:             innerNodes,
		FeeStrategy:       chain.FeeStrategyEconomy,
		MempoolApiBaseUrl: memAddress,
		DatabaseUrl:       postgres.ConnectionString(),
		AutoMigrateDb:     true,
		ListenAddress:     grpcAddress,
		CertmagicDomain:   "",
	}

	l := &Lspd{
		h:               h,
		config:          conf,
		name:            name,
		grpcAddress:     grpcAddress,
		clients:         clients,
		postgresBackend: postgres,
	}

	postgresWg.Wait()
	if postgresErr != nil {
		return nil, fmt.Errorf("failed to start postgres backend: %w", postgresErr)
	}
	err = postgresql.Migrate(l.postgresBackend.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to migrate postgres backend: %w", err)
	}
	tx, err := l.postgresBackend.Pool().Begin(l.h.Ctx)
	defer tx.Rollback(l.h.Ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin postgres tx: %w", err)
	}

	_, err = tx.Exec(
		l.h.Ctx,
		`DELETE FROM new_channel_params`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to delete new_channel_params: %w", err)
	}

	for i, _ := range nodes {
		_, err = tx.Exec(
			l.h.Ctx,
			`INSERT INTO new_channel_params (validity, params, token)
			VALUES
			(3600, '{"min_msat": "1000000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432}', $1),
			(259200, '{"min_msat": "1100000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432}', $1);`,
			fmt.Sprintf("hello%d", i),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert new_channel_params: %w", err)
		}
	}

	err = tx.Commit(l.h.Ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}

	h.AddStoppable(l)
	h.AddCleanable(l)
	return l, nil
}

func (l *Lspd) Start() error {
	ctx, cancel := context.WithCancel(l.h.Ctx)
	l.runtime = &lspdRuntime{
		ctx:    ctx,
		cancel: cancel,
	}
	l.runtime.wg.Add(1)
	go func(ctx context.Context, config *config.Config, wg *sync.WaitGroup) {
		err := lspd.Main(ctx, config)
		log.Printf("%s: lspd exited with: %v", l.name, err)
		wg.Done()
	}(ctx, l.config, &l.runtime.wg)

	for i, c := range l.clients {
		conn, err := grpc.DialContext(
			ctx,
			l.grpcAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithPerRPCCredentials(&token{token: fmt.Sprintf("hello%d", i)}),
		)
		if err != nil {
			cancel()
			return fmt.Errorf("failed to create grpc connection: %w", err)
		}

		l.runtime.clients = append(l.runtime.clients, &LspdClient{
			index:               i,
			lspd:                l,
			info:                c,
			client:              rpc.NewChannelOpenerClient(conn),
			notificationsClient: notifications.NewNotificationsClient(conn),
		})
	}

	return nil
}

func (l *Lspd) Stop() error {
	r := l.runtime
	if r == nil {
		return nil
	}

	r.cancel()
	r.wg.Wait()
	l.runtime = nil
	return nil
}

func (l *Lspd) Cleanup() error {
	return l.postgresBackend.Cleanup(context.Background())
}

func (c *LspdClient) ChannelInformation() *rpc.ChannelInformationReply {
	info, err := c.Rpc().ChannelInformation(
		c.lspd.h.Ctx,
		&rpc.ChannelInformationRequest{},
	)
	if err != nil {
		c.lspd.h.T.Fatalf("Failed to get ChannelInformation: %v", err)
	}

	return info
}

func (c *LspdClient) ctx() context.Context {
	return c.lspd.runtime.ctx
}

func (c *LspdClient) RegisterPayment(paymentInfo *rpc.PaymentInformation, continueOnError bool) error {
	serialized, err := proto.Marshal(paymentInfo)
	lntest.CheckError(c.lspd.h.T, err)

	encrypted, err := ecies.Encrypt(c.info.eciesPubkey, serialized)
	lntest.CheckError(c.lspd.h.T, err)

	log.Printf("Registering payment")
	_, err = c.Rpc().RegisterPayment(
		c.ctx(),
		&rpc.RegisterPaymentRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(c.lspd.h.T, err)
	}

	return err
}

func (c *LspdClient) SubscribeNotifications(b BreezClient, url string, continueOnError bool) error {
	msg := append(lightning.SignedMsgPrefix, []byte(url)...)
	first := sha256.Sum256([]byte(msg))
	second := sha256.Sum256(first[:])
	sig, err := ecdsa.SignCompact(b.Node().PrivateKey(), second[:], true)
	assert.NoError(b.Harness().T, err)
	request := notifications.SubscribeNotificationsRequest{
		Url:       url,
		Signature: zbase32.EncodeToString(sig),
	}
	serialized, err := proto.Marshal(&request)
	lntest.CheckError(c.lspd.h.T, err)

	encrypted, err := ecies.Encrypt(c.info.eciesPubkey, serialized)
	lntest.CheckError(c.lspd.h.T, err)

	log.Printf("Subscribing to notifications")
	_, err = c.NotificationsRpc().SubscribeNotifications(
		c.ctx(),
		&notifications.EncryptedNotificationRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(c.lspd.h.T, err)
	}

	return err
}

func (c *LspdClient) UnsubscribeNotifications(b BreezClient, url string, continueOnError bool) error {
	msg := append(lightning.SignedMsgPrefix, []byte(url)...)
	first := sha256.Sum256([]byte(msg))
	second := sha256.Sum256(first[:])
	sig, err := ecdsa.SignCompact(b.Node().PrivateKey(), second[:], true)
	assert.NoError(b.Harness().T, err)
	request := notifications.UnsubscribeNotificationsRequest{
		Url:       url,
		Signature: zbase32.EncodeToString(sig),
	}
	serialized, err := proto.Marshal(&request)
	lntest.CheckError(c.lspd.h.T, err)

	encrypted, err := ecies.Encrypt(c.info.eciesPubkey, serialized)
	lntest.CheckError(c.lspd.h.T, err)

	log.Printf("Removing notification subscription")
	_, err = c.NotificationsRpc().UnsubscribeNotifications(
		c.ctx(),
		&notifications.EncryptedNotificationRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(c.lspd.h.T, err)
	}

	return nil
}

type FeeParamSetting struct {
	Validity     time.Duration
	MinMsat      uint64
	Proportional uint32
}

func (c *LspdClient) SetFeeParams(settings []*FeeParamSetting) error {
	_, err := c.lspd.postgresBackend.Pool().Exec(c.lspd.h.Ctx, "DELETE FROM new_channel_params WHERE token = $1", fmt.Sprintf("hello%d", c.index))
	if err != nil {
		return fmt.Errorf("failed to delete new_channel_params: %w", err)
	}

	if len(settings) == 0 {
		return nil
	}

	query := `INSERT INTO new_channel_params (validity, params, token) VALUES `
	first := true
	for _, setting := range settings {
		if !first {
			query += `,`
		}

		query += fmt.Sprintf(
			`(%d, '{"min_msat": "%d", "proportional": %d, "max_idle_time": 4320, "max_client_to_self_delay": 432}', 'hello%d')`,
			int64(setting.Validity.Seconds()),
			setting.MinMsat,
			setting.Proportional,
			c.index,
		)

		first = false
	}
	query += `;`
	_, err = c.lspd.postgresBackend.Pool().Exec(c.lspd.h.Ctx, query)
	if err != nil {
		return fmt.Errorf("failed to insert new_channel_params: %w", err)
	}

	return nil
}

func (c *LspdClient) Lsps2GetInfo(bc BreezClient, req lsps2.GetInfoRequest) lsps2.GetInfoResponse {
	req.Version = lsps2.SupportedVersion
	r := c.lsps2RequestResponse(bc, "lsps2.get_info", req)
	var resp lsps2.GetInfoResponse
	err := json.Unmarshal(r, &resp)
	lntest.CheckError(c.lspd.h.T, err)

	return resp
}

func (c *LspdClient) Lsps2Buy(bc BreezClient, req lsps2.BuyRequest) lsps2.BuyResponse {
	req.Version = lsps2.SupportedVersion
	r := c.lsps2RequestResponse(bc, "lsps2.buy", req)
	var resp lsps2.BuyResponse
	err := json.Unmarshal(r, &resp)
	lntest.CheckError(c.lspd.h.T, err)

	return resp
}

func (c *LspdClient) lsps2RequestResponse(bc BreezClient, method string, req interface{}) []byte {
	id := randStringBytes(32)
	peerId := hex.EncodeToString(c.NodeId())
	inner, err := json.Marshal(req)
	lntest.CheckError(c.lspd.h.T, err)
	outer, err := json.Marshal(&jsonrpc.Request{
		JsonRpc: jsonrpc.Version,
		Method:  method,
		Id:      id,
		Params:  inner,
	})
	lntest.CheckError(c.lspd.h.T, err)

	log.Printf(string(outer))
	bc.Node().SendCustomMessage(&lntest.CustomMsgRequest{
		PeerId: peerId,
		Type:   lsps0.Lsps0MessageType,
		Data:   outer,
	})

	m := bc.ReceiveCustomMessage()
	log.Printf(string(m.Data))

	var resp jsonrpc.Response
	err = json.Unmarshal(m.Data, &resp)
	lntest.CheckError(c.lspd.h.T, err)

	if resp.Id != id {
		c.lspd.h.T.Fatalf("Received custom message, but had different id")
	}

	return resp.Result
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

type token struct {
	token string
}

func (t *token) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	m := make(map[string]string)
	m["authorization"] = "Bearer " + t.token
	return m, nil
}

// RequireTransportSecurity indicates whether the credentials requires
// transport security.
func (t *token) RequireTransportSecurity() bool {
	return false
}
