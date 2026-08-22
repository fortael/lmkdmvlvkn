// Package scan discovers and measures directories on disk (e.g. under
// ~/Library/Caches) without blocking the caller: listing is fast (a single
// os.ReadDir + Stat per entry), while the expensive recursive size
// computation is handed off to a throttled worker pool via Scanner.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// diskUsage reports how much space info actually occupies on disk, which is
// what matters for "how much will I get back if I delete this" — not
// info.Size(), the logical/apparent length.
//
// The two diverge wildly on sparse files, which macOS uses for VM disk
// images: OrbStack's data.img.raw reports a 228 GB logical size while
// occupying 12.6 GB of real blocks. Summing Size() there would claim a
// single file fills a 228 GB disk. APFS also compresses files
// transparently, where allocated blocks are again smaller than the logical
// size.
//
// Stat_t.Blocks is in 512-byte units on Darwin regardless of the
// filesystem's own block size — the same figure du(1) reports. If the
// platform-specific stat isn't available for some reason, fall back to the
// logical size rather than counting the file as zero.
func diskUsage(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}

// fileID uniquely identifies a file on disk, so a tree walk can count
// hard-linked content once instead of once per link. pnpm's store is the
// motivating case: it hard-links the same package blobs into every
// project's node_modules, so naive summing reports the same bytes many
// times over. This mirrors du(1), which also counts a multiply-linked inode
// only the first time it sees it.
type fileID struct {
	dev uint64
	ino uint64
}

// statID returns info's identity and whether it's hard-linked at all.
// Files with a single link can skip the bookkeeping entirely — that's the
// overwhelming majority of them.
func statID(info fs.FileInfo) (id fileID, linked bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink <= 1 {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: st.Ino}, true
}

// Entry describes one item found under a scanned root.
type Entry struct {
	Name string
	Path string
	// Source is the short TYPE-column label for the root this came from,
	// e.g. "Cache" or "AppSupp".
	Source string
	// Root identifies which dictionary answers for this entry. It is a
	// plain string rather than a knowledge.Root so this package stays
	// independent of the dictionary; the UI converts it back. The System
	// Data tab merges several roots into one table, and the same folder
	// name means different things in each — Caches/Google is a disposable
	// disk cache, Application Support/Google is the Chrome profile — so
	// an entry that lost track of its root would be looked up against the
	// wrong answers.
	Root      string
	ModTime   time.Time
	IsDir     bool
	Size      int64 // -1 until computed by the Scanner
	SizeErr   error
	SizeReady bool
}

// List returns the immediate children of dir as unsized Entry values.
// source is the short TYPE-column label and root is the dictionary key,
// both stamped onto every entry so they survive into the merged System
// Data table.
func List(dir, source, root string) ([]*Entry, error) {
	items, err := os.ReadDir(dir)
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
			Path:    filepath.Join(dir, it.Name()),
			Source:  source,
			Root:    root,
			ModTime: info.ModTime(),
			IsDir:   it.IsDir(),
			Size:    -1,
		})
	}
	return entries, nil
}

// DirSize walks path recursively and sums how much space the tree actually
// occupies on disk (see diskUsage), also returning the most recent
// modification time seen anywhere in it. Hard-linked files are counted once.
// Errors on individual entries (permission denied, broken symlinks, races
// with files disappearing mid-walk) are skipped rather than aborting the
// whole scan.
//
// Directories themselves contribute their own allocated blocks, matching
// du(1) — a tree of many small directories isn't free.
func DirSize(path string) (size int64, latest time.Time, err error) {
	var seen map[fileID]struct{}
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if t := info.ModTime(); t.After(latest) {
			latest = t
		}
		// Symlinks are not followed by WalkDir; counting the link's own
		// blocks (not its target's) is both what du does and what keeps a
		// symlink loop from inflating the total.
		if id, linked := statID(info); linked {
			if seen == nil {
				seen = make(map[fileID]struct{})
			}
			if _, dup := seen[id]; dup {
				return nil
			}
			seen[id] = struct{}{}
		}
		size += diskUsage(info)
		return nil
	})
	return size, latest, walkErr
}

// PathSize measures a single path whether it's a file or a directory,
// returning its on-disk usage and newest mtime. Callers that have a bare
// path and don't already know which it is (the curated Home-tab list, the
// Applications tab) should use this rather than branching themselves.
func PathSize(path string) (size int64, latest time.Time, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, time.Time{}, err
	}
	if info.IsDir() {
		return DirSize(path)
	}
	return diskUsage(info), info.ModTime(), nil
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
		size, _, err := PathSize(m)
		if err != nil {
			continue
		}
		total += size
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
// pool of workers so a large listing doesn't saturate the disk or CPU.
// Callers enqueue paths and receive results asynchronously off Results.
//
// The pending queue is unbounded, and deliberately so. Enqueue is called
// from the UI's update loop, which must never block: a bounded channel
// deadlocks the whole app once it fills, because the blocked UI then stops
// draining Results, which in turn blocks every worker. That is not
// hypothetical — the five Library roots produce well over a thousand
// entries between them, several times any buffer worth hard-coding.
type Scanner struct {
	mu      sync.Mutex
	wake    *sync.Cond
	pending []string
	Results chan SizeResult
}

// NewScanner starts workers goroutines pulling from the pending queue. A
// short pause between jobs on each worker keeps the scan background-
// friendly rather than maximizing throughput.
func NewScanner(workers int) *Scanner {
	if workers < 1 {
		workers = 1
	}
	s := &Scanner{Results: make(chan SizeResult, 512)}
	s.wake = sync.NewCond(&s.mu)
	for i := 0; i < workers; i++ {
		go s.run()
	}
	return s
}

// next pops the oldest pending path, sleeping until one is available.
func (s *Scanner) next() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.pending) == 0 {
		s.wake.Wait()
	}
	path := s.pending[0]
	// Release the drained backing array rather than re-slicing forever,
	// so repeated rescans don't retain every path ever queued.
	if len(s.pending) == 1 {
		s.pending = nil
	} else {
		s.pending = s.pending[1:]
	}
	return path
}

func (s *Scanner) run() {
	for {
		path := s.next()
		size, latest, err := PathSize(path)
		s.Results <- SizeResult{Path: path, Size: size, ModTime: latest, Err: err}
		time.Sleep(5 * time.Millisecond)
	}
}

// Enqueue schedules path for background size computation. It never blocks,
// however far behind the workers are.
func (s *Scanner) Enqueue(path string) {
	s.mu.Lock()
	s.pending = append(s.pending, path)
	s.mu.Unlock()
	s.wake.Signal()
}

// DiskUsage describes a mounted volume's capacity, in bytes.
type DiskUsage struct {
	Total int64
	Free  int64
	Used  int64
}

// Volume reports the capacity of the filesystem containing path, for the
// "freed X of Y" progress bar.
//
// Free uses Bavail (blocks available to an unprivileged process) rather
// than Bfree, which counts the root-only reserve as free and would
// overstate what the user can actually use.
func Volume(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, err
	}
	bs := int64(st.Bsize)
	total := int64(st.Blocks) * bs
	free := int64(st.Bavail) * bs
	return DiskUsage{Total: total, Free: free, Used: total - free}, nil
}
