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

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	ecies "github.com/ecies/go/v2"
	"github.com/golang/protobuf/proto"
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
	Start()
	Stop() error
	Harness() *lntest.TestHarness
	PublicKey() *btcec.PublicKey
	EciesPublicKey() *ecies.PublicKey
	Rpc() lspd.ChannelOpenerClient
	NodeId() []byte
	LightningNode() lntest.LightningNode
	SupportsChargingFees() bool
}

type lspBase struct {
	harness         *lntest.TestHarness
	name            string
	binary          string
	env             []string
	scriptFilePath  string
	grpcAddress     string
	pubkey          *secp256k1.PublicKey
	eciesPubkey     *ecies.PublicKey
	postgresBackend *PostgresContainer
}

func newLspd(h *lntest.TestHarness, name string, lnd *string, cln *string, envExt ...string) (*lspBase, error) {
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
	var ext string
	if lnd != nil {
		ext = fmt.Sprintf(`"lnd": %s`, *lnd)
	} else if cln != nil {
		ext = fmt.Sprintf(`"cln": %s`, *cln)
	} else {
		h.T.Fatalf("%s: need either lnd or cln config", name)
	}

	nodes := fmt.Sprintf(
		`NODES='[ { "name": "%s", "lspdPrivateKey": "%x", "token": "hello", "host": "host:port",`+
			` "publicChannelAmount": "1000183", "channelAmount": "100000", "channelPrivate": false,`+
			` "targetConf": "6", "minHtlcMsat": "600", "baseFeeMsat": "1000", "feeRate": "0.000001",`+
			` "timeLockDelta": "144", "channelFeePermyriad": "40", "channelMinimumFeeMsat": "2000000",`+
			` "additionalChannelCapacity": "100000", "maxInactiveDuration": "3888000", %s}]'`,
		name,
		lspdPrivateKeyBytes,
		ext,
	)
	env := []string{
		nodes,
		fmt.Sprintf("DATABASE_URL=%s", postgresBackend.ConnectionString()),
		fmt.Sprintf("LISTEN_ADDRESS=%s", grpcAddress),
	}

	env = append(env, envExt...)

	scriptFilePath := filepath.Join(scriptDir, "start-lspd.sh")

	l := &lspBase{
		harness:         h,
		name:            name,
		env:             env,
		binary:          lspdBinary,
		scriptFilePath:  scriptFilePath,
		grpcAddress:     grpcAddress,
		pubkey:          publ,
		eciesPubkey:     eciesPubl,
		postgresBackend: postgresBackend,
	}
	h.AddStoppable(l)
	h.AddCleanable(l)
	return l, nil
}

func (l *lspBase) Stop() error {
	return l.postgresBackend.Stop(context.Background())
}

func (l *lspBase) Cleanup() error {
	return l.postgresBackend.Cleanup(context.Background())
}

func (l *lspBase) Initialize() error {
	var cleanups []*lntest.Cleanup
	migrationsDir, err := getMigrationsDir()
	if err != nil {
		return err
	}

	err = l.postgresBackend.Start(l.harness.Ctx)
	if err != nil {
		return err
	}

	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: postgres container", l.name),
		Fn: func() error {
			return l.postgresBackend.Stop(context.Background())
		},
	})
	err = l.postgresBackend.RunMigrations(l.harness.Ctx, migrationsDir)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return err
	}

	log.Printf("%s: Creating lspd startup script at %s", l.name, l.scriptFilePath)
	scriptFile, err := os.OpenFile(l.scriptFilePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return err
	}

	defer scriptFile.Close()
	writer := bufio.NewWriter(scriptFile)
	_, err = writer.WriteString("#!/bin/bash\n")
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return err
	}

	for _, str := range l.env {
		_, err = writer.WriteString("export " + str + "\n")
		if err != nil {
			lntest.PerformCleanup(cleanups)
			return err
		}
	}

	_, err = writer.WriteString(l.binary + "\n")
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return err
	}

	err = writer.Flush()
	if err != nil {
		lntest.PerformCleanup(cleanups)
		return err
	}

	return nil
}

func RegisterPayment(l LspNode, paymentInfo *lspd.PaymentInformation) {
	serialized, err := proto.Marshal(paymentInfo)
	lntest.CheckError(l.Harness().T, err)

	encrypted, err := ecies.Encrypt(l.EciesPublicKey(), serialized)
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
