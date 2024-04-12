package itest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/breez/lntest"
	"github.com/breez/lspd/config"
	"github.com/breez/lspd/lightning"
	"github.com/breez/lspd/notifications"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	ecies "github.com/ecies/go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/tv42/zbase32"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

var (
	lspdExecutable = flag.String(
		"lspdexec", "", "full path to lpsd plugin binary",
	)
)

var (
	lspBaseFeeMsat uint32 = 1000
	lspFeeRatePpm  uint32 = 1
	lspCltvDelta   uint16 = 144
)

type LspNode interface {
	Start()
	Stop() error
	Harness() *lntest.TestHarness
	PublicKey() *btcec.PublicKey
	EciesPublicKey() *ecies.PublicKey
	Rpc() lspd.ChannelOpenerClient
	NotificationsRpc() notifications.NotificationsClient
	NodeId() []byte
	LightningNode() lntest.LightningNode
	PostgresBackend() *PostgresContainer
}

type lspBase struct {
	harness         *lntest.TestHarness
	conf            *config.NodeConfig
	name            string
	binary          string
	env             []string
	scriptFilePath  string
	logFilePath     string
	grpcAddress     string
	pubkey          *secp256k1.PublicKey
	eciesPubkey     *ecies.PublicKey
	postgresBackend *PostgresContainer
	isInitialized   bool
	runtime         *lspBaseRuntime
	cleanups        []*lntest.Cleanup
}

type lspBaseRuntime struct {
	node     lntest.LightningNode
	cmd      *exec.Cmd
	logFile  *os.File
	cleanups []*lntest.Cleanup
}

func newLspd(h *lntest.TestHarness, mem *mempoolApi, name string, nodeConfig *config.NodeConfig, lnd *config.LndConfig, cln *config.ClnConfig, envExt ...string) (*lspBase, error) {
	scriptDir := h.GetDirectory(fmt.Sprintf("lspd-%s", name))
	log.Printf("%s: Creating LSPD in dir %s", name, scriptDir)

	pgLogfile := filepath.Join(scriptDir, "postgres.log")
	h.RegisterLogfile(pgLogfile, fmt.Sprintf("%s-postgres", name))
	postgresBackend, err := NewPostgresContainer(pgLogfile)
	if err != nil {
		return nil, err
	}

	lspdBinary, err := getLspdBinary()
	if err != nil {
		return nil, err
	}

	lspdPort, err := lntest.GetPort()
	if err != nil {
		return nil, err
	}

	lspdPrivateKeyBytes, err := GenerateRandomBytes(32)
	if err != nil {
		return nil, err
	}

	_, publ := btcec.PrivKeyFromBytes(lspdPrivateKeyBytes)
	eciesPubl := ecies.NewPrivateKeyFromBytes(lspdPrivateKeyBytes).PublicKey
	host := "localhost"
	grpcAddress := fmt.Sprintf("%s:%d", host, lspdPort)
	minConfs := uint32(1)
	conf := &config.NodeConfig{
		Name:                      name,
		LspdPrivateKey:            hex.EncodeToString(lspdPrivateKeyBytes),
		Tokens:                    []string{"hello"},
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
		Lnd:                       lnd,
		Cln:                       cln,
	}

	if nodeConfig != nil {
		if nodeConfig.MinConfs != nil {
			conf.MinConfs = nodeConfig.MinConfs
		}

		if nodeConfig.LegacyOnionTokens != nil {
			conf.LegacyOnionTokens = nodeConfig.LegacyOnionTokens
		}
	}

	env := []string{
		fmt.Sprintf("DATABASE_URL=%s", postgresBackend.ConnectionString()),
		"AUTO_MIGRATE_DATABASE=true",
		fmt.Sprintf("LISTEN_ADDRESS=%s", grpcAddress),
		fmt.Sprintf("MEMPOOL_API_BASE_URL=%s", mem.Address()),
		"MEMPOOL_PRIORITY=economy",
	}

	env = append(env, envExt...)

	scriptFilePath := filepath.Join(scriptDir, "start-lspd.sh")
	logFilePath := filepath.Join(scriptDir, "lspd.log")
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lspd-%s", name))

	l := &lspBase{
		harness:         h,
		conf:            conf,
		name:            name,
		env:             env,
		binary:          lspdBinary,
		scriptFilePath:  scriptFilePath,
		logFilePath:     logFilePath,
		grpcAddress:     grpcAddress,
		pubkey:          publ,
		eciesPubkey:     eciesPubl,
		postgresBackend: postgresBackend,
	}
	h.AddStoppable(l)
	h.AddCleanable(l)
	return l, nil
}

