// Package vendors finds reinstallable dependency directories — node_modules,
// Composer vendor, Cargo target, Python virtualenvs, static-analysis caches —
// inside the user's own project trees under $HOME.
//
// The premise is that everything reported here can be recreated by a single
// command from a manifest that is committed to the repository, so the only
// question left for the user is "am I still working on this project?". That
// is why an Item carries the owning project's name and a ModTime derived
// from the project's own source rather than from the dependency directory:
// a node_modules installed a year ago in a project edited yesterday is not
// junk, while a project untouched for two years is exactly what should go.
// See modtime.go — getting that backwards makes the whole tab misleading.
//
// This package only finds paths. Sizes are measured separately by
// internal/scan.Scanner, off the UI loop.
package vendors

import (
	"context"
	"path/filepath"
	"sort"
	"time"
)

// Kind identifies a category of reinstallable directory.
type Kind struct {
	// Name is the short label shown in the TYPE column, e.g. "node", "php".
	// Kept to at most 7 characters so the table never truncates it.
	Name string
	// Dir is the directory name matched, e.g. "node_modules". For the
	// config-driven PHP tool caches there is no fixed name — psalm and
	// PHPStan put their cache wherever the project's config points them —
	// so Dir holds that configured path relative to the project instead.
	Dir string
	// Restore is the literal command that recreates the directory from the
	// manifest, e.g. "npm ci". Shown to the user so they know the cost of
	// deleting. It is refined per project where the honest answer depends
	// on which lock file is present: "npm ci" simply fails in a project
	// that has no package-lock.json, and promising it there would be a lie.
	Restore string
	// Manifest is the sibling file that must exist for a match to count,
	// e.g. "package.json". Empty means no manifest check — those kinds are
	// gated on a marker file inside the directory instead, or carry a name
	// that no tool other than the owning one ever creates.
	Manifest string
}

// Item is one reinstallable directory found inside a project.
type Item struct {
	Kind Kind
	// Path is the absolute path of the directory to delete.
	Path string
	// Project is the display name of the owning project — the directory
	// holding the manifest.
	Project string
	// ProjectPath is that project directory's absolute path.
	ProjectPath string
	// ModTime is the most recent modification anywhere in the project's own
	// source, NOT inside the dependency directory. See projectModTime.
	ModTime time.Time
}

// Scan walks root looking for reinstallable dependency directories.
// It is expected to run in the background; ctx bounds it.
//
// Items already found are returned alongside a non-nil error, so a scan cut
// short by a deadline still shows the user what it managed to reach rather
// than nothing at all.
//
// The result is sorted stalest-project-first, which is the order the user
// wants to work down: the whole point of the tab is finding projects nobody
// has touched in a year. Ties break on path so the order is stable across
// runs.
func Scan(ctx context.Context, root string) ([]Item, error) {
	w := &walker{
		root:   filepath.Clean(root),
		seen:   make(map[string]bool),
		pruned: make(map[string]bool),
	}
	err := w.walk(ctx, w.root, 0)
	// ModTime is stamped after the walk rather than during it, because a
	// project's source scan has to exclude every dependency directory found
	// in that project, and the last of them is only known once the walk has
	// passed the project by.
	w.stampModTimes(ctx)
	sort.Slice(w.items, func(i, j int) bool {
		a, b := w.items[i], w.items[j]
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.Before(b.ModTime)
		}
		return a.Path < b.Path
	})
	return w.items, err
}

// Kinds returns every kind this package can report, for a UI legend or help
// screen. The PHP tool caches are described by their default shape, since
// their real Dir is only known once a project's config has been read.
func Kinds() []Kind {
	out := make([]Kind, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Kind)
	}
	out = append(out, psalmKind("psalm.xml", "<cacheDirectory>"), phpstanKind("phpstan.neon", "<tmpDir>"))
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Dir < out[j].Dir
	})
	return out
}
