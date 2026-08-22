package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSparse creates a file with a large logical size but only one block
// actually allocated, the way virtualization tools create VM disk images.
// Seeking past the end and writing a single byte leaves the skipped range
// unallocated on APFS.
func writeSparse(t *testing.T, path string, logical int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Seek(logical-1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
}

// A sparse file must be measured by the blocks it occupies, not its
// logical length. Getting this wrong made a 12.6 GB OrbStack VM image
// report as 228 GB — more than the disk it sits on.
func TestPathSizeCountsAllocatedBlocksNotLogicalLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.img")
	const logical = 512 << 20 // 512 MiB of mostly-hole

	writeSparse(t, path, logical)

	got, _, err := PathSize(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != logical {
		t.Fatalf("test setup: logical size = %d, want %d", info.Size(), logical)
	}
	// Allow generous headroom for filesystem metadata while still being
	// nowhere near the logical size; on APFS this is a handful of KiB.
	if got >= logical/16 {
		t.Errorf("PathSize = %d, want far below logical size %d (sparse file measured as if fully allocated)", got, logical)
	}
}

// Hard links point at one copy of the data, so a tree containing several
// links to the same content occupies that content's space once. pnpm's
// store hard-links packages into every node_modules, which is what makes
// double-counting here so misleading in practice.
func TestDirSizeCountsHardLinkedContentOnce(t *testing.T) {
	dir := t.TempDir()
	payload := make([]byte, 256<<10) // 256 KiB, comfortably several blocks

	original := filepath.Join(dir, "original.bin")
	if err := os.WriteFile(original, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	single, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"link1.bin", "link2.bin", "link3.bin"} {
		if err := os.Link(original, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	withLinks, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}

	if withLinks != single {
		t.Errorf("DirSize with 3 extra hard links = %d, want unchanged %d (linked content counted more than once)",
			withLinks, single)
	}
}

// The everyday case still has to be right: a tree of ordinary files
// reports at least the bytes written, rounded up to whole blocks.
func TestDirSizeSumsOrdinaryFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	const each = 64 << 10
	for _, p := range []string{
		filepath.Join(dir, "a.bin"),
		filepath.Join(dir, "nested", "b.bin"),
		filepath.Join(sub, "c.bin"),
	} {
		if err := os.WriteFile(p, make([]byte, each), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got < 3*each {
		t.Errorf("DirSize = %d, want at least %d", got, 3*each)
	}
}

// A symlink loop must not send the walk into recursion or inflate the
// total; WalkDir doesn't follow links, and the link's own blocks are all
// that should be counted.
func TestDirSizeIgnoresSymlinkTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.bin")
	if err := os.WriteFile(target, make([]byte, 128<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, filepath.Join(dir, "alias.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}

	after, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The two links themselves may occupy a block or two; what must not
	// happen is the 128 KiB target being counted again.
	if after >= before+(128<<10) {
		t.Errorf("DirSize after adding symlinks = %d, want close to %d (symlink target counted as content)",
			after, before)
	}
}

// List stamps the TYPE label and dictionary root onto every row. The
// System Data tab merges five Library folders whose folder names collide,
// so an entry that lost its root would be described by the wrong
// dictionary section.
func TestListStampsSourceAndRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Google"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := List(dir, "AppSupp", "Application Support")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("List returned %d entries, want 1 (.DS_Store should be skipped)", len(entries))
	}
	e := entries[0]
	if e.Name != "Google" || e.Source != "AppSupp" || e.Root != "Application Support" {
		t.Errorf("entry = {Name:%q Source:%q Root:%q}, want {Google AppSupp Application Support}",
			e.Name, e.Source, e.Root)
	}
	if e.Size != -1 {
		t.Errorf("Size = %d, want -1 (unmeasured until the Scanner runs)", e.Size)
	}
}

// GlobSize backs the "this clean would free X" figure shown before the
// user confirms, so it has to measure the same way DirSize does.
func TestGlobSizeMeasuresMatchesOnly(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep")
	drop := filepath.Join(dir, "drop")
	for _, d := range []string{keep, drop} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.bin"), make([]byte, 96<<10), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := GlobSize(filepath.Join(dir, "drop"))
	if err != nil {
		t.Fatal(err)
	}
	whole, _, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got < 96<<10 {
		t.Errorf("GlobSize(drop) = %d, want at least %d", got, 96<<10)
	}
	if got >= whole {
		t.Errorf("GlobSize(drop) = %d, want less than the whole tree %d (matched more than the pattern)", got, whole)
	}
}

// Enqueue is called from the UI's update loop, so it must never block —
// no matter how far behind the workers are. A bounded queue here
// deadlocked the whole app: the UI froze mid-enqueue, stopped draining
// Results, and every worker then blocked trying to report. The five
// Library roots produce well over a thousand entries, so this is the
// normal case, not an edge case.
func TestEnqueueNeverBlocks(t *testing.T) {
	s := NewScanner(1)
	dir := t.TempDir()

	const jobs = 5000
	done := make(chan struct{})
	go func() {
		for i := 0; i < jobs; i++ {
			s.Enqueue(dir)
		}
		close(done)
	}()

	// Drain concurrently, but far more slowly than we enqueue, so the
	// queue is guaranteed to run well past any plausible buffer size.
	go func() {
		for range s.Results {
		}
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatalf("Enqueue blocked after fewer than %d jobs; the pending queue must be unbounded", jobs)
	}
}

// Everything queued has to come back out, in the face of many workers
// popping from the shared queue at once.
func TestScannerMeasuresEveryQueuedPath(t *testing.T) {
	s := NewScanner(4)
	want := make(map[string]bool)
	for i := 0; i < 50; i++ {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "f.bin"), make([]byte, 8<<10), 0o644); err != nil {
			t.Fatal(err)
		}
		want[dir] = true
		s.Enqueue(dir)
	}

	got := make(map[string]bool, len(want))
	deadline := time.After(30 * time.Second)
	for len(got) < len(want) {
		select {
		case r := <-s.Results:
			if r.Err != nil {
				t.Errorf("measuring %s: %v", r.Path, r.Err)
			}
			if r.Size <= 0 {
				t.Errorf("%s measured as %d bytes, want positive", r.Path, r.Size)
			}
			got[r.Path] = true
		case <-deadline:
			t.Fatalf("only %d of %d results arrived", len(got), len(want))
		}
	}
}
