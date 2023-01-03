package itest

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type LndLspNode struct {
	harness       *lntest.TestHarness
	lightningNode *lntest.LndNode
	logFilePath   string
	isInitialized bool
	lspBase       *lspBase
	runtime       *lndLspNodeRuntime
	mtx           sync.Mutex
}

type lndLspNodeRuntime struct {
	logFile  *os.File
	cmd      *exec.Cmd
	rpc      lspd.ChannelOpenerClient
	cleanups []*lntest.Cleanup
}

func NewLndLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) LspNode {
	args := []string{
		"--protocol.zero-conf",
		"--protocol.option-scid-alias",
		"--requireinterceptor",
		"--bitcoin.defaultchanconfs=0",
		fmt.Sprintf("--bitcoin.chanreservescript=\"0 if (chanAmt != %d) else chanAmt/100\"", publicChanAmount),
		fmt.Sprintf("--bitcoin.basefee=%d", lspBaseFeeMsat),
		fmt.Sprintf("--bitcoin.feerate=%d", lspFeeRatePpm),
		fmt.Sprintf("--bitcoin.timelockdelta=%d", lspCltvDelta),
	}

	lightningNode := lntest.NewLndNode(h, m, name, args...)
	lnd := fmt.Sprintf(
		`{ "address": "%s", "cert": "%s", "macaroon": "%x" }`,
		lightningNode.GrpcHost(),
		base64.StdEncoding.EncodeToString(lightningNode.TlsCert()),
		lightningNode.Macaroon(),
	)
	lspBase, err := newLspd(h, name, &lnd, nil)
	if err != nil {
		h.T.Fatalf("failed to initialize lspd")
	}
	scriptDir := filepath.Dir(lspBase.scriptFilePath)
	logFilePath := filepath.Join(scriptDir, "lspd.log")
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lspd-%s", name))

	lspNode := &LndLspNode{
		harness:       h,
		lightningNode: lightningNode,
		logFilePath:   logFilePath,
		lspBase:       lspBase,
	}

	h.AddStoppable(lspNode)
	return lspNode
}

func (c *LndLspNode) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var cleanups []*lntest.Cleanup
	wasInitialized := c.isInitialized
	if !c.isInitialized {
		err := c.lspBase.Initialize()
		if err != nil {
			c.harness.T.Fatalf("failed to initialize lsp: %v", err)
		}
		c.isInitialized = true
		cleanups = append(cleanups, &lntest.Cleanup{
			Name: fmt.Sprintf("%s: lsp base", c.lspBase.name),
			Fn:   c.lspBase.Stop,
		})
	}

	c.lightningNode.Start()
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: lsp lightning node", c.lspBase.name),
		Fn:   c.lightningNode.Stop,
	})

	if !wasInitialized {
		scriptFile, err := os.ReadFile(c.lspBase.scriptFilePath)
		if err != nil {
			lntest.PerformCleanup(cleanups)
			c.harness.T.Fatalf("failed to open scriptfile '%s': %v", c.lspBase.scriptFilePath, err)
		}

		err = os.Remove(c.lspBase.scriptFilePath)
		if err != nil {
			lntest.PerformCleanup(cleanups)
			c.harness.T.Fatalf("failed to remove scriptfile '%s': %v", c.lspBase.scriptFilePath, err)
		}

		split := strings.Split(string(scriptFile), "\n")
		for i, s := range split {
			if strings.HasPrefix(s, "export NODES") {
				ext := fmt.Sprintf(
					`"lnd": { "address": "%s", "cert": "%s", "macaroon": "%x" }}]'`,
					c.lightningNode.GrpcHost(),
					base64.StdEncoding.EncodeToString(c.lightningNode.TlsCert()),
					c.lightningNode.Macaroon(),
				)
				start, _, _ := strings.Cut(s, `"lnd"`)

				split[i] = start + ext
			}
		}
		newContent := strings.Join(split, "\n")
		err = os.WriteFile(c.lspBase.scriptFilePath, []byte(newContent), 0755)
		if err != nil {
			lntest.PerformCleanup(cleanups)
			c.harness.T.Fatalf("failed to rewrite scriptfile '%s': %v", c.lspBase.scriptFilePath, err)
		}
	}

	cmd := exec.CommandContext(c.harness.Ctx, c.lspBase.scriptFilePath)
	logFile, err := os.Create(c.logFilePath)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		c.harness.T.Fatalf("failed create lsp logfile: %v", err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: logfile", c.lspBase.name),
		Fn:   logFile.Close,
	})

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	log.Printf("%s: starting lspd %s", c.lspBase.name, c.lspBase.scriptFilePath)
	err = cmd.Start()
	if err != nil {
		lntest.PerformCleanup(cleanups)
		c.harness.T.Fatalf("failed to start lspd: %v", err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: cmd", c.lspBase.name),
		Fn: func() error {
			proc := cmd.Process
			if proc == nil {
				return nil
			}

			proc.Kill()

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

	conn, err := grpc.Dial(
		c.lspBase.grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(&token{token: "hello"}),
	)
	if err != nil {
		lntest.PerformCleanup(cleanups)
		c.harness.T.Fatalf("failed to create grpc connection: %v", err)
	}
	cleanups = append(cleanups, &lntest.Cleanup{
		Name: fmt.Sprintf("%s: grpc conn", c.lspBase.name),
		Fn:   conn.Close,
	})

	client := lspd.NewChannelOpenerClient(conn)
	c.runtime = &lndLspNodeRuntime{
		logFile:  logFile,
		cmd:      cmd,
		rpc:      client,
		cleanups: cleanups,
	}
}

func (c *LndLspNode) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.runtime == nil {
		return nil
	}

	lntest.PerformCleanup(c.runtime.cleanups)
	c.runtime = nil
	return nil
}

func (c *LndLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *LndLspNode) PublicKey() *btcec.PublicKey {
	return c.lspBase.pubkey
}

func (c *LndLspNode) EciesPublicKey() *ecies.PublicKey {
	return c.lspBase.eciesPubkey
}

func (c *LndLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.runtime.rpc
}

func (l *LndLspNode) SupportsChargingFees() bool {
	return true
}

func (l *LndLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *LndLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}
