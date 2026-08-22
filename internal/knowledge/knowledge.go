// Package knowledge is the hand-curated dictionary describing what each
// well-known cache directory is for, exactly what happens if it's deleted,
// and the literal commands that would do it. Folders not present in the
// dictionary are treated as Unknown and never offered for cleaning until
// someone adds an entry for them.
//
// Every lookup is scoped by Root — the top-level location an entry was
// found in — because the same folder name means very different things
// depending on where it sits. ~/Library/Caches/Google is Chrome's
// disposable disk cache, safe to wipe; ~/Library/Application
// Support/Google is the Chrome profile holding history, cookies and saved
// passwords. Keying the dictionary by bare folder name would let the first
// entry answer for the second.
package knowledge

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Root identifies one top-level location the app scans. The zero value is
// deliberately not a valid root, so an unset Root looks up nothing rather
// than silently inheriting Caches' answers.
type Root string

const (
	RootCaches          Root = "Caches"
	RootAppSupport      Root = "Application Support"
	RootGroupContainers Root = "Group Containers"
	RootLogs            Root = "Logs"
	RootContainers      Root = "Containers"
	// RootHome is the curated flat list of disposable tool state under
	// $HOME. Unlike the roots above we never list its directory and look
	// up whatever turns up — see HomeItems.
	RootHome Root = "Home"
	// RootApplications covers installed .app bundles and their contents.
	RootApplications Root = "Applications"
)

// Score is a 0-3 safety rating for deleting a folder.
type Score int

const (
	// Unknown means we haven't researched this folder yet. Never offer a
	// clean action for it, regardless of Commands.
	Unknown Score = 0
	// Risky means deletion will likely affect the owning app's saved
	// state (settings, login sessions, purchase history) — delete at
	// your own risk.
	Risky Score = 1
	// Caution means deletion is probably fine but may cost the app
	// something recoverable: re-downloads, re-indexing, slower next
	// launch.
	Caution Score = 2
	// Safe means this is a pure, disposable cache the owning app
	// recreates transparently.
	Safe Score = 3
)

// Entry is one dictionary record.
type Entry struct {
	Score Score
	// Description explains what the folder is and what actually lives in
	// it — who writes to it and why it exists.
	Description string
	// Effects explains, in plain language, exactly what happens after
	// cleaning: what disappears, what doesn't, and any prerequisites
	// (e.g. quit the app first).
	Effects string
	// Commands are the literal shell commands a clean action is
	// equivalent to, shown so the user knows precisely which folders and
	// files are on the chopping block before confirming.
	Commands []string
	// CleanPaths are glob patterns, relative to the folder's own path,
	// naming exactly what a clean action removes. Empty means "everything
	// directly inside the folder" (the common case); a non-empty list
	// means only those specific relative paths are removed and everything
	// else in the folder is left alone — used where deleting the whole
	// folder is too broad (e.g. a JetBrains IDE version still in use).
	CleanPaths []string
	// Container marks a folder that exists only to hold other folders
	// (e.g. JetBrains, which holds one cache directory per installed IDE
	// version) — never offer a clean action for it directly; the user has
	// to open it and clean/inspect what's inside individually.
	Container bool
	// Native, when set, is an alternative clean action: instead of us
	// deleting files ourselves, we run the owning tool's own cache-clean
	// command and let it decide exactly what's safe to remove.
	Native *NativeClean
	// Orphan marks a folder whose owning application appears to be
	// uninstalled — set by AnnotateOrphan, never written by hand in the
	// dictionary. It's presentation only: it flags the row and extends
	// the description, but deliberately does not change Score or unlock a
	// clean action the entry didn't already have.
	Orphan bool
}

// NativeClean describes a tool-provided cleanup command as an alternative
// to deleting files directly.
type NativeClean struct {
	// Description explains what the command does and why it's preferable
	// to a plain rm -rf here.
	Description string
	// Command is the literal shell command that gets run.
	Command string
}

// CanClean reports whether the UI should offer a clean action for e.
func (e Entry) CanClean() bool {
	return !e.Container && e.Score >= Risky && len(e.Commands) > 0
}

// RootSpec describes one scanned top-level directory: where it lives and
// what to call it in the TYPE column.
type RootSpec struct {
	Root Root
	Path string
	// Label is the short TYPE-column tag. Kept to colTypeWidth or under
	// so the table never has to truncate it.
	Label string
}

// SystemDataRoots are the locations aggregated into the System Data tab,
// ordered biggest-first on a typical machine so the initial listing fills
// in roughly most-interesting-first. $HOME is deliberately absent: it is
// mostly the user's own files, and the handful of disposable tool caches
// in it are curated by hand on the Home tab instead.
func SystemDataRoots() []RootSpec {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	lib := filepath.Join(home, "Library")
	return []RootSpec{
		{Root: RootAppSupport, Path: filepath.Join(lib, "Application Support"), Label: "AppSupp"},
		{Root: RootGroupContainers, Path: filepath.Join(lib, "Group Containers"), Label: "GroupC"},
		{Root: RootCaches, Path: filepath.Join(lib, "Caches"), Label: "Cache"},
		{Root: RootLogs, Path: filepath.Join(lib, "Logs"), Label: "Log"},
		{Root: RootContainers, Path: filepath.Join(lib, "Containers"), Label: "Contain"},
	}
}

