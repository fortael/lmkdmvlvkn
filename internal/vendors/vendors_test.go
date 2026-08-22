package vendors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write creates a file, and any parent directories it needs, with the given
// contents. Most fixtures only care that the file exists.
func write(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkdir creates a directory and returns its path.
func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// project builds a minimal project: a directory holding manifest, with one
// populated dependency directory beside it.
func project(t *testing.T, dir, manifest, depDir string) string {
	t.Helper()
	mkdir(t, dir)
	write(t, filepath.Join(dir, manifest), "{}")
	write(t, filepath.Join(dir, depDir, "somepackage", "index.js"), "// installed")
	return dir
}

func scanned(t *testing.T, root string) []Item {
	t.Helper()
	items, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return items
}

func hasPath(items []Item, path string) bool {
	for _, it := range items {
		if it.Path == path {
			return true
		}
	}
	return false
}

func itemAt(t *testing.T, items []Item, path string) Item {
	t.Helper()
	for _, it := range items {
		if it.Path == path {
			return it
		}
	}
	t.Fatalf("no item for %s; got %v", path, pathsOf(items))
	return Item{}
}

func pathsOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Path)
	}
	return out
}

// setTreeTimes backdates everything under root, so a fixture can then touch
// only the parts it wants to look recent.
func setTreeTimes(t *testing.T, root string, tm time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, tm, tm)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The manifest gate on vendor/ is the single most important check in this
// package. Go projects keep a vendor/ directory too, and there are far more
// go.mod files than composer.json files on a typical developer's machine.
// Reporting a Go vendor/ as "restorable with composer install" would hand
// the user a command that cannot possibly work.
func TestVendorRequiresComposerManifest(t *testing.T) {
	root := t.TempDir()
	goProject := project(t, filepath.Join(root, "go-service"), "go.mod", "vendor")
	phpProject := project(t, filepath.Join(root, "php-service"), "composer.json", "vendor")

	items := scanned(t, root)

	if hasPath(items, filepath.Join(goProject, "vendor")) {
		t.Error("a Go vendor/ was reported as reinstallable; only composer.json makes vendor/ a PHP dependency directory")
	}
	php := itemAt(t, items, filepath.Join(phpProject, "vendor"))
	if php.Kind.Name != "php" || php.Kind.Restore != "composer install" {
		t.Errorf("PHP vendor kind = {Name:%q Restore:%q}, want {php composer install}", php.Kind.Name, php.Kind.Restore)
	}
	if php.Project != "php-service" || php.ProjectPath != phpProject {
		t.Errorf("project = {%q %q}, want {php-service %s}", php.Project, php.ProjectPath, phpProject)
	}
}

// node_modules without a package.json beside it is not a project's
// dependency tree — it is something a tool unpacked, and npm ci would have
// nothing to read.
func TestNodeModulesRequiresPackageJSON(t *testing.T) {
	root := t.TempDir()
	orphan := mkdir(t, root, "not-a-project")
	write(t, filepath.Join(orphan, "node_modules", "left", "over.js"), "x")
	real := project(t, filepath.Join(root, "app"), "package.json", "node_modules")

	items := scanned(t, root)

	if hasPath(items, filepath.Join(orphan, "node_modules")) {
		t.Error("node_modules with no package.json beside it was reported")
	}
	if !hasPath(items, filepath.Join(real, "node_modules")) {
		t.Errorf("node_modules with a package.json was not reported; got %v", pathsOf(items))
	}
}

// Every package inside node_modules carries its own package.json, and many
// carry their own node_modules. Without pruning, one project would produce
// thousands of rows for directories that all disappear when the top one is
// deleted — and the walk would take minutes to produce them.
func TestNestedDependencyDirectoriesArePruned(t *testing.T) {
	root := t.TempDir()
	app := project(t, filepath.Join(root, "app"), "package.json", "node_modules")
	nested := filepath.Join(app, "node_modules", "some-lib")
	write(t, filepath.Join(nested, "package.json"), "{}")
	write(t, filepath.Join(nested, "node_modules", "dep", "index.js"), "x")

	items := scanned(t, root)

	if len(items) != 1 {
		t.Errorf("got %d items, want 1; nested dependency directories must be pruned, not reported: %v",
			len(items), pathsOf(items))
	}
	if !hasPath(items, filepath.Join(app, "node_modules")) {
		t.Errorf("the top-level node_modules is the one to report; got %v", pathsOf(items))
	}
}

