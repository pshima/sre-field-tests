package observe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Writer is an append-only, crash-safe sink for Records. Each Write serializes
// one record, appends it with a trailing newline, and (optionally) fsyncs, so a
// crash at any point leaves at most one torn trailing line — which Reader
// discards. The file is opened O_APPEND so concurrent writers never interleave
// mid-record at the OS level for writes under PIPE_BUF.
//
// It is deliberately dependency-free and does minimal allocation: this sink has
// to keep working while the observed host is under resource pressure.
type Writer struct {
	mu   sync.Mutex
	f    *os.File
	bw   *bufio.Writer
	sync bool
}

// OpenWriter opens (creating if needed) path for append. If syncEach is true,
// every Write is fsync'd for maximum durability under a hostile host; set false
// for lower-overhead batched durability.
func OpenWriter(path string, syncEach bool) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open observer stream: %w", err)
	}
	return &Writer{f: f, bw: bufio.NewWriterSize(f, 64<<10), sync: syncEach}, nil
}

// Write appends one record as a single JSON line.
func (w *Writer) Write(r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.bw.Write(line); err != nil {
		return err
	}
	if err := w.bw.WriteByte('\n'); err != nil {
		return err
	}
	if w.sync {
		if err := w.bw.Flush(); err != nil {
			return err
		}
		return w.f.Sync()
	}
	return nil
}

// Flush flushes the buffer and fsyncs. Call periodically when syncEach is false.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.bw.Flush(); err != nil {
		return err
	}
	return w.f.Sync()
}

// Close flushes and closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.bw.Flush(); err != nil {
		_ = w.f.Close()
		return err
	}
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}

// ReadAll reads every well-formed record from a JSONL stream, silently skipping
// a torn trailing line (the expected artifact of a crash mid-append). Malformed
// non-final lines are returned as an error since they indicate real corruption.
func ReadAll(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var out []Record
	var pending []byte
	haveP := false
	flush := func() error {
		if !haveP {
			return nil
		}
		var rec Record
		if err := json.Unmarshal(pending, &rec); err != nil {
			return fmt.Errorf("corrupt record: %w", err)
		}
		out = append(out, rec)
		haveP = false
		return nil
	}
	for sc.Scan() {
		// Defer parsing by one line so the last (possibly torn) line can be
		// dropped without erroring.
		if err := flush(); err != nil {
			return out, err
		}
		line := sc.Bytes()
		pending = append(pending[:0], line...)
		haveP = true
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	// Parse the final buffered line; if it is torn, drop it silently.
	if haveP {
		var rec Record
		if err := json.Unmarshal(pending, &rec); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

// ReadFile is a convenience wrapper around ReadAll for a file path.
func ReadFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadAll(f)
}
