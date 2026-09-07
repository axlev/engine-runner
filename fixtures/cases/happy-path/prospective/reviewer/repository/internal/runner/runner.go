//go:build ignore

// Package runner executes a single bench stage with a bounded retry policy.
package runner

import "time"

type Config struct {
	Retry RetryConfig
}

type RetryConfig struct {
	MaxWait time.Duration
}

// Execute runs fn, retrying according to cfg.Retry until it succeeds or the
// configured wait budget is exhausted.
func Execute(cfg Config, fn func() error) error {
	timeout := cfg.Retry.MaxWait
	deadline := time.Now().Add(timeout)
	var err error
	for time.Now().Before(deadline) {
		if err = fn(); err == nil {
			return nil
		}
	}
	return err
}
