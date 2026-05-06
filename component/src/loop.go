package component

import (
	"context"
	"time"
)

// RunControlLoop executes step until running() becomes false or context is canceled.
func RunControlLoop(ctx context.Context, running func() bool, intervalSec float64, step func(context.Context)) {
	for running() {
		step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(intervalSec * float64(time.Second))):
		}
	}
}

// ShouldRunInterval checks whether now-last is enough to run and updates last.
func ShouldRunInterval(now float64, last *float64, intervalSec float64) bool {
	if now-*last < intervalSec {
		return false
	}
	*last = now
	return true
}
