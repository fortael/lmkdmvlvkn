package vendors

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// sourceDepth bounds how far into a project the source scan behind ModTime
// descends. Four levels reaches app/Http/Controllers/Api in a Laravel service
// and src/main/java/com in a Gradle one, which is where the code actually is,
// while the dependency directories that hold the file counts worth worrying
// about are stepped over entirely.
const sourceDepth = 4

// gitDir is excluded from the source scan. Its mtime moves whenever an
// editor runs a background fetch or a language server refreshes an index,
// none of which is the user working on the project — and everything that is
// (editing, committing, checking out a branch) writes to the working tree
// first, so nothing is lost by ignoring it.
const gitDir = ".git"

// stampModTimes fills in Item.ModTime for every item found, computing each
// project's figure once however many dependency directories it turned up.
func (w *walker) stampModTimes(ctx context.Context) {
	cache := make(map[string]time.Time)
	for i := range w.items {
		project := w.items[i].ProjectPath
		mt, done := cache[project]
		if !done {
			mt = w.projectModTime(ctx, project)
			cache[project] = mt
		}
		w.items[i].ModTime = mt
	}
}

// projectModTime answers "when did the user last work on this project?" as
// the newest modification time anywhere in the project's own source.
//
// It deliberately ignores the dependency directories themselves, and that
// distinction is the whole feature. A node_modules carries the mtime of the
// last `npm install`, which says when packages were fetched and nothing at
// all about whether the project is still in use: a tree installed a year ago
// under a project edited yesterday would be reported as a year stale, and the
// user would delete the dependencies of something they are working on this
// week. The inverse is just as bad — reinstalling dependencies in a project
// abandoned two years ago resets its apparent age to today and hides the one
// directory that most deserved deleting.
//
// So the figure comes from the manifest and the source around it, with every
// reported dependency directory, every directory any rule matches, and .git
// excluded. What is left is the files the user writes by hand, which is
// exactly the thing being asked about.
func (w *walker) projectModTime(ctx context.Context, projectDir string) time.Time {
	var latest time.Time
	// The project directory's own mtime counts: it moves when a file is
	// added or removed at the top level, and it is the only reading
	// available if the directory turns out to be unreadable below.
	if info, err := os.Lstat(projectDir); err == nil {
		latest = info.ModTime()
	}
	w.sourceModTime(ctx, projectDir, 0, &latest)
	return latest
}

// sourceModTime walks the project's source, raising latest as it goes.
// Unreadable directories are skipped in silence, as everywhere else here.
func (w *walker) sourceModTime(ctx context.Context, dir string, depth int, latest *time.Time) {
	if ctx.Err() != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		isDir := e.IsDir() // false for symlinks, which are never followed
		if isDir && (name == gitDir || ruleDirs[name] || w.pruned[path]) {
			continue
		}
		// Info on a DirEntry does not follow symlinks, so a link contributes
		// its own timestamp rather than its target's.
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime(); t.After(*latest) {
			*latest = t
		}
		if isDir && depth+1 <= sourceDepth {
			w.sourceModTime(ctx, path, depth+1, latest)
		}
	}
}
