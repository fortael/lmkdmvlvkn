package vendors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// maxDepth bounds how far below root the walk will descend. Six levels
// comfortably clears the real layouts on this machine — the deepest match
// found under $HOME is ~/GolandProjects/octogent/apps/api/node_modules at
// four — while stopping a pathological or accidentally-generated tree from
// turning a background scan into a hang. Directories at exactly maxDepth are
// still read, so a match can sit one level deeper than the limit itself.
const maxDepth = 6

// topLevelSkips are directories skipped when they appear directly under root,
// and only there. The "only there" matters: ~/Library is macOS application
// state, but app/Library is an ordinary source directory in a Laravel project
// and one of those exists on this machine. A blanket name match would step
// over the user's own code.
//
//   - Library, Applications, .Trash come from the app's own remit — the other
//     tabs already cover them, and .app bundles are code-signed, so touching
//     their contents makes Gatekeeper refuse to launch them.
//   - go is the GOPATH. Its pkg/mod is a content-addressed, read-only module
//     cache; a node_modules vendored inside some module there would be
//     reported as deletable even though `npm ci` cannot put it back where the
//     cache expects it. It is also 2.8 GB of nothing relevant to walk.
//   - sdk holds downloaded Go toolchains, managed by `go install
//     golang.org/dl/...`, not by any manifest.
//   - OrbStack is a FUSE mount exposing the filesystems of every container
//     and VM; walking it leaves the host entirely and can block on a stopped
//     machine. SharedVolumes is the same idea for mounted shares.
//   - Pictures, Music and Movies hold library bundles (.photoslibrary and
//     friends) that are enormous, opaque, and never projects.
var topLevelSkips = map[string]bool{
	"Library":       true,
	"Applications":  true,
	".Trash":        true,
	"go":            true,
	"sdk":           true,
	"OrbStack":      true,
	"SharedVolumes": true,
	"Pictures":      true,
	"Music":         true,
	"Movies":        true,
}

// bundleSuffixes are directory-name suffixes that mark a macOS bundle: a
// directory the Finder presents as a single opaque file. Their insides are
// the vendor's business, are frequently code-signed, and any node_modules in
// there belongs to a shipped application rather than to the user.
var bundleSuffixes = []string{".app", ".photoslibrary", ".framework", ".bundle", ".xcodeproj", ".xcworkspace"}

// walker carries the state the recursion needs.
type walker struct {
	root  string
	items []Item
	// seen prevents a path being reported twice. Two psalm config files in
	// one project (psalm.xml plus psalm.xml.dist) can name the same cache
	// directory, and a duplicate row would let the user "delete" it twice.
	seen map[string]bool
	// pruned holds paths already reported, so the walk never descends into
	// something it has offered to delete. Matched directories are pruned
	// where they are found, but the PHP tool caches are discovered from a
	// config file several levels above where they actually live, which is
	// why this has to be a path set rather than a flag on the recursion.
	pruned map[string]bool
}

// walk reads dir and recurses. depth is dir's distance below root, so root
// itself is 0.
//
// Errors reading an individual directory — permission denied, or a directory
// that vanished mid-walk — are swallowed: a single unreadable folder must not
// cost the user the rest of the scan. Only ctx cancellation stops the walk,
// and it propagates all the way out.
func (w *walker) walk(ctx context.Context, dir string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	// One pass to index the listing, so every manifest check below is a map
	// lookup rather than a stat syscall per candidate directory.
	siblings := make(map[string]bool, len(entries))
	for _, e := range entries {
		siblings[e.Name()] = true
	}

	// psalm and PHPStan are handled off the directory names entirely: both
	// write their cache wherever the project's config file sends them, which
	// on this machine is a hashed subdirectory of storage/tmp. Nothing about
	// those path components is guessable, so the config has to be read.
	w.addPHPToolCaches(dir, siblings)

	for _, e := range entries {
		// DirEntry.IsDir reports the directory entry's own type without
		// following it, so a symlink — even one pointing at a directory, even
		// one pointing at its own ancestor — is skipped here and can never
		// send the walk round in a loop.
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		child := filepath.Join(dir, name)

		if w.pruned[child] {
			continue
		}
		if k, ok := match(dir, name, siblings); ok {
			w.add(Item{
				Kind:        k,
				Path:        child,
				Project:     filepath.Base(dir),
				ProjectPath: dir,
			})
			// Prune. Descending into a matched directory is what makes a
			// naive version of this scan take minutes: node_modules nests
			// node_modules, and every transitive package would be reported
			// as its own row even though deleting the parent removes them
			// all anyway.
			continue
		}
		if w.skip(name, depth) {
			continue
		}
		if depth+1 > maxDepth {
			continue
		}
		if err := w.walk(ctx, child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// skip reports whether a directory that matched no rule should also not be
// descended into. Order matters: this runs after match, so the dot-prefixed
// kinds (.venv, .gradle, .next, .pytest_cache) are already accounted for and
// the hidden-directory rule below cannot swallow them.
func (w *walker) skip(name string, depth int) bool {
	if depth == 0 && topLevelSkips[name] {
		return true
	}
	// Hidden directories are skipped wholesale. This is the rule that keeps
	// the tab honest rather than merely fast: ~/.npm/_npx caches, ~/.vscode
	// and ~/.cursor extensions all ship a package.json next to a
	// node_modules, and without this the user would be shown sixteen
	// "projects" named after npx cache hashes and offered the chance to
	// break their editor. It also covers .git, whose contents are never a
	// project of their own, and every tool dotdir under $HOME.
	//
	// The trade-off is a project kept somewhere like ~/.dotfiles, which this
	// will not find. That is rare, and far cheaper than the alternative.
	if strings.HasPrefix(name, ".") {
		return true
	}
	for _, s := range bundleSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// add records an item unless its path has already been reported, and marks
// the path so the walk will not descend into it later.
func (w *walker) add(it Item) {
	if w.seen[it.Path] {
		return
	}
	w.seen[it.Path] = true
	w.pruned[it.Path] = true
	w.items = append(w.items, it)
}
