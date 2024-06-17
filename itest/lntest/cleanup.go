package lntest

import "log"

type CleanupFunc func() error

type Cleanup struct {
	Name string
	Fn   CleanupFunc
}

// Runs the cleanup functions in reverse order.
func PerformCleanup(cleanups []*Cleanup) {
	if cleanups == nil {
		return
	}

	for i := len(cleanups) - 1; i >= 0; i-- {
		c := cleanups[i]

		if c == nil || c.Fn == nil {
			continue
		}

		err := c.Fn()
		if err != nil {
			log.Printf("Cleanup '%s' error: %v", c.Name, err)
		}
	}
}
