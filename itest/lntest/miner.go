package lntest

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/elementsproject/glightning/gbitcoin"
)

type Miner struct {
	harness         *TestHarness
	binary          string
	args            []string
	dir             string
	rpcPort         uint32
	rpcUser         string
	rpcPass         string
	zmqBlockAddress string
	zmqTxAddress    string
	isInitialized   bool
	timesStarted    int
	runtime         *minerRuntime
	mtx             sync.Mutex
}

type minerRuntime struct {
	rpc      *gbitcoin.Bitcoin
	cmd      *exec.Cmd
	cleanups []*Cleanup
}

func NewMiner(h *TestHarness) *Miner {
	binary, err := GetBitcoindBinary()
	CheckError(h.T, err)

	return NewMinerFromBinary(h, binary)
}

func NewMinerFromBinary(h *TestHarness, binary string) *Miner {
	btcUser := "btcuser"
	btcPass := "btcpass"
	bitcoindDir := h.GetDirectory("miner")
	rpcPort, err := GetPort()
	CheckError(h.T, err)

	zmqBlockPort, err := GetPort()
	CheckError(h.T, err)

	zmqTxPort, err := GetPort()
	CheckError(h.T, err)

	host := "127.0.0.1"
	zmqBlockAddress := fmt.Sprintf("tcp://%s:%d", host, zmqBlockPort)
	zmqTxAddress := fmt.Sprintf("tcp://%s:%d", host, zmqTxPort)
	args := []string{
		"-regtest",
		"-server",
		"-logtimestamps",
		"-nolisten",
		"-addresstype=bech32",
		"-txindex",
		"-fallbackfee=0.00000253",
		"-debug=mempool",
		"-debug=rpc",
		fmt.Sprintf("-datadir=%s", bitcoindDir),
		fmt.Sprintf("-rpcport=%d", rpcPort),
		fmt.Sprintf("-rpcpassword=%s", btcPass),
		fmt.Sprintf("-rpcuser=%s", btcUser),
		fmt.Sprintf("-zmqpubrawblock=%s", zmqBlockAddress),
		fmt.Sprintf("-zmqpubrawtx=%s", zmqTxAddress),
	}

	miner := &Miner{
		harness:         h,
		binary:          binary,
		args:            args,
		dir:             bitcoindDir,
		rpcPort:         rpcPort,
		rpcUser:         btcUser,
		rpcPass:         btcPass,
		zmqBlockAddress: zmqBlockAddress,
		zmqTxAddress:    zmqTxAddress,
	}

	h.AddStoppable(miner)
	h.RegisterLogfile(filepath.Join(bitcoindDir, "regtest", "debug.log"), filepath.Base(bitcoindDir))
	return miner
}

func (m *Miner) Start() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.runtime != nil {
		log.Printf("miner: Start called, but was already started.")
		return
	}

	var cleanups []*Cleanup
	log.Printf("starting %s on rpc port %d in dir %s...", m.binary, m.rpcPort, m.dir)
	cmd := exec.CommandContext(m.harness.Ctx, m.binary, m.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Start()
	CheckError(m.harness.T, err)
	cleanups = append(cleanups, &Cleanup{
		Name: "miner: cmd",
		Fn: func() error {
			proc := cmd.Process
			if proc == nil {
				return nil
			}

			sig := syscall.SIGINT
			if runtime.GOOS == "windows" {
				sig = syscall.SIGKILL
			}

			err := syscall.Kill(-proc.Pid, sig)
			if err != nil {
				log.Printf("miner: Error sending signal %v: %v", sig, err)
			}
			state, err := proc.Wait()
			if err != nil {
				log.Printf("miner: Error waiting for process to complete. State %v: %v", state, err)
			}

			return err
		},
	})
	err = m.waitForLog("init message: Done loading")
	if err != nil {
		PerformCleanup(cleanups)
		m.harness.T.Fatalf("Error waiting for bitcoind to start: %v", err)
	}
	m.timesStarted += 1

	log.Printf("miner: bitcoind started (%d)!", cmd.Process.Pid)

	rpc := gbitcoin.NewBitcoin(m.rpcUser, m.rpcPass)
	rpc.SetTimeout(uint(2))

	log.Printf("miner: Starting up bitcoin client")
	rpc.StartUp("http://localhost", m.dir, uint(m.rpcPort))
	m.runtime = &minerRuntime{
		rpc:      rpc,
		cmd:      cmd,
		cleanups: cleanups,
	}

	if !m.isInitialized {
		err = m.initializeFirstRun()
		if err != nil {
			m.runtime = nil
			PerformCleanup(cleanups)
			m.harness.Fatalf("Failed to initialize for first run: %v", err)
		}
	}
}

