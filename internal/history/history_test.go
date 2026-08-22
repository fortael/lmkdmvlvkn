package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate points $HOME at a temp directory so tests never touch the real
// history log.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestAppendThenLoadReturnsNewestFirst(t *testing.T) {
	isolate(t)

	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		err := Append(Record{
			Time:   base.Add(time.Duration(i) * time.Hour),
			Name:   name,
			Path:   "/tmp/" + name,
			Action: ActionClean,
			Freed:  int64(i+1) << 20,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	recs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("loaded %d records, want 3", len(recs))
	}
	if recs[0].Name != "third" || recs[2].Name != "first" {
		t.Errorf("order = %s..%s, want newest (third) first", recs[0].Name, recs[2].Name)
	}
	if got := TotalFreed(recs); got != (1+2+3)<<20 {
		t.Errorf("TotalFreed = %d, want %d", got, (1+2+3)<<20)
	}
}

// Nothing cleaned yet is the normal first-run state, not a failure.
func TestLoadMissingFileIsNotAnError(t *testing.T) {
	isolate(t)
	recs, err := Load()
	if err != nil {
		t.Fatalf("Load on a fresh machine returned %v, want nil", err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records, want none", len(recs))
	}
}

// Being killed mid-write leaves a truncated final line. That must cost at
// most the one record, never the whole history.
func TestLoadSkipsCorruptLines(t *testing.T) {
	isolate(t)

	if err := Append(Record{Name: "good-one", Freed: 100}); err != nil {
		t.Fatal(err)
	}
	path, err := File()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"name\":\"truncated\",\"fre\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := Append(Record{Name: "good-two", Freed: 200}); err != nil {
		t.Fatal(err)
	}

	recs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("loaded %d records, want the 2 intact ones", len(recs))
	}
	if got := TotalFreed(recs); got != 300 {
		t.Errorf("TotalFreed = %d, want 300", got)
	}
}

func TestClearRemovesEverythingAndIsIdempotent(t *testing.T) {
	isolate(t)

	if err := Append(Record{Name: "x", Freed: 1}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	recs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records after Clear, want none", len(recs))
	}
	// Clearing an already-clear history is a no-op, not an error.
	if err := Clear(); err != nil {
		t.Errorf("second Clear returned %v, want nil", err)
	}
}

// Failed cleanups are recorded too — the point of the log is the trail,
// including what did not work — but they must not inflate the total.
func TestTotalFreedIgnoresFailures(t *testing.T) {
	recs := []Record{
		{Name: "ok", Freed: 500},
		{Name: "failed", Freed: 0, Err: "permission denied"},
		{Name: "ok2", Freed: 250},
	}
	if got := TotalFreed(recs); got != 750 {
		t.Errorf("TotalFreed = %d, want 750", got)
	}
}

func TestFileLivesUnderHome(t *testing.T) {
	isolate(t)
	path, err := File()
	if err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	if !strings.HasPrefix(path, home+string(filepath.Separator)) {
		t.Errorf("File() = %q, want it under $HOME (%q)", path, home)
	}
	if filepath.Ext(path) == "" {
		t.Errorf("File() = %q, want a named file", path)
	}
	// The directory is created eagerly so the first Append can't fail on a
	// missing parent.
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("File() did not create its directory: %v", err)
	}
}
