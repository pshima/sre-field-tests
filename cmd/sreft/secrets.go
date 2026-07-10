package main

import (
	"bufio"
	"log/slog"
	"os"
	"strings"
)

// loadLocalSecrets makes API keys available to the tooling from gitignored local
// files, so a run "just works" once the operator drops a key in place (issue #5).
// Precedence, highest first:
//
//  1. an already-set environment variable (never overridden);
//  2. a KEY=VALUE line in a local .env file;
//  3. a raw key in an OPENROUTER_KEY file (the whole file is the key).
//
// Values are never logged. All three sources are gitignored.
func loadLocalSecrets(log *slog.Logger) {
	loadDotEnv(".env")

	// Fallback: a raw single-secret file (OPENROUTER_KEY holds just the key).
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		if v := readRawSecret("OPENROUTER_KEY"); v != "" {
			_ = os.Setenv("OPENROUTER_API_KEY", v)
		}
	}

	if os.Getenv("OPENROUTER_API_KEY") != "" {
		log.Debug("OPENROUTER_API_KEY is set (value not logged)")
	}
}

// loadDotEnv parses a simple KEY=VALUE .env file and sets any variable not
// already present in the environment. Missing file is not an error.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k == "" || os.Getenv(k) != "" {
			continue // don't override an explicit environment value
		}
		_ = os.Setenv(k, v)
	}
}

// readRawSecret returns the trimmed contents of a single-secret file, or "".
func readRawSecret(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
