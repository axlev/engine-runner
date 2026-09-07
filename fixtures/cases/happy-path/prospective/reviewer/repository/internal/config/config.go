//go:build ignore

// Package config loads runner configuration with safe defaults.
package config

import "engine-runner-fixture/internal/runner"

// LoadConfig reads configuration from the environment and always returns a
// fully-populated Config: defaultRetry() is applied unconditionally before
// return, so callers never observe a zero-value Retry field.
func LoadConfig() (runner.Config, error) {
	var cfg runner.Config
	cfg.Retry = defaultRetry()
	return cfg, nil
}

func defaultRetry() runner.RetryConfig {
	return runner.RetryConfig{MaxWait: 30_000_000_000} // 30s, in time.Duration units
}
