// Package scan discovers and measures directories on disk (e.g. under
// ~/Library/Caches) without blocking the caller: listing is fast (a single
// os.ReadDir + Stat per entry), while the expensive recursive size
// computation is handed off to a throttled worker pool via Scanner.
package scan

import (
	"os"
	"path/filepath"
	"time"
)

// Entry describes one top-level item found under a scanned root.
type Entry struct {
	Name      string
	Path      string
	Source    string // short label for which root this came from, e.g. "Cache"
	ModTime   time.Time
	IsDir     bool
	Size      int64 // -1 until computed by the Scanner
	SizeErr   error
	SizeReady bool
}

// List returns the immediate children of root as unsized Entry values.
// source is a short human-readable label (e.g. "Cache") recorded on each
// entry to identify which scanned root it came from — System Data will
// eventually aggregate several roots (Caches, Logs, Application Support,
// ...) into one table.
func List(root, source string) ([]*Entry, error) {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	entries := make([]*Entry, 0, len(items))
	for _, it := range items {
		if it.Name() == ".DS_Store" {
			continue
		}
		info, err := it.Info()
		if err != nil {
			continue
		}
		entries = append(entries, &Entry{
			Name:    it.Name(),
			Path:    filepath.Join(root, it.Name()),
			Source:  source,
			ModTime: info.ModTime(),
			IsDir:   it.IsDir(),
			Size:    -1,
		})
	}
	return entries, nil
}

// DirSize walks path recursively and sums file sizes, also returning the
// most recent modification time seen anywhere in the tree. Errors on
// individual entries (permission denied, broken symlinks, races with files
// disappearing mid-walk) are skipped rather than aborting the whole scan.
func DirSize(path string) (size int64, latest time.Time, err error) {
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			size += info.Size()
		}
		if t := info.ModTime(); t.After(latest) {
			latest = t
		}
		return nil
	})
	return size, latest, walkErr
}

// GlobSize sums the size of everything matching pattern (a filepath.Glob
// pattern): file sizes are taken directly, directory matches are walked
// recursively via DirSize. Used to estimate how much space a granular,
// glob-based clean action (a knowledge.Entry's CleanPaths) would actually
// free, before running it.
func GlobSize(pattern string) (int64, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, m := range matches {
		info, err := os.Lstat(m)
		if err != nil {
			continue
		}
		if info.IsDir() {
			size, _, _ := DirSize(m)
			total += size
			continue
		}
		total += info.Size()
	}
	return total, nil
}

// SizeResult is delivered on Scanner.Results once a queued path has been
// measured.
type SizeResult struct {
	Path    string
	Size    int64
	ModTime time.Time
	Err     error
}

// Scanner measures directory sizes in the background using a small, fixed
// pool of workers so a large cache listing doesn't saturate the disk or
// CPU. Callers enqueue paths and receive results asynchronously off
// Results.
type Scanner struct {
	jobs    chan string
	Results chan SizeResult
}

// NewScanner starts workers goroutines pulling from an internal job queue.
// A short pause between jobs on each worker keeps the scan background-
// friendly rather than maximizing throughput.
func NewScanner(workers int) *Scanner {
	if workers < 1 {
		workers = 1
	}
	s := &Scanner{
		jobs:    make(chan string, 512),
		Results: make(chan SizeResult, 512),
	}
	for i := 0; i < workers; i++ {
		go s.run()
	}
	return s
}

func (s *Scanner) run() {
	for path := range s.jobs {
		size, latest, err := DirSize(path)
		s.Results <- SizeResult{Path: path, Size: size, ModTime: latest, Err: err}
		time.Sleep(15 * time.Millisecond)
	}
}

// Enqueue schedules path for background size computation.
func (s *Scanner) Enqueue(path string) {
	s.jobs <- path
}
