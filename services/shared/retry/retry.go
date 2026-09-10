// Package retry is exponential backoff that respects context cancellation.
//
// Ported from the starter's shared/retry, with one change: the context is
// checked *before* each attempt as well as during the wait, so a cancelled
// context cannot cause one last doomed dial after shutdown has begun.
package retry

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

type Config struct {
	// Attempts includes the first try. Zero means the default.
	Attempts int
	// Initial is the wait after the first failure; it doubles each time.
	Initial time.Duration
	// Max caps the wait between attempts, not the total time.
	Max time.Duration
	// Jitter spreads retries so that N services restarting together do not
	// reconnect in lockstep and hammer a broker that is still coming up.
	Jitter bool
}

var Default = Config{Attempts: 5, Initial: 250 * time.Millisecond, Max: 10 * time.Second, Jitter: true}

func (c Config) withDefaults() Config {
	if c.Attempts <= 0 {
		c.Attempts = Default.Attempts
	}
	if c.Initial <= 0 {
		c.Initial = Default.Initial
	}
	if c.Max <= 0 {
		c.Max = Default.Max
	}
	return c
}

// Do runs work until it succeeds, the attempts are exhausted, or ctx is done.
// The last error is returned, wrapped with how many attempts were made.
func Do(ctx context.Context, config Config, work func(ctx context.Context) error) error {
	config = config.withDefaults()

	wait := config.Initial
	var last error

	for attempt := 1; attempt <= config.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		last = work(ctx)
		if last == nil {
			return nil
		}

		if attempt == config.Attempts {
			break
		}

		delay := wait
		if config.Jitter {
			// Full jitter: uniform in [0, wait). Cheaper on a thundering herd
			// than equal jitter, and the worst case is simply an early retry.
			delay = time.Duration(rand.Int64N(int64(wait)) + 1)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if wait *= 2; wait > config.Max {
			wait = config.Max
		}
	}

	return fmt.Errorf("retry: gave up after %d attempts: %w", config.Attempts, last)
}