// A pathological or generated tree must not be able to hang a background
// scan, so the walk stops descending past maxDepth.
func TestDepthLimit(t *testing.T) {
	root := t.TempDir()
	// maxDepth is 6, so a project directory sitting exactly that far below
	// root is still read, and one level deeper is not.
	atLimit := project(t, filepath.Join(root, "a", "b", "c", "d", "e", "reachable"), "package.json", "node_modules")
	tooDeep := project(t, filepath.Join(root, "a", "b", "c", "d", "e", "f", "buried"), "package.json", "node_modules")

	items := scanned(t, root)

	if !hasPath(items, filepath.Join(atLimit, "node_modules")) {
		t.Errorf("a project %d levels below root should be reached; got %v", maxDepth, pathsOf(items))
	}
	if hasPath(items, filepath.Join(tooDeep, "node_modules")) {
		t.Errorf("a project %d levels below root is past the depth limit and must not be walked", maxDepth+1)
	}
}

// Following symlinks would let a link pointing at its own ancestor spin the
// walk forever, and would report directories that do not live under root at
// all. The test passing at all is half the assertion — an infinite walk
// would hang it.
func TestSymlinksAreNotFollowed(t *testing.T) {
	outside := t.TempDir()
	project(t, filepath.Join(outside, "elsewhere"), "package.json", "node_modules")

	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link-to-outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	visible := project(t, filepath.Join(root, "real"), "package.json", "node_modules")

	done := make(chan []Item, 1)
	go func() { done <- scanned(t, root) }()

	select {
	case items := <-done:
		// Exactly one item, and it is the directory genuinely under root.
		// Following the links would add the project behind link-to-outside
		// (reported under a path inside root, so a prefix check would miss
		// it) and a copy of everything for each turn around the loop.
		if len(items) != 1 || items[0].Path != filepath.Join(visible, "node_modules") {
			t.Errorf("got %v, want only %s; symlinks must not be traversed",
				pathsOf(items), filepath.Join(visible, "node_modules"))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Scan did not finish; a symlink loop was followed")
	}
}

// This is the difference between the feature being useful and being actively
// misleading. node_modules carries the date of the last install, which says
// nothing about whether the project is still in use. A tree installed today
// in a project last edited two years ago is exactly what the user wants to
// delete, and reporting it as fresh would hide it.
func TestModTimeComesFromProjectSourceNotDependencies(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "abandoned")
	write(t, filepath.Join(app, "package.json"), "{}")
	write(t, filepath.Join(app, "src", "index.js"), "// last touched long ago")
	write(t, filepath.Join(app, "node_modules", "dep", "index.js"), "// installed this morning")

	old := time.Now().Add(-2 * 365 * 24 * time.Hour)
	setTreeTimes(t, app, old)
	// Reinstalling dependencies only moves timestamps inside node_modules.
	setTreeTimes(t, filepath.Join(app, "node_modules"), time.Now())

	it := itemAt(t, scanned(t, root), filepath.Join(app, "node_modules"))
	if it.ModTime.After(old.Add(24 * time.Hour)) {
		t.Errorf("ModTime = %s, want about %s: it must come from the project's own source, not from inside node_modules",
			it.ModTime, old)
	}
}

// The inverse case matters just as much: dependencies installed a year ago
// under a project edited this morning are not stale, and offering them for
// deletion because node_modules looks old would take away the packages of
// something the user is working on right now.
func TestModTimeTracksRecentSourceEditsWithOldDependencies(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "in-use")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "src", "Service.php"), "<?php")
	write(t, filepath.Join(app, "vendor", "acme", "lib.php"), "<?php")

	old := time.Now().Add(-365 * 24 * time.Hour)
	setTreeTimes(t, app, old)
	// Only the source file was edited today.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(app, "src", "Service.php"), now, now); err != nil {
		t.Fatal(err)
	}

	it := itemAt(t, scanned(t, root), filepath.Join(app, "vendor"))
	if it.ModTime.Before(now.Add(-24 * time.Hour)) {
		t.Errorf("ModTime = %s, want about %s: a source edit today means the project is still in use", it.ModTime, now)
	}
}

