package itest

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/breez/lntest"
	lspd "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	ecies "github.com/ecies/go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var clnPluginExec = flag.String(
	"clnpluginexec", "", "full path to cln plugin wrapper binary",
)

type ClnLspNode struct {
	harness       *lntest.TestHarness
	lightningNode *lntest.ClnNode
	lspBase       *lspBase
	logFilePath   string
	runtime       *clnLspNodeRuntime
	isInitialized bool
	mtx           sync.Mutex
	pluginBinary  string
	pluginAddress string
}

type clnLspNodeRuntime struct {
	logFile  *os.File
	cmd      *exec.Cmd
	rpc      lspd.ChannelOpenerClient
	cleanups []*lntest.Cleanup
}

func NewClnLspdNode(h *lntest.TestHarness, m *lntest.Miner, name string) LspNode {
	scriptDir := h.GetDirectory("lspd")
	pluginBinary := *clnPluginExec
	pluginPort, err := lntest.GetPort()
	if err != nil {
		h.T.Fatalf("failed to get port for the htlc interceptor plugin.")
	}
	pluginAddress := fmt.Sprintf("127.0.0.1:%d", pluginPort)

	args := []string{
		fmt.Sprintf("--plugin=%s", pluginBinary),
		fmt.Sprintf("--lsp.listen=%s", pluginAddress),
		fmt.Sprintf("--fee-base=%d", lspBaseFeeMsat),
		fmt.Sprintf("--fee-per-satoshi=%d", lspFeeRatePpm),
		fmt.Sprintf("--cltv-delta=%d", lspCltvDelta),
		"--max-concurrent-htlcs=30",
		"--dev-allowdustreserve=true",
	}
	lightningNode := lntest.NewClnNode(h, m, name, args...)
	cln := fmt.Sprintf(
		`{ "pluginAddress": "%s", "socketPath": "%s" }`,
		pluginAddress,
		filepath.Join(lightningNode.SocketDir(), lightningNode.SocketFile()),
	)
	lspbase, err := newLspd(h, name, nil, &cln)
	if err != nil {
		h.T.Fatalf("failed to initialize lspd")
	}

	logFilePath := filepath.Join(scriptDir, "lspd.log")
	h.RegisterLogfile(logFilePath, fmt.Sprintf("lspd-%s", name))

	lspNode := &ClnLspNode{
		harness:       h,
		lightningNode: lightningNode,
		logFilePath:   logFilePath,
		lspBase:       lspbase,
		pluginBinary:  pluginBinary,
		pluginAddress: pluginAddress,
	}

	h.AddStoppable(lspNode)
	return lspNode
}

func (c *ClnLspNode) Start() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	var cleanups []*lntest.Cleanup
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
		Name: fmt.Sprintf("%s: lightning node", c.lspBase.name),
		Fn:   c.lightningNode.Stop,
	})

	cmd := exec.CommandContext(c.harness.Ctx, c.lspBase.scriptFilePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
	c.runtime = &clnLspNodeRuntime{
		logFile:  logFile,
		cmd:      cmd,
		rpc:      client,
		cleanups: cleanups,
	}
}

func (c *ClnLspNode) Stop() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.runtime == nil {
		return nil
	}

	lntest.PerformCleanup(c.runtime.cleanups)
	c.runtime = nil
	return nil
}

func (c *ClnLspNode) Harness() *lntest.TestHarness {
	return c.harness
}

func (c *ClnLspNode) PublicKey() *btcec.PublicKey {
	return c.lspBase.pubkey
}

func (c *ClnLspNode) EciesPublicKey() *ecies.PublicKey {
	return c.lspBase.eciesPubkey
}

func (c *ClnLspNode) Rpc() lspd.ChannelOpenerClient {
	return c.runtime.rpc
}

func (l *ClnLspNode) NodeId() []byte {
	return l.lightningNode.NodeId()
}

func (l *ClnLspNode) LightningNode() lntest.LightningNode {
	return l.lightningNode
}

func (l *ClnLspNode) SupportsChargingFees() bool {
	return false
}