func (l *lspBase) StopRuntime() {
	if l.runtime != nil {
		lntest.PerformCleanup(l.runtime.cleanups)
	}

	l.runtime = nil
}

func (l *lspBase) Stop() error {
	l.StopRuntime()
	lntest.PerformCleanup(l.cleanups)
	return nil
}

func (l *lspBase) Cleanup() error {
	return l.postgresBackend.Cleanup(context.Background())
}

func (l *lspBase) Start(
	node lntest.LightningNode,
	confFunc func(*config.NodeConfig),
) {
	var runtimeCleanups []*lntest.Cleanup
	wasInitialized := l.isInitialized

	node.Start()
	runtimeCleanups = append(runtimeCleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: lsp lightning node", l.name),
		Fn:   node.Stop,
	})

	if !wasInitialized {
		l.initialize(runtimeCleanups, confFunc)
	}

	cmd := exec.CommandContext(l.harness.Ctx, l.scriptFilePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := os.Create(l.logFilePath)
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(l.cleanups)
		l.harness.T.Fatalf("failed create lsp logfile: %v", err)
	}
	runtimeCleanups = append(runtimeCleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: logfile", l.name),
		Fn:   logFile.Close,
	})

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	log.Printf("%s: starting lspd %s", l.name, l.scriptFilePath)
	err = cmd.Start()
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(l.cleanups)
		l.harness.T.Fatalf("failed start lspd: %v", err)
	}
	runtimeCleanups = append(runtimeCleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: cmd", l.name),
		Fn: func() error {
			proc := cmd.Process
			if proc == nil {
				return nil
			}

			syscall.Kill(-proc.Pid, syscall.SIGINT)

			log.Printf("About to wait for lspd to exit")
			status, err := proc.Wait()
			if err != nil {
				log.Printf("waiting for lspd process error: %v, status: %v", err, status)
			}
			err = cmd.Wait()
			if err != nil {
				log.Printf("waiting for lspd cmd error: %v", err)
			}

			return nil
		},
	})

	if !wasInitialized {
		<-time.After(time.Second * 2)
		tx, err := l.postgresBackend.Pool().Begin(l.harness.Ctx)
		defer tx.Rollback(l.harness.Ctx)
		if err != nil {
			lntest.PerformCleanup(runtimeCleanups)
			lntest.PerformCleanup(l.cleanups)
			l.harness.T.Fatalf("failed to begin postgres tx: %v", err)
		}

		_, err = tx.Exec(
			l.harness.Ctx,
			`DELETE FROM new_channel_params`,
		)
		if err != nil {
			lntest.PerformCleanup(runtimeCleanups)
			lntest.PerformCleanup(l.cleanups)
			l.harness.T.Fatalf("failed to delete new_channel_params: %v", err)
		}

		_, err = tx.Exec(
			l.harness.Ctx,
			`INSERT INTO new_channel_params (validity, params, token)
			VALUES
			(3600, '{"min_msat": "1000000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432}', 'hello'),
			(259200, '{"min_msat": "1100000", "proportional": 7500, "max_idle_time": 4320, "max_client_to_self_delay": 432}', 'hello');`,
		)
		if err != nil {
			lntest.PerformCleanup(runtimeCleanups)
			lntest.PerformCleanup(l.cleanups)
			l.harness.T.Fatalf("failed to insert new_channel_params: %v", err)
		}

		err = tx.Commit(l.harness.Ctx)
		if err != nil {
			lntest.PerformCleanup(runtimeCleanups)
			lntest.PerformCleanup(l.cleanups)
			l.harness.T.Fatalf("failed to commit tx: %v", err)
		}
	}

	l.runtime = &lspBaseRuntime{
		node:     node,
		cmd:      cmd,
		logFile:  logFile,
		cleanups: runtimeCleanups,
	}
}

