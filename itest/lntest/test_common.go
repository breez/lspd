package lntest

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"
	"time"
)

var (
	bitcoindExecutable = flag.String(
		"bitcoindexec", "", "full path to bitcoind binary",
	)
	bitcoinCliExecutable = flag.String(
		"bitcoincliexec", "", "full path to bitcoin-cli binary",
	)
	lightningdExecutable = flag.String(
		"lightningdexec", "", "full path to lightningd binary",
	)
	lndExecutable = flag.String(
		"lndexec", "", "full path to lnd binary",
	)
	testDir = flag.String(
		"testdir", "", "full path to the root testing directory",
	)
	preserveLogs = flag.Bool(
		"preservelogs", false, "value indicating whether the logs of artifacts should be preserved",
	)
	preserveState = flag.Bool(
		"preservestate", false, "value indicating whether all artifact state should be preserved",
	)
)

var waitSleepInterval = time.Millisecond * 100

func CheckError(t *testing.T, err error) {
	if err != nil {
		debug.PrintStack()
		t.Fatal(err)
	}
}

func GetPort() (uint32, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

func GetTestRootDir() (*string, error) {
	dir := testDir
	if dir == nil || *dir == "" {
		pathDir, err := exec.LookPath("testdir")
		if err != nil {
			dir = &pathDir
		}
	}

	if dir == nil || *dir == "" {
		tempDir, err := os.MkdirTemp("", "lntest")
		return &tempDir, err
	}

	info, err := os.Stat(*dir)
	if err != nil {
		// Create the dir if it doesn't exist
		err = os.MkdirAll(*dir, os.ModePerm)
		if err != nil {
			return nil, err
		}

		return dir, nil
	}

	// dir exists, make sure it's a directory.
	if !info.IsDir() {
		return nil, fmt.Errorf("TestDir '%s' exists but is not a directory", *dir)
	}

	return dir, nil
}

func GetBitcoindBinary() (string, error) {
	if bitcoindExecutable != nil {
		return *bitcoindExecutable, nil
	}

	return exec.LookPath("bitcoind")
}

func GetBitcoinCliBinary() (string, error) {
	if bitcoinCliExecutable != nil {
		return *bitcoinCliExecutable, nil
	}

	return exec.LookPath("bitcoin-cli")
}

func GetLightningdBinary() (string, error) {
	if lightningdExecutable != nil {
		return *lightningdExecutable, nil
	}

	return exec.LookPath("lightningd")
}

func GetLndBinary() (string, error) {
	if lndExecutable != nil {
		return *lndExecutable, nil
	}

	return exec.LookPath("lnd")
}

func GetPreserveLogs() bool {
	return *preserveLogs
}

func GetPreserveState() bool {
	return *preserveState
}

func getTimeoutSeconds(t *testing.T, timeout time.Time) uint32 {
	timeoutSeconds := time.Until(timeout).Seconds()
	if timeoutSeconds < 0 {
		CheckError(t, fmt.Errorf("timeout expired"))
	}

	return uint32(timeoutSeconds)
}

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

func GenerateRandomString() (string, error) {
	b, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}
