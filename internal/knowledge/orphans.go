package knowledge

// Orphan detection: spotting folders under ~/Library that belong to an app
// which is no longer installed. These are the classic "leftovers" — an app
// gets dragged to the Trash, and its Application Support, Caches, Logs and
// Containers folders stay behind forever, since macOS has no uninstall
// hook that would clean them up.
//
// The signal is deliberately conservative. Plenty of legitimate folders
// under Library are named like bundle identifiers but have no .app at all:
// system daemons, background updaters, Safari web extensions, XPC helpers.
// Reporting those as orphans would be worse than useless — it would teach
// the user to ignore the flag. So an orphan is only ever claimed when the
// name is bundle-ID-shaped, is not Apple's, and matches no installed app
// even as a prefix. Anything we can't be confident about is silently left
// alone, and orphan status never upgrades a Score or invents a Command: it
// only adds context to what the dictionary already said.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lmkdmvlvkn/internal/scan"
)

// appDirs are the standard locations an installed application can live in.
// /System/Applications holds Apple's built-in apps on Catalina and later;
// it's included so their bundle IDs register as installed even though
// com.apple.* folders are skipped by the orphan check anyway.
func appDirs() []string {
	dirs := []string{"/Applications", "/System/Applications", "/Applications/Utilities"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// bundleIDRe matches a reverse-DNS bundle identifier with at least three
// components (com.hnc.Discord). Two-component names like "group.storekit"
// are too generic to reason about and are skipped.
var bundleIDRe = regexp.MustCompile(`^[A-Za-z0-9-]+(?:\.[A-Za-z0-9_-]+){2,}$`)

// teamPrefixRe matches the 10-character Apple Developer Team ID that
// prefixes group-container folder names, e.g. the "6N38VWS5BX." in
// 6N38VWS5BX.ru.keepcoder.Telegram. The team ID identifies the vendor, not
// the app, so it has to come off before comparing against bundle IDs.
var teamPrefixRe = regexp.MustCompile(`^[A-Z0-9]{10}\.`)

// AppIndex is the set of bundle identifiers of every installed
// application. The ready flag distinguishes "indexed, and this app is
// genuinely absent" from "indexing failed" — when indexing fails we must
// never claim anything is an orphan, since every folder would look like
// one.
type AppIndex struct {
	ids   map[string]bool
	ready bool
}

// IndexInstalledApps reads the bundle identifier out of every .app in the
// standard application directories. It is a few dozen short-lived
// `defaults read` processes (see scan.BundleID), run once in the
// background at startup.
func IndexInstalledApps() AppIndex {
	ids := make(map[string]bool)
	var found bool
	for _, dir := range appDirs() {
		items, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		found = true
		for _, it := range items {
			if !strings.HasSuffix(it.Name(), ".app") {
				continue
			}
			if id := scan.BundleID(filepath.Join(dir, it.Name())); id != "" {
				ids[strings.ToLower(id)] = true
			}
		}
	}
	// An index with no apps at all means the scan failed rather than that
	// the Mac has no software on it, so refuse to answer instead of
	// declaring everything orphaned.
	return AppIndex{ids: ids, ready: found && len(ids) > 0}
}

// normalizeBundleName reduces a Library folder name to the bundle ID it
// would correspond to, or returns ok=false if the name isn't something we
// can reason about. It strips the group-container team-ID prefix and the
// "group." marker that shared containers use.
func normalizeBundleName(name string) (string, bool) {
	n := teamPrefixRe.ReplaceAllString(name, "")
	n = strings.TrimPrefix(n, "group.")
	if !bundleIDRe.MatchString(n) {
		return "", false
	}
	return strings.ToLower(n), true
}

// Orphaned reports whether name looks like it belongs to an application
// that is no longer installed.
//
// The prefix rule matters as much as the exact match: helper folders like
// com.hnc.Discord.ShipIt (Sparkle's updater) or com.google.Chrome.helper
// are owned by an installed app even though no .app carries that exact
// identifier, so a folder is considered live if any installed bundle ID is
// a prefix of it — or, for suite-style apps, if it is a prefix of an
// installed ID.
func (ix AppIndex) Orphaned(name string) bool {
	if !ix.ready {
		return false
	}
	id, ok := normalizeBundleName(name)
	if !ok {
		return false
	}
	// Apple's own folders are overwhelmingly system daemons and frameworks
	// with no .app anywhere; they are never leftovers in the sense a user
	// could act on.
	if strings.HasPrefix(id, "com.apple.") {
		return false
	}
	if ix.ids[id] {
		return false
	}
	for installed := range ix.ids {
		if strings.HasPrefix(id, installed+".") || strings.HasPrefix(installed, id+".") {
			return false
		}
	}
	return true
}

// orphanNote is appended to a leftover folder's description. It is
// deliberately hedged: background helpers and Safari extension hosts are
// legitimately app-less, so this reports a strong hint rather than a fact.
const orphanNote = " No installed application claims this bundle identifier, so this looks like a leftover from an " +
	"app that was deleted — macOS removes the .app when you trash it but never touches the folders it created " +
	"under ~/Library. Worth confirming before you act on it: background updaters, Safari web extensions and " +
	"command-line tools legitimately own folders here without shipping an .app of their own."

// leftoversPossible reports whether root is somewhere apps abandon
// bundle-ID-named folders when uninstalled.
//
// It is deliberately limited to the Library roots. On the Applications tab
// the rows *are* the applications, so asking whether an app is installed
// is incoherent — and actively wrong, since a bundle's folder name is not
// its identifier: zoom.us.app parses as a plausible identifier but Zoom
// ships as us.zoom.xos, which would flag an installed app as a leftover.
// The Home tab lists paths, not identifiers, so it has nothing to check.
func leftoversPossible(root Root) bool {
	switch root {
	case RootCaches, RootAppSupport, RootGroupContainers, RootLogs, RootContainers:
		return true
	default:
		return false
	}
}

// AnnotateOrphan adds leftover context to e when name has no installed
// owner. It never changes Score, Commands or CleanPaths — a folder that
// the dictionary rated Risky stays Risky, and an Unknown folder stays
// uncleanable. The only thing that changes is what the user is told, which
// is exactly the amount of authority a heuristic like this has earned.
func AnnotateOrphan(e Entry, root Root, name string, ix AppIndex) Entry {
	if !leftoversPossible(root) || !ix.Orphaned(name) {
		return e
	}
	if e.Description == "" {
		e.Description = "An unrecognised folder named after an application bundle identifier."
	}
	e.Description += orphanNote
	e.Orphan = true
	return e
}

// AppIndexForTest builds a ready index over the given bundle identifiers.
// Exported so the UI package can exercise orphan-dependent behaviour
// without shelling out to `defaults read` for every installed app.
func AppIndexForTest(ids ...string) AppIndex {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[strings.ToLower(id)] = true
	}
	return AppIndex{ids: m, ready: true}
}