func (l *lspBase) initialize(runtimeCleanups []*lntest.Cleanup, confFunc func(*config.NodeConfig)) {
	var initializeCleanups []*lntest.Cleanup
	err := l.postgresBackend.Start(l.harness.Ctx)
	if err != nil {
		lntest.PerformCleanup(initializeCleanups)
		l.harness.T.Fatalf("failed to start postgres container: %v", err)
	}

	initializeCleanups = append(initializeCleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: postgres container", l.name),
		Fn: func() error {
			return l.postgresBackend.Stop(context.Background())
		},
	})

	log.Printf("%s: Creating lspd startup script at %s", l.name, l.scriptFilePath)
	scriptFile, err := os.OpenFile(l.scriptFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(initializeCleanups)
		l.harness.T.Fatalf("failed to create lspd startup script: %v", err)
	}

	defer scriptFile.Close()
	writer := bufio.NewWriter(scriptFile)
	_, err = writer.WriteString("#!/bin/bash\n")
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(initializeCleanups)
		l.harness.T.Fatalf("failed to write lspd startup script: %v", err)
	}

	confFunc(l.conf)
	confJson, _ := json.Marshal(l.conf)
	nodes := fmt.Sprintf(`NODES='[%s]'`, string(confJson))
	log.Printf("%s: node config: %s", l.name, nodes)

	for _, str := range append(l.env, nodes) {
		_, err = writer.WriteString("export " + str + "\n")
		if err != nil {
			lntest.PerformCleanup(runtimeCleanups)
			lntest.PerformCleanup(initializeCleanups)
			l.harness.T.Fatalf("failed to write lspd startup script: %v", err)
		}
	}

	_, err = writer.WriteString(l.binary + "\n")
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(initializeCleanups)
		l.harness.T.Fatalf("failed to write lspd startup script: %v", err)
	}

	err = writer.Flush()
	if err != nil {
		lntest.PerformCleanup(runtimeCleanups)
		lntest.PerformCleanup(initializeCleanups)
		l.harness.T.Fatalf("failed to flush lspd startup script: %v", err)
	}

	l.isInitialized = true
	l.cleanups = initializeCleanups
}

func ChannelInformation(l LspNode) *lspd.ChannelInformationReply {
	info, err := l.Rpc().ChannelInformation(
		l.Harness().Ctx,
		&lspd.ChannelInformationRequest{},
	)
	if err != nil {
		l.Harness().T.Fatalf("Failed to get ChannelInformation: %v", err)
	}

	return info
}

func RegisterPayment(l LspNode, paymentInfo *lspd.PaymentInformation, continueOnError bool) error {
	serialized, err := proto.Marshal(paymentInfo)
	lntest.CheckError(l.Harness().T, err)

	encrypted, err := ecies.Encrypt(l.EciesPublicKey(), serialized)
	lntest.CheckError(l.Harness().T, err)

	ctx := metadata.AppendToOutgoingContext(l.Harness().Ctx, "authorization", "Bearer hello")
	log.Printf("Registering payment")
	_, err = l.Rpc().RegisterPayment(
		ctx,
		&lspd.RegisterPaymentRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(l.Harness().T, err)
	}

	return err
}

func SubscribeNotifications(l LspNode, b BreezClient, url string, continueOnError bool) error {
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
	lntest.CheckError(l.Harness().T, err)

	encrypted, err := ecies.Encrypt(l.EciesPublicKey(), serialized)
	lntest.CheckError(l.Harness().T, err)

	ctx := metadata.AppendToOutgoingContext(l.Harness().Ctx, "authorization", "Bearer hello")
	log.Printf("Subscribing to notifications")
	_, err = l.NotificationsRpc().SubscribeNotifications(
		ctx,
		&notifications.EncryptedNotificationRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(l.Harness().T, err)
	}

	return err
}

func UnsubscribeNotifications(l LspNode, b BreezClient, url string, continueOnError bool) error {
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
	lntest.CheckError(l.Harness().T, err)

	encrypted, err := ecies.Encrypt(l.EciesPublicKey(), serialized)
	lntest.CheckError(l.Harness().T, err)

	ctx := metadata.AppendToOutgoingContext(l.Harness().Ctx, "authorization", "Bearer hello")
	log.Printf("Removing notification subscription")
	_, err = l.NotificationsRpc().UnsubscribeNotifications(
		ctx,
		&notifications.EncryptedNotificationRequest{
			Blob: encrypted,
		},
	)

	if !continueOnError {
		lntest.CheckError(l.Harness().T, err)
	}

	return nil
}

type FeeParamSetting struct {
	Validity     time.Duration
	MinMsat      uint64
	Proportional uint32
}

func SetFeeParams(l LspNode, settings []*FeeParamSetting) error {
	_, err := l.PostgresBackend().Pool().Exec(l.Harness().Ctx, "DELETE FROM new_channel_params")
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
			`(%d, '{"min_msat": "%d", "proportional": %d, "max_idle_time": 4320, "max_client_to_self_delay": 432}', 'hello')`,
			int64(setting.Validity.Seconds()),
			setting.MinMsat,
			setting.Proportional,
		)

		first = false
	}
	query += `;`
	_, err = l.PostgresBackend().Pool().Exec(l.Harness().Ctx, query)
	if err != nil {
		return fmt.Errorf("failed to insert new_channel_params: %w", err)
	}

	return nil
}

func getLspdBinary() (string, error) {
	if lspdExecutable != nil {
		return *lspdExecutable, nil
	}

	return exec.LookPath("lspd")
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
