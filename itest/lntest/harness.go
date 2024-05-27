package lntest

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/multierr"
	"golang.org/x/exp/slices"
)

type TestHarness struct {
	*testing.T
	Ctx           context.Context
	cancel        context.CancelFunc
	deadline      time.Time
	Dir           string
	mtx           sync.RWMutex
	stoppables    []Stoppable
	cleanables    []Cleanable
	logFiles      []*logFile
	dumpLogs      bool
	preserveLogs  bool
	preserveState bool
}

type logFile struct {
	path string
	name string
}

type Stoppable interface {
	Stop() error
}

type Cleanable interface {
	Cleanup() error
}

type HarnessOption int

const (
	DumpLogs      HarnessOption = 0
	PreserveLogs  HarnessOption = 1
	PreserveState HarnessOption = 2
)

func NewTestHarness(t *testing.T, deadline time.Time, options ...HarnessOption) *TestHarness {
	rootDir, err := GetTestRootDir()
	CheckError(t, err)

	testDir, err := os.MkdirTemp(*rootDir, "")
	CheckError(t, err)

	log.Printf("Testing directory for this harness: '%s'", testDir)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	h := &TestHarness{
		T:             t,
		Ctx:           ctx,
		cancel:        cancel,
		deadline:      deadline,
		Dir:           testDir,
		dumpLogs:      slices.Contains(options, DumpLogs),
		preserveLogs:  slices.Contains(options, PreserveLogs) || GetPreserveLogs(),
		preserveState: slices.Contains(options, PreserveState) || GetPreserveState(),
	}

	return h
}

func (h *TestHarness) Deadline() time.Time {
	return h.deadline
}

func (h *TestHarness) GetDirectory(pattern string) string {
	dir, err := os.MkdirTemp(h.Dir, pattern)
	CheckError(h.T, err)

	return dir
}

func (h *TestHarness) AddStoppable(stoppable Stoppable) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	h.stoppables = append(h.stoppables, stoppable)
}

func (h *TestHarness) RegisterLogfile(path string, name string) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	h.logFiles = append(h.logFiles, &logFile{path: path, name: name})
}

func (h *TestHarness) AddCleanable(cleanable Cleanable) {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	h.cleanables = append(h.cleanables, cleanable)
}

func (h *TestHarness) TearDown() {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	for _, stoppable := range h.stoppables {
		err := stoppable.Stop()
		if err != nil {
			log.Printf("Failed to tear down artifact. Error: %v", err)
		}
	}

	if h.dumpLogs {
		for _, logFile := range h.logFiles {
			var sb strings.Builder
			sb.WriteString("*********************************************************\n")
			sb.WriteString(fmt.Sprintf("Log dump for %s, path %s\n", logFile.name, logFile.path))
			sb.WriteString("*****************************************************************************\n")
			content, err := os.ReadFile(logFile.path)
			if err == nil {
				sb.Write(content)
			}
			sb.WriteString("\n")
			sb.WriteString("*****************************************************************************\n")
			sb.WriteString(fmt.Sprintf("End log dump for %s, path %s\n", logFile.name, logFile.path))
			sb.WriteString("*****************************************************************************\n")
			log.Print(sb.String())
		}
	}

	err := h.cleanup()
	if err != nil {
		log.Printf("Harness cleanup failed. Error: %v", err)
	}

	h.cancel()
}

func (h *TestHarness) cleanup() error {
	logsPath := filepath.Join(h.Dir, "logs")
	err := os.MkdirAll(logsPath, os.ModePerm)
	if err != nil {
		log.Printf("Could not create preserve log directory %s. Error: %v", logsPath, err)
	}

	if h.preserveLogs {
		for _, logFile := range h.logFiles {
			src, err := os.Open(logFile.path)
			if err != nil {
				continue
			}
			logFileName := logFile.name
			if !strings.HasSuffix(logFileName, ".log") {
				logFileName += ".log"
			}

			dst, err := os.Create(filepath.Join(logsPath, logFileName))
			if err != nil {
				log.Printf("Could not preserve log %s with name %s, error: %v", logFile.path, logFileName, err)
				continue
			}

			_, err = io.Copy(dst, src)
			if err != nil {
				log.Printf("Could not preserve log %s with name %s, error: %v", logFile.path, logFileName, err)
				continue
			}
		}

		log.Printf("Preserved logs in directory '%s'", logsPath)
	}

	if !h.preserveState {
		var tempDir string
		// Save the logs from being deleted.
		if h.preserveLogs {
			t, err := os.MkdirTemp("", "")
			if err != nil {
				log.Printf("Could not preserve logs, failed to create temporary dir.")
			} else {
				tempDir = t
				defer os.RemoveAll(tempDir)
				err = os.Rename(logsPath, filepath.Join(tempDir, "logs"))
				if err != nil {
					log.Printf("Could not preserve logs, failed to move to temporary dir.")
				}
			}
		}

		var err error
		for _, cleanable := range h.cleanables {
			err = multierr.Append(err, cleanable.Cleanup())
		}

		if err != nil {
			log.Printf("Harness cleanup had errors: %+v", err)
		}

		err = os.RemoveAll(h.Dir)
		if err != nil {
			log.Printf("Failed to clean testing directory '%s'", h.Dir)
		}

		if h.preserveLogs {
			// Move back the preserved logs.
			err = os.Mkdir(h.Dir, os.ModePerm)
			if err != nil {
				log.Printf("Failed to recreate testing directory '%s' to preserve logs.", h.Dir)
			}

			err = os.Rename(filepath.Join(tempDir, "logs"), logsPath)
			if err != nil {
				log.Printf("Failed to preserve logs. Could not put back the logs directory.")
			}
		}
	}

	return err
}
