package vendors

import (
	"os"
	"path/filepath"
)

// rule pairs a Kind with the conditions that must hold before a directory of
// that name counts as a match. Every rule needs at least one of them: a bare
// name match is never enough, because the cost of a false positive here is
// the user deleting something no command can bring back.
type rule struct {
	Kind
	// altManifests lists further accepted manifest names beyond
	// Kind.Manifest. Gradle is the reason: build.gradle, build.gradle.kts,
	// settings.gradle and settings.gradle.kts all mark the same thing, but
	// Kind.Manifest is a single string because that is what the UI shows.
	altManifests []string
	// markers are entries that must exist *inside* the candidate directory.
	// Any one of them satisfies the gate. This is how a Python virtualenv is
	// recognised: the directory name is worthless as evidence, "venv" and
	// "env" being perfectly ordinary names for hand-written config, but a
	// pyvenv.cfg inside is written by nothing except `python -m venv`.
	markers []string
	// restore, when set, replaces Kind.Restore with a project-specific
	// command. dir is the matched directory's own name, so the command can
	// name the directory it recreates.
	restore func(projectDir, dir string) string
}

// rules is the whole catalogue, keyed by nothing — matching is by directory
// name via rulesByDir, built below.
//
// Every entry here was chosen so that a match implies the directory is owned
// by a tool and rebuildable by that tool. Kinds that could not be pinned down
// that way are deliberately absent; see the notes at the bottom of this file.
var rules = []rule{
	// JavaScript. The manifest gate is what separates a project's
	// node_modules from the ones bundled inside editor extensions and npx
	// caches, and the restore command is chosen from the lock file because
	// `npm ci` aborts outright when package-lock.json is missing.
	{
		Kind:    Kind{Name: "node", Dir: "node_modules", Restore: "npm ci", Manifest: "package.json"},
		restore: nodeRestore,
	},

	// PHP. The manifest check is not a nicety: Go projects also keep a
	// vendor/ directory, and there are 68 go.mod files against 25
	// composer.json files on the machine this was written for. Deleting a
	// Go vendor/ without running `go mod vendor` breaks the build in a way
	// `composer install` will not fix.
	{Kind: Kind{Name: "php", Dir: "vendor", Restore: "composer install", Manifest: "composer.json"}},

	// Rust. target/ is a plain, common word; Cargo.toml is what makes it
	// Cargo's build directory rather than somebody's output folder.
	{Kind: Kind{Name: "rust", Dir: "target", Restore: "cargo build", Manifest: "Cargo.toml"}},

	// Gradle. Same story, more urgently: "build" is the single most
	// overloaded directory name there is — every Go service in
	// ~/GolandProjects has one holding compiled binaries that no Gradle
	// command would ever restore. Only a Gradle manifest alongside makes
	// build/ safe to offer.
	{
		Kind:         Kind{Name: "gradle", Dir: "build", Restore: "./gradlew build", Manifest: "build.gradle"},
		altManifests: []string{"build.gradle.kts", "settings.gradle", "settings.gradle.kts"},
	},
	{
		Kind:         Kind{Name: "gradle", Dir: ".gradle", Restore: "./gradlew build", Manifest: "build.gradle"},
		altManifests: []string{"build.gradle.kts", "settings.gradle", "settings.gradle.kts"},
	},

	// CocoaPods.
	{Kind: Kind{Name: "pods", Dir: "Pods", Restore: "pod install", Manifest: "Podfile"}},

	// Python virtualenvs. Gated on the pyvenv.cfg the venv module writes,
	// never on the directory name, so even a directory called "env" is only
	// reported when it demonstrably is one.
	{
		Kind:    Kind{Name: "python", Dir: ".venv", Restore: "python -m venv .venv", Manifest: markerManifest},
		markers: []string{"pyvenv.cfg"},
		restore: venvRestore,
	},
	{
		Kind:    Kind{Name: "python", Dir: "venv", Restore: "python -m venv venv", Manifest: markerManifest},
		markers: []string{"pyvenv.cfg"},
		restore: venvRestore,
	},
	{
		Kind:    Kind{Name: "python", Dir: "env", Restore: "python -m venv env", Manifest: markerManifest},
		markers: []string{"pyvenv.cfg"},
		restore: venvRestore,
	},

	// Tool caches whose names no other software creates. These need no gate
	// beyond the name — a directory called .mypy_cache was written by mypy
	// and by nothing else — and gating them on a manifest would only make
	// the scan miss caches in projects configured through pyproject.toml,
	// setup.cfg or the tool's own defaults.
	{Kind: Kind{Name: "pycache", Dir: "__pycache__", Restore: "python -m compileall ."}},
	{Kind: Kind{Name: "pytest", Dir: ".pytest_cache", Restore: "pytest"}},
	{Kind: Kind{Name: "mypy", Dir: ".mypy_cache", Restore: "mypy ."}},
	{Kind: Kind{Name: "ruff", Dir: ".ruff_cache", Restore: "ruff check ."}},
	{Kind: Kind{Name: "tox", Dir: ".tox", Restore: "tox", Manifest: "tox.ini"}, altManifests: []string{"pyproject.toml", "setup.cfg"}},
	{Kind: Kind{Name: "phpunit", Dir: ".phpunit.cache", Restore: "vendor/bin/phpunit"}},

	// JavaScript build/bundler caches. Dot-prefixed, framework-owned names,
	// still gated on package.json so a stray directory of the same name
	// outside a Node project is left alone.
	{Kind: Kind{Name: "next", Dir: ".next", Restore: "npm run build", Manifest: "package.json"}},
	{Kind: Kind{Name: "nuxt", Dir: ".nuxt", Restore: "npm run build", Manifest: "package.json"}},
	{Kind: Kind{Name: "svelte", Dir: ".svelte-kit", Restore: "npm run build", Manifest: "package.json"}},
	{Kind: Kind{Name: "angular", Dir: ".angular", Restore: "npm run build", Manifest: "package.json"}},
	{Kind: Kind{Name: "turbo", Dir: ".turbo", Restore: "npx turbo build", Manifest: "package.json"}},
	{Kind: Kind{Name: "parcel", Dir: ".parcel-cache", Restore: "npx parcel build", Manifest: "package.json"}},
}

