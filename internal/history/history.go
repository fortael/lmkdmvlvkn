// Package history is the persistent record of everything this app has
// deleted: what, where, when, and how much it freed.
//
// It exists so the Results tab can answer "what has this thing actually
// done for me", and so a cleanup that turns out to have been a mistake
// leaves a trail naming the exact path. It is append-only and never
// consulted to decide what to delete — purely a record.
package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Action records which of the app's three removal mechanisms ran.
type Action string

const (
	ActionClean  Action = "clean"
	ActionNative Action = "native"
	ActionDelete Action = "delete"
)

// Record is one deletion.
type Record struct {
	Time   time.Time `json:"time"`
	Name   string    `json:"name"`
	Path   string    `json:"path"`
	Source string    `json:"source"` // which tab it came from
	Action Action    `json:"action"`
	Freed  int64     `json:"freed_bytes"`
	Err    string    `json:"error,omitempty"`
}

// dirName is the app's state directory under $HOME.
const dirName = ".maccleaner"

// fileName is JSON Lines rather than one JSON document.
//
// The log is append-only and grows without bound, and the process can be
// killed mid-cleanup at any moment. Appending one self-contained object
// per line is an O(1) write that cannot corrupt earlier entries, whereas
// maintaining a single top-level array means reading, re-encoding and
// rewriting the whole file on every deletion — and losing the lot if the
// write is interrupted. Each line is still ordinary JSON.
const fileName = "history.jsonl"

// File returns the log's path, creating its directory if needed.
func File() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Append adds one record. A failure here is reported but must never
// abort a cleanup that has already happened — the caller treats it as a
// warning, since the deletion is done either way.
func Append(r Record) error {
	path, err := File()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Load reads the whole log, newest first. A truncated or corrupt line —
// the expected result of being killed mid-write — is skipped rather than
// failing the read, so one bad line never hides the entire history.
func Load() ([]Record, error) {
	path, err := File()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // nothing cleaned yet is not an error
		}
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	// Paths can be long; the default 64KB token limit is generous but a
	// pathological line shouldn't abort the scan.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		// Return what parsed rather than nothing: a partial history is
		// still worth showing.
		return out, err
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Clear removes the log entirely.
func Clear() error {
	path, err := File()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// TotalFreed sums what the recorded cleanups actually freed. Failed
// entries are included only for the bytes they managed to free, which is
// normally zero.
func TotalFreed(recs []Record) int64 {
	var total int64
	for _, r := range recs {
		if r.Freed > 0 {
			total += r.Freed
		}
	}
	return total
}