func (m *Miner) initializeFirstRun() error {
	l := true
	d := false
	_, err := m.runtime.rpc.CreateWallet(&gbitcoin.CreateWalletRequest{
		WalletName:    "default",
		LoadOnStartup: &l,
		Descriptors:   &d,
	})
	if err != nil {
		log.Printf("miner: Create wallet failed. Ignoring error: %v", err)
	}

	// Go ahead and run 101 blocks
	log.Printf("Get new address")
	addr, err := m.runtime.rpc.GetNewAddress(gbitcoin.Bech32)
	if err != nil {
		return fmt.Errorf("failed to get new address: %v", err)
	}

	log.Printf("Generate to address")
	_, err = m.runtime.rpc.GenerateToAddress(addr, 101)
	if err != nil {
		return fmt.Errorf("failed to generate to address: %v", err)
	}

	m.isInitialized = true
	return nil
}

func (n *Miner) waitForLog(phrase string) error {
	logfilePath := filepath.Join(n.dir, "regtest", "debug.log")
	// at startup we need to wait for the file to open
	for time.Now().Before(n.harness.Deadline()) {
		if _, err := os.Stat(logfilePath); os.IsNotExist(err) {
			<-time.After(waitSleepInterval)
			continue
		}
		break
	}
	logfile, _ := os.Open(logfilePath)
	defer logfile.Close()

	reader := bufio.NewReader(logfile)
	counted := 0
	for time.Now().Before(n.harness.Deadline()) {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				<-time.After(waitSleepInterval)
			} else {
				return err
			}
		}
		m, err := regexp.MatchString(phrase, line)
		if err != nil {
			return err
		}

		if m {
			if counted == n.timesStarted {
				return nil
			}

			counted += 1
		}
	}

	return fmt.Errorf("unable to find \"%s\" in %s", phrase, logfilePath)
}

func (m *Miner) Stop() error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.runtime == nil {
		log.Printf("miner: Stop called, but was already stopped.")
		return nil
	}

	PerformCleanup(m.runtime.cleanups)
	m.runtime = nil
	return nil
}

func (m *Miner) ZmqBlockAddress() string {
	return m.zmqBlockAddress
}

func (m *Miner) ZmqTxAddress() string {
	return m.zmqTxAddress
}

func (m *Miner) MineBlocks(n uint) {
	addr, err := m.runtime.rpc.GetNewAddress(gbitcoin.Bech32)
	CheckError(m.harness.T, err)
	_, err = m.runtime.rpc.GenerateToAddress(addr, n)
	CheckError(m.harness.T, err)
}

func (m *Miner) SendToAddress(addr string, amountSat uint64) {
	amountBtc := amountSat / uint64(100000000)
	amountSatRemainder := amountSat % 100000000
	amountStr := strconv.FormatUint(amountBtc, 10) + "." + fmt.Sprintf("%08s", strconv.FormatUint(amountSatRemainder, 10))
	log.Printf("miner: Sending %s btc to address %s", amountStr, addr)
	_, err := m.runtime.rpc.SendToAddress(addr, amountStr)
	CheckError(m.harness.T, err)
}

func (m *Miner) SendToAddressAndMine(addr string, amountSat uint64, blocks uint) {
	m.SendToAddress(addr, amountSat)
	m.MineBlocks(blocks)
}

func (m *Miner) GetBlockHeight() uint32 {
	info, err := m.runtime.rpc.GetChainInfo()
	CheckError(m.harness.T, err)
	return info.Blocks
}
