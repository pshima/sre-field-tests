package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obs.jsonl")
	w, err := OpenWriter(path, true)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	ts := time.Unix(1_700_000_000, 0).UTC()
	want := []Record{
		Sample(ts, "cgroup-mem", "orders", MetricMemoryUsage, 250<<20, "By"),
		Event(ts.Add(time.Second), "docker-events", "orders", EventOOMKill, map[string]any{"exit_code": 137}),
	}
	for _, r := range want {
		if err := w.Write(r); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d", len(got), len(want))
	}
	if got[0].Metric != MetricMemoryUsage || got[0].Value != float64(250<<20) {
		t.Errorf("sample mismatch: %+v", got[0])
	}
	if got[1].Event != EventOOMKill {
		t.Errorf("event mismatch: %+v", got[1])
	}
}

// TestTornTrailingLineDropped simulates a crash mid-append: the final line is
// incomplete. ReadAll must return every complete record and silently drop the
// torn tail — the crash-safety guarantee the observer relies on.
func TestTornTrailingLineDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obs.jsonl")
	w, _ := OpenWriter(path, true)
	ts := time.Unix(1_700_000_000, 0).UTC()
	_ = w.Write(Sample(ts, "c", "t", "m", 1, "1"))
	_ = w.Write(Sample(ts, "c", "t", "m", 2, "1"))
	_ = w.Close()

	// Append a torn (newline-less, truncated JSON) record, as a crash would.
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.WriteString(`{"ts":"2023-11`)
	_ = f.Close()

	got, err := ReadAll(mustOpen(t, path))
	if err != nil {
		t.Fatalf("ReadAll should tolerate a torn tail, got err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (torn tail dropped)", len(got))
	}
	if got[1].Value != 2 {
		t.Errorf("last complete record wrong: %+v", got[1])
	}
}

// TestCorruptMiddleLineErrors ensures a corrupt non-final line is a real error,
// not silently dropped — that would hide genuine data loss.
func TestCorruptMiddleLineErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obs.jsonl")
	content := "{\"ts\":\"2023-11-14T22:13:20Z\",\"kind\":\"sample\",\"value\":1}\n" +
		"NOT JSON AT ALL\n" +
		"{\"ts\":\"2023-11-14T22:13:21Z\",\"kind\":\"sample\",\"value\":2}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadAll(mustOpen(t, path))
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("expected corrupt-record error, got %v", err)
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
