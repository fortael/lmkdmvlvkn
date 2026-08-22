package scan

// Application-bundle discovery for the Applications tab.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AppDirs are the locations user-installable applications live in.
// /System/Applications is deliberately absent: it is Apple's read-only
// system volume, nothing there can be removed, and the Applications tab
// excludes Apple software anyway.
func AppDirs() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// BundleID returns the CFBundleIdentifier of the .app bundle at appPath.
//
// Info.plist is usually in Apple's binary plist format, which the standard
// library cannot parse, so this shells out to `defaults read` rather than
// pulling in a plist dependency for one field. Callers run this over a few
// dozen apps at most, in the background, so the process-per-app cost is
// not worth optimising away.
func BundleID(appPath string) string {
	// `defaults read` wants the plist path without the .plist extension.
	plist := filepath.Join(appPath, "Contents", "Info")
	out, err := exec.Command("defaults", "read", plist, "CFBundleIdentifier").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ListApplications returns the non-Apple application bundles installed on
// this machine, as unsized entries ready for the Scanner. root is the
// dictionary key stamped onto every entry, as in List.
//
// Apple's own apps are filtered out entirely and never shown: they live on
// the sealed system volume or are managed by the OS, removing them ranges
// from impossible to actively harmful, and the user has been explicit that
// handling those is their business elsewhere. Bundles whose identifier
// can't be read are kept rather than dropped — an unreadable Info.plist is
// itself worth seeing, and the dictionary will simply rate it Unknown.
func ListApplications(root string) ([]*Entry, error) {
	var entries []*Entry
	var firstErr error
	seen := make(map[string]bool)

	for _, dir := range AppDirs() {
		items, err := os.ReadDir(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, it := range items {
			if !strings.HasSuffix(it.Name(), ".app") {
				continue
			}
			path := filepath.Join(dir, it.Name())
			if strings.HasPrefix(strings.ToLower(BundleID(path)), "com.apple.") {
				continue
			}
			// The same app can appear in both /Applications and
			// ~/Applications (a stale copy left behind by an installer
			// that later moved it); show the first one found rather than
			// listing the name twice.
			if seen[it.Name()] {
				continue
			}
			seen[it.Name()] = true

			info, err := it.Info()
			if err != nil {
				continue
			}
			entries = append(entries, &Entry{
				Name:    it.Name(),
				Path:    path,
				Source:  "App",
				Root:    root,
				ModTime: info.ModTime(),
				IsDir:   it.IsDir(),
				Size:    -1,
			})
		}
	}
	if len(entries) > 0 {
		return entries, nil
	}
	return entries, firstErr
}