// A scan runs in the background while the user is doing something else, so a
// cancelled context has to stop it rather than let it run to completion.
func TestContextCancellationReturnsPromptly(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		project(t, filepath.Join(root, name), "package.json", "node_modules")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		items, err := Scan(ctx, root)
		if len(items) != 0 {
			t.Errorf("got %d items from a cancelled scan, want none", len(items))
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Scan returned nil error for a cancelled context")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Scan ignored context cancellation")
	}
}

// npx caches and editor extensions ship a package.json next to a
// node_modules, so without the hidden-directory rule the user would be shown
// a list of "projects" named after cache hashes and offered the chance to
// break their editor.
func TestHiddenDirectoriesAreSkipped(t *testing.T) {
	root := t.TempDir()
	npx := project(t, filepath.Join(root, ".npm", "_npx", "1415fee72ff6294b"), "package.json", "node_modules")
	ext := project(t, filepath.Join(root, ".vscode", "extensions", "some.ext-1.2.3"), "package.json", "node_modules")
	real := project(t, filepath.Join(root, "Projects", "app"), "package.json", "node_modules")

	items := scanned(t, root)

	for _, p := range []string{npx, ext} {
		if hasPath(items, filepath.Join(p, "node_modules")) {
			t.Errorf("%s is tool-managed state under a dotdir and must not be offered for deletion", p)
		}
	}
	if !hasPath(items, filepath.Join(real, "node_modules")) {
		t.Errorf("a real project must still be found; got %v", pathsOf(items))
	}
}

// Dot-prefixed kinds are matched before the hidden-directory rule gets a
// chance to skip them, or .venv and .gradle would never be reported at all.
func TestDotPrefixedKindsSurviveTheHiddenRule(t *testing.T) {
	root := t.TempDir()
	py := mkdir(t, root, "pyapp")
	write(t, filepath.Join(py, "requirements.txt"), "requests\n")
	write(t, filepath.Join(py, ".venv", "pyvenv.cfg"), "home = /usr/bin\n")
	write(t, filepath.Join(py, ".venv", "lib", "python3.12", "site-packages", "requests", "__init__.py"), "")

	it := itemAt(t, scanned(t, root), filepath.Join(py, ".venv"))
	if it.Kind.Name != "python" {
		t.Errorf("kind = %q, want python", it.Kind.Name)
	}
	if it.Kind.Restore != "python -m venv .venv && pip install -r requirements.txt" {
		t.Errorf("restore = %q, want the requirements.txt form", it.Kind.Restore)
	}
}

// "venv" and "env" are ordinary words. Only the pyvenv.cfg that the venv
// module writes proves a directory is a virtualenv rather than somebody's
// configuration folder.
func TestVirtualenvRequiresPyvenvCfg(t *testing.T) {
	root := t.TempDir()
	fake := mkdir(t, root, "webapp", "env")
	write(t, filepath.Join(fake, "production.yaml"), "key: value")
	realProject := mkdir(t, root, "pyapp")
	write(t, filepath.Join(realProject, "venv", "pyvenv.cfg"), "home = /usr/bin\n")

	items := scanned(t, root)

	if hasPath(items, fake) {
		t.Error("a directory named env holding config files was reported as a virtualenv")
	}
	if !hasPath(items, filepath.Join(realProject, "venv")) {
		t.Errorf("a directory with pyvenv.cfg in it is a virtualenv; got %v", pathsOf(items))
	}
}

