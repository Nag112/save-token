// Package config loads save-token settings: defaults, a JSON config file,
// and SAVE_TOKEN_* environment overrides. Ported from token-saver
// src/config.py (core keys only for v1).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Config holds every tunable knob used by the v1 pipeline.
type Config struct {
	Enabled bool `json:"enabled"`

	MinInputLength      int     `json:"min_input_length"`
	MinCompressionRatio float64 `json:"min_compression_ratio"`

	WrapTimeout    int `json:"wrap_timeout"`
	MaxOutputBytes int `json:"max_output_bytes"`
	MaxChainDepth  int `json:"max_chain_depth"`

	GenericTruncateThreshold int `json:"generic_truncate_threshold"`
	GenericKeepHead          int `json:"generic_keep_head"`
	GenericKeepTail          int `json:"generic_keep_tail"`
	GenericKeepCritical      int `json:"generic_keep_critical"`
	RecoverCriticalLines     int `json:"recover_critical_lines"`
	MaxDiffHunkLines         int `json:"max_diff_hunk_lines"`
	MaxDiffContextLines      int `json:"max_diff_context_lines"`
	MaxLogEntries            int `json:"max_log_entries"`
	GitBranchThreshold       int `json:"git_branch_threshold"`
	GitStashThreshold        int `json:"git_stash_threshold"`
	LintExampleCount         int `json:"lint_example_count"`
	LintGroupThreshold       int `json:"lint_group_threshold"`
	MaxTracebackLines        int `json:"max_traceback_lines"`

	CharsPerToken int `json:"chars_per_token"`

	DisabledProcessors []string `json:"disabled_processors"`
}

// DefaultConfig returns the built-in defaults (matching token-saver).
func DefaultConfig() *Config {
	return &Config{
		Enabled:                  true,
		MinInputLength:           1,
		MinCompressionRatio:      0.0,
		WrapTimeout:              300,
		MaxOutputBytes:           10_000_000,
		MaxChainDepth:            3,
		GenericTruncateThreshold: 200,
		GenericKeepHead:          100,
		GenericKeepTail:          50,
		GenericKeepCritical:      20,
		RecoverCriticalLines:     20,
		MaxDiffHunkLines:         50,
		MaxDiffContextLines:      3,
		MaxLogEntries:            10,
		GitBranchThreshold:       15,
		GitStashThreshold:        5,
		LintExampleCount:         2,
		LintGroupThreshold:       3,
		MaxTracebackLines:        30,
		CharsPerToken:            4,
	}
}

var (
	once sync.Once
	cfg  *Config
)

// Get returns the process-wide configuration, loaded once.
func Get() *Config {
	once.Do(func() {
		cfg = load()
	})
	return cfg
}

func configDir() string {
	if d := os.Getenv("SAVE_TOKEN_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "save-token")
}

func load() *Config {
	c := DefaultConfig()

	if dir := configDir(); dir != "" {
		path := filepath.Join(dir, "config.json")
		if data, err := os.ReadFile(path); err == nil {
			// json.Unmarshal into the struct leaves unset keys at their
			// current (default) values, matching merge-with-defaults.
			_ = json.Unmarshal(data, c)
		}
	}

	applyEnv(c)
	return c
}

// applyEnv applies SAVE_TOKEN_<KEY> overrides with type coercion matching
// the Python _coerce_value rules (bool before int; numeric strings; comma
// lists).
func applyEnv(c *Config) {
	setBool := func(key string, dst *bool) {
		if v, ok := os.LookupEnv("SAVE_TOKEN_" + key); ok {
			if b, err := parseBool(v); err == nil {
				*dst = b
			}
		}
	}
	setInt := func(key string, dst *int) {
		if v, ok := os.LookupEnv("SAVE_TOKEN_" + key); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				*dst = n
			}
		}
	}
	setFloat := func(key string, dst *float64) {
		if v, ok := os.LookupEnv("SAVE_TOKEN_" + key); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				*dst = f
			}
		}
	}

	setBool("ENABLED", &c.Enabled)
	setInt("MIN_INPUT_LENGTH", &c.MinInputLength)
	setFloat("MIN_COMPRESSION_RATIO", &c.MinCompressionRatio)
	setInt("WRAP_TIMEOUT", &c.WrapTimeout)
	setInt("MAX_OUTPUT_BYTES", &c.MaxOutputBytes)
	setInt("MAX_CHAIN_DEPTH", &c.MaxChainDepth)
	setInt("GENERIC_TRUNCATE_THRESHOLD", &c.GenericTruncateThreshold)
	setInt("GENERIC_KEEP_HEAD", &c.GenericKeepHead)
	setInt("GENERIC_KEEP_TAIL", &c.GenericKeepTail)
	setInt("GENERIC_KEEP_CRITICAL", &c.GenericKeepCritical)
	setInt("RECOVER_CRITICAL_LINES", &c.RecoverCriticalLines)
	setInt("MAX_DIFF_HUNK_LINES", &c.MaxDiffHunkLines)
	setInt("MAX_DIFF_CONTEXT_LINES", &c.MaxDiffContextLines)
	setInt("MAX_LOG_ENTRIES", &c.MaxLogEntries)
	setInt("GIT_BRANCH_THRESHOLD", &c.GitBranchThreshold)
	setInt("GIT_STASH_THRESHOLD", &c.GitStashThreshold)
	setInt("LINT_EXAMPLE_COUNT", &c.LintExampleCount)
	setInt("LINT_GROUP_THRESHOLD", &c.LintGroupThreshold)
	setInt("MAX_TRACEBACK_LINES", &c.MaxTracebackLines)
	setInt("CHARS_PER_TOKEN", &c.CharsPerToken)
	if v, ok := os.LookupEnv("SAVE_TOKEN_DISABLED_PROCESSORS"); ok {
		var list []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				list = append(list, p)
			}
		}
		c.DisabledProcessors = list
	}
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, strconv.ErrSyntax
}