// markerManifest spells out, in the Manifest field, that a kind is gated on a
// file inside the directory rather than on a sibling. Written as a field name
// so the rule table above stays readable.
const markerManifest = "pyvenv.cfg (inside)"

// Kinds deliberately NOT implemented, and why. Each of these was considered
// and rejected because no available evidence separates the disposable case
// from the irreplaceable one:
//
//   - A bare vendor/ with no composer.json. That is a Go vendor directory,
//     and its contents are not necessarily reproducible from go.mod alone
//     (a replaced or since-deleted module leaves nothing to re-download).
//   - dist/ and out/. Overwhelmingly build output, but routinely committed
//     as the published artifact of a library, and nothing on disk says
//     which case a given one is.
//   - A bare tmp/ or storage/tmp/. Tempting, since psalm on this machine is
//     configured to cache there — but those directories also hold coverage
//     reports, a tracked .gitignore, and in one project a copy of
//     .bash_history. They are reachable only through psalm's own config,
//     which names the exact subdirectory; see phptools.go.
//   - .idea/ and .vscode/. Editor state, not reinstallable: run
//     configurations and code styles live there and no command recreates
//     them.
//   - Xcode DerivedData and SwiftPM caches. They live under ~/Library,
//     which this scan skips by design, and belong to internal/knowledge.
//   - .terraform/. Restorable with `terraform init`, but it also holds the
//     provider lock's resolved plugins and, in some layouts, local state.
//     Not worth the risk for the space involved.