// "build" is the most overloaded directory name there is. Every Go service
// on the machine this was written for has one full of compiled binaries that
// no Gradle command would restore.
func TestGradleBuildRequiresGradleManifest(t *testing.T) {
	root := t.TempDir()
	goSvc := project(t, filepath.Join(root, "go-service"), "go.mod", "build")
	gradleApp := mkdir(t, root, "jvm-app")
	write(t, filepath.Join(gradleApp, "build.gradle.kts"), "plugins {}")
	write(t, filepath.Join(gradleApp, "build", "libs", "app.jar"), "x")
	write(t, filepath.Join(gradleApp, ".gradle", "8.5", "checksums", "md5.bin"), "x")

	items := scanned(t, root)

	if hasPath(items, filepath.Join(goSvc, "build")) {
		t.Error("a Go project's build/ was reported as a Gradle build directory")
	}
	for _, want := range []string{"build", ".gradle"} {
		if !hasPath(items, filepath.Join(gradleApp, want)) {
			t.Errorf("%s in a project with build.gradle.kts should be reported; got %v", want, pathsOf(items))
		}
	}
}

// "Can I get this back?" is the entire basis for the decision, so the
// command shown has to be one that actually works here: npm ci exits with an
// error when there is no package-lock.json, and a pnpm or Yarn project would
// end up with a different tree.
func TestRestoreCommandMatchesTheLockFile(t *testing.T) {
	cases := []struct {
		lock string
		want string
	}{
		{"package-lock.json", "npm ci"},
		{"pnpm-lock.yaml", "pnpm install --frozen-lockfile"},
		{"yarn.lock", "yarn install --immutable"},
		{"bun.lockb", "bun install"},
		{"", "npm install"},
	}
	for _, c := range cases {
		name := c.lock
		if name == "" {
			name = "no-lock"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			app := project(t, filepath.Join(root, "app"), "package.json", "node_modules")
			if c.lock != "" {
				write(t, filepath.Join(app, c.lock), "")
			}
			it := itemAt(t, scanned(t, root), filepath.Join(app, "node_modules"))
			if it.Kind.Restore != c.want {
				t.Errorf("restore = %q, want %q", it.Kind.Restore, c.want)
			}
		})
	}
}

// ~/Library is macOS application state, but app/Library is an ordinary
// source directory in a Laravel project — one exists on the machine this was
// written for. The skip has to apply at the top level only.
func TestTopLevelSkipsDoNotApplyDeeper(t *testing.T) {
	root := t.TempDir()
	skipped := project(t, filepath.Join(root, "Library", "Caches", "thing"), "package.json", "node_modules")
	deeper := project(t, filepath.Join(root, "Projects", "laravel", "app", "Library"), "composer.json", "vendor")

	items := scanned(t, root)

	if hasPath(items, filepath.Join(skipped, "node_modules")) {
		t.Error("~/Library must be skipped; the other tabs cover it")
	}
	if !hasPath(items, filepath.Join(deeper, "vendor")) {
		t.Errorf("app/Library is source, not macOS state, and must still be walked; got %v", pathsOf(items))
	}
}

// One unreadable directory must not cost the user the rest of the scan.
func TestPermissionErrorsDoNotAbortTheScan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions are not enforced")
	}
	root := t.TempDir()
	locked := mkdir(t, root, "locked")
	reachable := project(t, filepath.Join(root, "reachable"), "package.json", "node_modules")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	items := scanned(t, root)

	if !hasPath(items, filepath.Join(reachable, "node_modules")) {
		t.Errorf("an unreadable sibling directory aborted the scan; got %v", pathsOf(items))
	}
}

// The list is worked down from the top, so the projects nobody has touched
// in a year belong there.
func TestItemsAreSortedStalestFirst(t *testing.T) {
	root := t.TempDir()
	stale := project(t, filepath.Join(root, "stale"), "package.json", "node_modules")
	fresh := project(t, filepath.Join(root, "fresh"), "package.json", "node_modules")
	setTreeTimes(t, stale, time.Now().Add(-2*365*24*time.Hour))
	setTreeTimes(t, fresh, time.Now())

	items := scanned(t, root)

	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %v", len(items), pathsOf(items))
	}
	if items[0].Project != "stale" {
		t.Errorf("first item is %q, want the stalest project first", items[0].Project)
	}
}
