package itest

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/breez/lntest"
	"github.com/breez/lspd/btceclegacy"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	lspdExecutable = flag.String(
		"lspdexec", "", "full path to lpsd plugin binary",
	)
	lspdMigrationsDir = flag.String(
		"lspdmigrationsdir", "", "full path to lspd sql migrations directory",
	)
)

var (
	lspBaseFeeMsat uint32 = 1000
	lspFeeRatePpm  uint32 = 1
	lspCltvDelta   uint16 = 40
)

type LspNode interface {
	Harness() *lntest.TestHarness
	PublicKey() *btcec.PublicKey
	Rpc() lspd.ChannelOpenerClient
	NodeId() []byte
	LightningNode() lntest.LightningNode
}

type ClnLspNode struct {
	harness         *lntest.TestHarness
	lightningNode   *lntest.CoreLightningNode
	rpc             lspd.ChannelOpenerClient
	publicKey       btcec.PublicKey
	postgresBackend *PostgresContainer
}

func (c *ClnLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *ClnLspNode) PublicKey() *btcec.PublicKey {
	return &c.publicKey
}

func (c *ClnLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.rpc
}

func (l *ClnLspNode) TearDown() error {
	// NOTE: The lightningnode will be torn down on its own.
	return l.postgresBackend.Shutdown(l.harness.Ctx)
}

func (l *ClnLspNode) Cleanup() error {
	return l.postgresBackend.Cleanup(l.harness.Ctx)
}

func (l *ClnLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *ClnLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

type LndLspNode struct {
	harness         *lntest.TestHarness
	lightningNode   *lntest.LndNode
	rpc             lspd.ChannelOpenerClient
	publicKey       btcec.PublicKey
	postgresBackend *PostgresContainer
	logFile         *os.File
	lspdCmd         *exec.Cmd
}

func (c *LndLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *LndLspNode) PublicKey() *btcec.PublicKey {
	return &c.publicKey
}

func (c *LndLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.rpc
}

func (l *LndLspNode) TearDown() error {
	// NOTE: The lightningnode will be torn down on its own.
	if l.lspdCmd != nil && l.lspdCmd.Process != nil {
		err := l.lspdCmd.Process.Kill()
		if err != nil {
			log.Printf("error stopping lspd process: %v", err)
		}
	}

	if l.logFile != nil {
		err := l.logFile.Close()
		if err != nil {
			log.Printf("error closing logfile: %v", err)
		}
	}

	return l.postgresBackend.Shutdown(l.harness.Ctx)
}

func (l *LndLspNode) Cleanup() error {
	return l.postgresBackend.Cleanup(l.harness.Ctx)
}

func (l *LndLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *LndLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

func NewClnLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) LspNode {
	scriptFilePath, grpcAddress, publ, postgresBackend := setupLspd(h, name, "RUN_CLN=true")
	args := []string{
		fmt.Sprintf("--plugin=%s", scriptFilePath),
		fmt.Sprintf("--fee-base=%d", lspBaseFeeMsat),
		fmt.Sprintf("--fee-per-satoshi=%d", lspFeeRatePpm),
		fmt.Sprintf("--cltv-delta=%d", lspCltvDelta),
	}

	lightningNode := lntest.NewCoreLightningNode(h, m, name, args...)

	conn, err := grpc.Dial(
		grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&token{token: "hello"}),
	)
	lntest.CheckError(h.T, err)

	client := lspd.NewChannelOpenerClient(conn)

	lspNode := &ClnLspNode{
		harness:         h,
		lightningNode:   lightningNode,
		rpc:             client,
		publicKey:       *publ,
		postgresBackend: postgresBackend,
	}

	h.AddStoppable(lspNode)
	h.AddCleanable(lspNode)
	return lspNode
}

func NewLndLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) LspNode {
	args := []string{
		"--protocol.zero-conf",
		"--protocol.option-scid-alias",
		"--requireinterceptor",
		"--bitcoin.defaultchanconfs=0",
		"--bitcoin.chanreservescript=\"0\"",
		fmt.Sprintf("--bitcoin.basefee=%d", lspBaseFeeMsat),
		fmt.Sprintf("--bitcoin.feerate=%d", lspFeeRatePpm),
		fmt.Sprintf("--bitcoin.timelockdelta=%d", lspCltvDelta),
	}

	lightningNode := lntest.NewLndNode(h, m, name, args...)
	tlsCert := strings.Replace(string(lightningNode.TlsCert()), "\n", "\\n", -1)
	scriptFilePath, grpcAddress, publ, postgresBackend := setupLspd(h, name,
		"RUN_LND=true",
		fmt.Sprintf("LND_CERT=\"%s\"", tlsCert),
		fmt.Sprintf("LND_ADDRESS=%s", lightningNode.GrpcHost()),
		fmt.Sprintf("LND_MACAROON_HEX=%x", lightningNode.Macaroon()),
	)
	scriptDir := filepath.Dir(scriptFilePath)
	logFilePath := filepath.Join(scriptDir, "lspd.log")
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lspd-%s", name))

	lspdCmd := exec.CommandContext(h.Ctx, scriptFilePath)
	logFile, err := os.Create(logFilePath)
	lntest.CheckError(h.T, err)

	lspdCmd.Stdout = logFile
	lspdCmd.Stderr = logFile

	log.Printf("%s: starting lspd %s", name, scriptFilePath)
	err = lspdCmd.Start()
	lntest.CheckError(h.T, err)

	conn, err := grpc.Dial(
		grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&token{token: "hello"}),
	)
	lntest.CheckError(h.T, err)

	client := lspd.NewChannelOpenerClient(conn)

	lspNode := &LndLspNode{
		harness:         h,
		lightningNode:   lightningNode,
		rpc:             client,
		publicKey:       *publ,
		postgresBackend: postgresBackend,
		logFile:         logFile,
		lspdCmd:         lspdCmd,
	}

	h.AddStoppable(lspNode)
	h.AddCleanable(lspNode)
	return lspNode
}

func setupLspd(h *lntest.TestHarness, name string, envExt ...string) (string, string, *secp256k1.PublicKey, *PostgresContainer) {
	scriptDir := h.GetDirectory(fmt.Sprintf("lspd-%s", name))
	log.Printf("%s: Creating LSPD in dir %s", name, scriptDir)
	migrationsDir, err := getMigrationsDir()
	lntest.CheckError(h.T, err)

	pgLogfile := filepath.Join(scriptDir, "postgres.log")
	h.RegisterLogfile(pgLogfile, fmt.Sprintf("%s-postgres", name))
	postgresBackend := StartPostgresContainer(h.T, h.Ctx, pgLogfile)
	postgresBackend.RunMigrations(h.T, h.Ctx, migrationsDir)

	lspdBinary, err := getLspdBinary()
	lntest.CheckError(h.T, err)

	lspdPort, err := lntest.GetPort()
	lntest.CheckError(h.T, err)

	lspdPrivateKeyBytes, err := GenerateRandomBytes(32)
	lntest.CheckError(h.T, err)

	_, publ := btcec.PrivKeyFromBytes(lspdPrivateKeyBytes)
	host := "localhost"
	grpcAddress := fmt.Sprintf("%s:%d", host, lspdPort)
	env := []string{
		"NODE_NAME=lsp",
		"NODE_PUBKEY=dunno",
		"NODE_HOST=host:port",
		"TOKEN=hello",
		fmt.Sprintf("DATABASE_URL=%s", postgresBackend.ConnectionString()),
		fmt.Sprintf("LISTEN_ADDRESS=%s", grpcAddress),
		fmt.Sprintf("LSPD_PRIVATE_KEY=%x", lspdPrivateKeyBytes),
	}

	env = append(env, envExt...)

	scriptFilePath := filepath.Join(scriptDir, "start-lspd.sh")
	log.Printf("%s: Creating lspd startup script at %s", name, scriptFilePath)
	scriptFile, err := os.OpenFile(scriptFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	lntest.CheckError(h.T, err)

	writer := bufio.NewWriter(scriptFile)
	_, err = writer.WriteString("#!/bin/bash\n")
	lntest.CheckError(h.T, err)

	for _, str := range env {
		_, err = writer.WriteString("export " + str + "\n")
		lntest.CheckError(h.T, err)
	}

	_, err = writer.WriteString(lspdBinary + "\n")
	lntest.CheckError(h.T, err)

	err = writer.Flush()
	lntest.CheckError(h.T, err)
	scriptFile.Close()

	return scriptFilePath, grpcAddress, publ, postgresBackend
}

func RegisterPayment(l LspNode, paymentInfo *lspd.PaymentInformation) {
	serialized, err := proto.Marshal(paymentInfo)
	lntest.CheckError(l.Harness().T, err)

	encrypted, err := btceclegacy.Encrypt(l.PublicKey(), serialized)
	lntest.CheckError(l.Harness().T, err)

	log.Printf("Registering payment")
	_, err = l.Rpc().RegisterPayment(
		l.Harness().Ctx,
		&lspd.RegisterPaymentRequest{
			Blob: encrypted,
		},
	)
	lntest.CheckError(l.Harness().T, err)
}

func getLspdBinary() (string, error) {
	if lspdExecutable != nil {
		return *lspdExecutable, nil
	}

	return exec.LookPath("lspd")
}

func getMigrationsDir() (string, error) {
	if lspdMigrationsDir != nil {
		return *lspdMigrationsDir, nil
	}

	return exec.LookPath("lspdmigrationsdir")
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