// HomeItem is one hand-picked entry on the Home tab.
//
// $HOME gets a curated list rather than a directory listing because,
// unlike Library, it is overwhelmingly the user's own work: source
// checkouts, documents, screenshots. Listing it and inviting the user to
// delete rows would be pointing a loaded gun at their projects folder.
// Only a specific, known set of paths in it are disposable tool state, so
// only those are named — and anything not on this list is never shown at
// all, let alone offered for cleaning.
type HomeItem struct {
	// RelPath is relative to $HOME, e.g. ".ollama" or "go/pkg/mod". It
	// may name a file rather than a directory.
	RelPath string
	// Display overrides the table's NAME column. Empty means use RelPath,
	// which is what most entries want ("~/" is prepended by the UI).
	Display string
	Entry   Entry
}

// HomeItems returns the curated Home-tab list, skipping paths that don't
// exist on this machine — the list covers tools the user may or may not
// have installed, and an absent Rust toolchain shouldn't show up as an
// empty row.
func HomeItems() []HomeItem {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := make([]HomeItem, 0, len(homeItems))
	for _, it := range homeItems {
		if _, err := os.Lstat(filepath.Join(home, it.RelPath)); err != nil {
			continue
		}
		out = append(out, it)
	}
	return out
}

// db is the per-root dictionary. Each root's table lives in its own file
// (caches.go, appsupport.go, ...) so the curated prose stays browsable.
var db = map[Root]map[string]Entry{
	RootCaches:          cachesDB,
	RootAppSupport:      appSupportDB,
	RootGroupContainers: groupContainersDB,
	RootLogs:            logsDB,
	RootContainers:      containersDB,
	RootApplications:    applicationsDB,
}

// patterns are per-root regexp fallbacks for folders whose name varies,
// typically by embedding a version number.
var patterns = map[Root][]patternEntry{
	RootCaches:     {jetbrainsCachePattern},
	RootAppSupport: {jetbrainsConfigPattern},
	RootLogs:       {jetbrainsLogPattern},
}

// Lookup returns the dictionary entry for name under root, with no context
// about sibling folders. Exact folder names are checked first, then that
// root's patterns. Names matching neither return a zero-value Unknown
// entry. Most callers that have a folder listing available should prefer
// Effective, which additionally accounts for sibling versions.
func Lookup(root Root, name string) Entry {
	if tbl, ok := db[root]; ok {
		if e, ok := tbl[name]; ok {
			return e
		}
	}
	for _, p := range patterns[root] {
		if p.re.MatchString(name) {
			return p.build(name)
		}
	}
	return Entry{Score: Unknown}
}

// Effective is Lookup adjusted for context: under RootCaches, if name is a
// JetBrains per-version cache folder and siblings (the other folder names
// in the same directory) contains a strictly newer version of the same
// IDE, that older version's cache is fully superseded — nothing will ever
// read it again — so it's promoted to Safe with a whole-folder delete,
// instead of the conservative subpath-only cleanup Lookup gives the
// version still in use.
//
// This promotion is deliberately limited to caches. The matching folders
// under Application Support hold that version's settings, keymaps and
// plugin configuration rather than regenerable cache, and stay untouched
// no matter how obsolete the version is — migrating or discarding those is
// the user's call, not ours.
func Effective(root Root, name string, siblings []string) Entry {
	base := Lookup(root, name)
	if root != RootCaches {
		return base
	}

	product, year, minor, ok := parseJetBrainsVersion(name)
	if !ok {
		return base
	}
	for _, sib := range siblings {
		if sib == name {
			continue
		}
		p2, y2, m2, ok2 := parseJetBrainsVersion(sib)
		if !ok2 || p2 != product {
			continue
		}
		if y2 > year || (y2 == year && m2 > minor) {
			return Entry{
				Score: Safe,
				Description: base.Description + " A newer " + product + " install exists on this machine, so this " +
					"particular version's cache is fully superseded.",
				Effects: "A newer version of " + product + " is installed, so this version will never run again " +
					"unless you reinstall it specifically — the whole folder (indexes, local history, plugin " +
					"caches, everything) is safe to delete outright. Your settings, keymaps and license are not " +
					"here; they live under ~/Library/Application Support/JetBrains, which this app never touches. " +
					"If you do reinstall this exact version, it just reindexes your projects and re-downloads " +
					"plugins like a fresh install would.",
				Commands: []string{`rm -rf ~/Library/Caches/JetBrains/` + name + `/*`},
			}
		}
	}
	return base
}

// patternEntry matches folder names by regexp rather than exact string,
// building the Entry dynamically so text like Commands can embed the
// actual matched folder name.
type patternEntry struct {
	re    *regexp.Regexp
	build func(name string) Entry
}

// jetbrainsVersionRe matches a JetBrains IDE's per-version folder, e.g.
// "GoLand2026.1", "IntelliJIdea2024.3", "WebStorm2026.2", capturing the
// product name, year, and minor version separately for comparison. The
// same naming scheme is used under Caches, Application Support and Logs.
var jetbrainsVersionRe = regexp.MustCompile(
	`^(GoLand|IntelliJIdea|WebStorm|PyCharm|PhpStorm|CLion|Rider|DataGrip|RubyMine|AppCode|AndroidStudio|RustRover|Fleet)(\d{4})\.(\d+)$`,
)

// parseJetBrainsVersion splits a per-version JetBrains folder name into
// product + comparable (year, minor) version numbers.
func parseJetBrainsVersion(name string) (product string, year, minor int, ok bool) {
	m := jetbrainsVersionRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0, 0, false
	}
	year, err1 := strconv.Atoi(m[2])
	minor, err2 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil {
		return "", 0, 0, false
	}
	return m[1], year, minor, true
}
