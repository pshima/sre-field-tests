package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsAndRespectsEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("# a comment\nexport SREFT_TEST_A=\"from-dotenv\"\nSREFT_TEST_B = plain\n"), 0o600)

	// A value already in the environment must win over the file.
	t.Setenv("SREFT_TEST_A", "from-env")
	t.Setenv("SREFT_TEST_B", "") // present-but-empty is treated as unset by getenv checks

	loadDotEnv(path)

	if got := os.Getenv("SREFT_TEST_A"); got != "from-env" {
		t.Errorf("SREFT_TEST_A = %q, want from-env (env must win)", got)
	}
	if got := os.Getenv("SREFT_TEST_B"); got != "plain" {
		t.Errorf("SREFT_TEST_B = %q, want plain (quotes/space trimmed, empty env filled)", got)
	}
}

func TestReadRawSecretTrims(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "OPENROUTER_KEY")
	os.WriteFile(path, []byte("  sk-or-example\n"), 0o600)
	if got := readRawSecret(path); got != "sk-or-example" {
		t.Errorf("readRawSecret = %q, want trimmed key", got)
	}
	if got := readRawSecret(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing file should return empty, got %q", got)
	}
}