// rulesByDir indexes rules by the directory name they match. Several rules
// can share a name in principle, so the value is a slice and the first rule
// whose gates pass wins.
var rulesByDir = func() map[string][]rule {
	m := make(map[string][]rule, len(rules))
	for _, r := range rules {
		m[r.Dir] = append(m[r.Dir], r)
	}
	return m
}()

// ruleDirs is the set of every directory name any rule matches. The source
// scan behind ModTime uses it to step over dependency directories without
// having to re-evaluate the gates.
var ruleDirs = func() map[string]bool {
	m := make(map[string]bool, len(rules))
	for _, r := range rules {
		m[r.Dir] = true
	}
	return m
}()

// match reports whether name, a subdirectory of projectDir, satisfies some
// rule. siblings is projectDir's listing, already in hand from the walk, so
// the manifest check costs a map lookup instead of a stat per candidate.
func match(projectDir, name string, siblings map[string]bool) (Kind, bool) {
	for _, r := range rulesByDir[name] {
		if !r.manifestPresent(siblings) {
			continue
		}
		if !r.markerPresent(filepath.Join(projectDir, name)) {
			continue
		}
		k := r.Kind
		if r.restore != nil {
			k.Restore = r.restore(projectDir, name)
		}
		return k, true
	}
	return Kind{}, false
}

// manifestPresent reports whether any accepted manifest sits next to the
// candidate directory. A rule with no manifest is gated elsewhere and passes.
func (r rule) manifestPresent(siblings map[string]bool) bool {
	if len(r.markers) > 0 || r.Manifest == "" || r.Manifest == markerManifest {
		return true
	}
	if siblings[r.Manifest] {
		return true
	}
	for _, alt := range r.altManifests {
		if siblings[alt] {
			return true
		}
	}
	return false
}

// markerPresent reports whether the candidate directory contains one of the
// files that prove which tool owns it. Rules without markers pass.
func (r rule) markerPresent(path string) bool {
	if len(r.markers) == 0 {
		return true
	}
	for _, m := range r.markers {
		if _, err := os.Lstat(filepath.Join(path, m)); err == nil {
			return true
		}
	}
	return false
}

// nodeRestore names the command that actually works in this project. `npm ci`
// is the right answer only when package-lock.json exists — without one it
// exits with an error rather than installing anything — and a project on
// pnpm or Yarn would end up with a different tree entirely. Since the whole
// decision rests on "can I get this back", the command shown has to be one
// the user can really run.
func nodeRestore(projectDir, _ string) string {
	switch {
	case exists(projectDir, "pnpm-lock.yaml"):
		return "pnpm install --frozen-lockfile"
	case exists(projectDir, "yarn.lock"):
		return "yarn install --immutable"
	case exists(projectDir, "bun.lockb"), exists(projectDir, "bun.lock"):
		return "bun install"
	case exists(projectDir, "package-lock.json"):
		return "npm ci"
	default:
		// No lock file: npm ci would fail, so the honest command is the
		// resolving one, even though it may not reproduce the same tree.
		return "npm install"
	}
}

// venvRestore names both the venv creation and the install step, picked from
// whichever dependency manager the project uses.
func venvRestore(projectDir, dir string) string {
	switch {
	case exists(projectDir, "uv.lock"):
		return "uv sync"
	case exists(projectDir, "poetry.lock"):
		return "poetry install"
	case exists(projectDir, "Pipfile.lock"), exists(projectDir, "Pipfile"):
		return "pipenv install"
	case exists(projectDir, "requirements.txt"):
		return "python -m venv " + dir + " && pip install -r requirements.txt"
	case exists(projectDir, "pyproject.toml"):
		return "python -m venv " + dir + " && pip install -e ."
	default:
		return "python -m venv " + dir
	}
}

func exists(dir, name string) bool {
	_, err := os.Lstat(filepath.Join(dir, name))
	return err == nil
}
