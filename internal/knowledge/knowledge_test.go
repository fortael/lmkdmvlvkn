package knowledge

import (
	"strings"
	"testing"
)

// allEntries walks every curated entry in the package, labelled by where
// it came from, so the invariant tests below cover new dictionary files
// automatically as they're added.
func allEntries(t *testing.T) map[string]Entry {
	t.Helper()
	out := make(map[string]Entry)
	for root, tbl := range db {
		for name, e := range tbl {
			out[string(root)+"/"+name] = e
		}
	}
	for _, it := range homeItems {
		out["Home/"+it.RelPath] = it.Entry
	}
	return out
}

// The dictionary is keyed by root because the same folder name means
// different things in different places — Caches/Google is Chrome's
// disposable disk cache, Application Support/Google is the profile holding
// saved passwords. A lookup must never fall through to another root's
// answer.
func TestLookupIsScopedToItsRoot(t *testing.T) {
	if got := Lookup(RootCaches, "go-build"); got.Score == Unknown {
		t.Fatal("test setup: expected Caches/go-build to be a known entry")
	}
	for _, root := range []Root{RootAppSupport, RootGroupContainers, RootLogs, RootContainers, RootApplications} {
		if got := Lookup(root, "go-build"); got.Score != Unknown {
			t.Errorf("Lookup(%s, go-build) = %v, want Unknown (leaked from the Caches table)", root, got.Score)
		}
	}
}

func TestLookupUnknownForUnresearchedName(t *testing.T) {
	e := Lookup(RootCaches, "definitely-not-a-real-folder-name")
	if e.Score != Unknown {
		t.Errorf("Score = %v, want Unknown", e.Score)
	}
	if e.CanClean() {
		t.Error("CanClean() = true for an unresearched folder; it must never be offered for cleaning")
	}
}

// A JetBrains cache folder superseded by a newer install of the same IDE
// will never be read again, so it's promoted to a whole-folder delete.
func TestEffectivePromotesSupersededCache(t *testing.T) {
	siblings := []string{"GoLand2026.1", "GoLand2026.2", "WebStorm2026.1"}

	old := Effective(RootCaches, "GoLand2026.1", siblings)
	if old.Score != Safe {
		t.Errorf("superseded GoLand2026.1: Score = %v, want Safe", old.Score)
	}
	if len(old.CleanPaths) != 0 {
		t.Errorf("superseded GoLand2026.1: CleanPaths = %v, want empty (whole-folder delete)", old.CleanPaths)
	}

	// The version still in use keeps the conservative subpath cleanup.
	current := Effective(RootCaches, "GoLand2026.2", siblings)
	if current.Score == Safe {
		t.Error("GoLand2026.2 is the newest install; it must not be promoted to Safe")
	}
	if len(current.CleanPaths) == 0 {
		t.Error("GoLand2026.2: want granular CleanPaths so indexes and Local History survive")
	}

	// Only an IDE with a newer sibling is superseded; a lone version isn't.
	lone := Effective(RootCaches, "WebStorm2026.1", siblings)
	if lone.Score == Safe {
		t.Error("WebStorm2026.1 has no newer sibling; it must not be promoted to Safe")
	}
}

// The supersede rule is what must stay confined to Caches: outside it,
// having a newer sibling installed may not change the answer at all.
//
// This is narrower than "nothing outside Caches is ever safely deletable".
// A JetBrains version's *logs* are disposable whether or not that version
// is still current, so the Logs table rates them Safe on their own merits
// — no sibling involved. What would be wrong is an entry that becomes
// deletable only because a newer version turned up.
func TestEffectiveIsSiblingIndependentOutsideCaches(t *testing.T) {
	const name = "GoLand2026.1"
	alone := []string{name}
	superseded := []string{name, "GoLand2026.2"}

	for _, root := range []Root{RootAppSupport, RootGroupContainers, RootLogs, RootContainers, RootApplications} {
		a := Effective(root, name, alone)
		b := Effective(root, name, superseded)
		if a.Score != b.Score || len(a.CleanPaths) != len(b.CleanPaths) || len(a.Commands) != len(b.Commands) {
			t.Errorf("Effective(%s, %s): a newer sibling changed the verdict (%v/%d paths/%d cmds -> "+
				"%v/%d paths/%d cmds); only Caches may be superseded",
				root, name, a.Score, len(a.CleanPaths), len(a.Commands),
				b.Score, len(b.CleanPaths), len(b.Commands))
		}
	}
}

// Product owner's explicit call: we clean superseded IDE *caches*, but the
// IDE's own settings, keymaps, plugin config and license live under
// Application Support and are never ours to delete, however obsolete the
// version is. Uninstalling an IDE is the user's decision.
func TestJetBrainsConfigIsNeverCleanable(t *testing.T) {
	siblings := []string{"GoLand2026.1", "GoLand2026.2", "PhpStorm2026.1", "PhpStorm2026.2"}
	for _, name := range siblings {
		got := Effective(RootAppSupport, name, siblings)
		if got.CanClean() {
			t.Errorf("Application Support/%s is cleanable; IDE settings and licenses must never be deleted", name)
		}
		if got.Description == "" {
			t.Errorf("Application Support/%s has no Description; it should still explain itself", name)
		}
	}
}

func TestCanCleanGating(t *testing.T) {
	tests := []struct {
		name string
		e    Entry
		want bool
	}{
		{"unknown score", Entry{Score: Unknown, Commands: []string{"rm -rf x"}}, false},
		{"container", Entry{Score: Safe, Container: true, Commands: []string{"rm -rf x"}}, false},
		{"no commands", Entry{Score: Safe}, false},
		{"safe with commands", Entry{Score: Safe, Commands: []string{"rm -rf x"}}, true},
		{"risky with commands", Entry{Score: Risky, Commands: []string{"rm -rf x"}}, true},
	}
	for _, tt := range tests {
		if got := tt.e.CanClean(); got != tt.want {
			t.Errorf("%s: CanClean() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// The UI pairs each real command with the CleanPaths pattern at the same
// position to show per-path sizes. If the counts disagree the sizes land
// on the wrong command, so keep them in lockstep across the dictionary.
func TestCleanPathsMatchRealCommandCount(t *testing.T) {
	for key, e := range allEntries(t) {
		if len(e.CleanPaths) == 0 {
			continue
		}
		real := 0
		for _, c := range e.Commands {
			if !strings.HasPrefix(c, "#") {
				real++
			}
		}
		if real != len(e.CleanPaths) {
			t.Errorf("%s: %d real commands but %d CleanPaths; they must correspond one-to-one",
				key, real, len(e.CleanPaths))
		}
	}
}

// Anything the app offers to delete has to explain itself first — that's
// the whole premise of the knowledge base over a blind cleaner.
func TestCleanableEntriesAreDocumented(t *testing.T) {
	for key, e := range allEntries(t) {
		if !e.CanClean() {
			continue
		}
		if strings.TrimSpace(e.Description) == "" {
			t.Errorf("%s is cleanable but has no Description", key)
		}
		if strings.TrimSpace(e.Effects) == "" {
			t.Errorf("%s is cleanable but has no Effects", key)
		}
	}
}

func TestNativeCleanEntriesAreDocumented(t *testing.T) {
	for key, e := range allEntries(t) {
		if e.Native == nil {
			continue
		}
		if strings.TrimSpace(e.Native.Command) == "" {
			t.Errorf("%s has a Native clean with an empty Command", key)
		}
		if strings.TrimSpace(e.Native.Description) == "" {
			t.Errorf("%s has a Native clean with no Description", key)
		}
	}
}

// Guard against a curated command that would take out a home directory or
// the filesystem root through a stray space or an unanchored glob.
func TestNoCommandTargetsHomeOrRootWholesale(t *testing.T) {
	forbidden := []string{
		"rm -rf ~",
		"rm -rf ~/",
		"rm -rf ~/*",
		"rm -rf /",
		"rm -rf /*",
		"rm -rf $HOME",
		"rm -rf $HOME/",
		"rm -rf $HOME/*",
		"rm -rf .",
		"rm -rf ..",
	}
	check := func(key, cmd string) {
		if strings.HasPrefix(cmd, "#") {
			return
		}
		trimmed := strings.TrimSpace(cmd)
		for _, bad := range forbidden {
			if trimmed == bad {
				t.Errorf("%s: refusing command %q — it would delete the whole home directory or disk", key, cmd)
			}
		}
	}
	for key, e := range allEntries(t) {
		for _, c := range e.Commands {
			check(key, c)
		}
		if e.Native != nil {
			check(key, e.Native.Command)
		}
	}
}

// Anything inside a .app bundle is code-signed; removing even an unused
// resource makes Gatekeeper refuse to launch the app. The Applications tab
// is therefore descriptive only, and no entry in it may offer a delete.
func TestApplicationsDictionaryIsDescriptiveOnly(t *testing.T) {
	for name, e := range applicationsDB {
		if e.CanClean() {
			t.Errorf("applicationsDB[%q] is cleanable; deleting inside a signed bundle breaks the app", name)
		}
		if e.Native != nil {
			t.Errorf("applicationsDB[%q] offers a native clean; the tab is descriptive only", name)
		}
	}
}

// Home items are addressed by path, so a stray absolute path or parent
// traversal would escape the home directory entirely.
func TestHomeItemsAreRelativeAndContained(t *testing.T) {
	for _, it := range homeItems {
		if it.RelPath == "" {
			t.Error("home item with an empty RelPath")
			continue
		}
		if strings.HasPrefix(it.RelPath, "/") || strings.HasPrefix(it.RelPath, "~") {
			t.Errorf("home item %q: RelPath must be relative to $HOME", it.RelPath)
		}
		if strings.Contains(it.RelPath, "..") {
			t.Errorf("home item %q: RelPath must not traverse out of $HOME", it.RelPath)
		}
	}
}

func TestHomeItemsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(homeItems))
	for _, it := range homeItems {
		if seen[it.RelPath] {
			t.Errorf("duplicate home item %q", it.RelPath)
		}
		seen[it.RelPath] = true
	}
}

// --- orphan detection ----------------------------------------------------

func testIndex(ids ...string) AppIndex {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[strings.ToLower(id)] = true
	}
	return AppIndex{ids: m, ready: true}
}

// If the app scan failed we know nothing, and every folder would look
// abandoned. Claiming that would be worse than staying quiet.
func TestOrphanedIsSilentWhenIndexNotReady(t *testing.T) {
	var empty AppIndex
	if empty.Orphaned("com.longgone.App") {
		t.Error("an unready index must never report an orphan")
	}
}

func TestOrphanedDetectsUninstalledApp(t *testing.T) {
	ix := testIndex("com.tinyspeck.slackmacgap", "com.google.Chrome")
	if !ix.Orphaned("com.longgone.EditorPro") {
		t.Error("com.longgone.EditorPro has no installed owner; want orphan")
	}
	if ix.Orphaned("com.google.Chrome") {
		t.Error("com.google.Chrome is installed; must not be an orphan")
	}
}

// Sparkle updaters and helper processes own folders named after a
// sub-identifier of an installed app. Flagging those would make the
// warning noise.
func TestOrphanedRespectsHelperPrefixes(t *testing.T) {
	ix := testIndex("com.hnc.Discord")
	for _, name := range []string{"com.hnc.Discord.ShipIt", "com.hnc.Discord.helper.Renderer"} {
		if ix.Orphaned(name) {
			t.Errorf("%s belongs to an installed app; must not be an orphan", name)
		}
	}
}

// Group containers prefix the vendor's 10-character Team ID onto the
// bundle identifier; it has to come off before comparing.
func TestOrphanedStripsTeamIDPrefix(t *testing.T) {
	ix := testIndex("ru.keepcoder.Telegram")
	if ix.Orphaned("6N38VWS5BX.ru.keepcoder.Telegram") {
		t.Error("Telegram is installed; its group container must not be an orphan")
	}
	if !ix.Orphaned("ABCDE12345.com.vanished.Suite") {
		t.Error("com.vanished.Suite has no installed owner; want orphan")
	}
}

// Apple's folders are overwhelmingly daemons and frameworks with no .app
// anywhere, so they are never actionable leftovers.
func TestOrphanedSkipsAppleAndNonBundleNames(t *testing.T) {
	ix := testIndex("com.google.Chrome")
	for _, name := range []string{
		"com.apple.Safari",
		"com.apple.akd",
		"Homebrew",  // not bundle-ID shaped
		"go-build",  // not bundle-ID shaped
		"group.foo", // too few components to reason about
	} {
		if ix.Orphaned(name) {
			t.Errorf("%s must not be reported as an orphan", name)
		}
	}
}

// Orphan status is a heuristic, so it may inform but must never widen what
// the app is willing to delete.
func TestAnnotateOrphanDoesNotChangeCleanability(t *testing.T) {
	ix := testIndex("com.google.Chrome")
	base := Entry{Score: Risky, Description: "Something.", Effects: "Effects.", Commands: []string{"rm -rf x"}}

	got := AnnotateOrphan(base, RootCaches, "com.vanished.Thing", ix)
	if !got.Orphan {
		t.Fatal("want Orphan set for an uninstalled bundle")
	}
	if got.Score != base.Score {
		t.Errorf("Score = %v, want unchanged %v", got.Score, base.Score)
	}
	if len(got.Commands) != len(base.Commands) {
		t.Errorf("Commands changed: %v, want unchanged %v", got.Commands, base.Commands)
	}
	if !strings.Contains(got.Description, base.Description) {
		t.Error("original Description should be preserved")
	}
	if got.Description == base.Description {
		t.Error("Description should be extended with leftover context")
	}

	// An unknown folder stays unknown and uncleanable even when orphaned.
	unknown := AnnotateOrphan(Entry{Score: Unknown}, RootCaches, "com.vanished.Thing", ix)
	if unknown.CanClean() {
		t.Error("an orphaned Unknown entry must still not be cleanable")
	}
}

func TestAnnotateOrphanLeavesInstalledAppsAlone(t *testing.T) {
	ix := testIndex("com.google.Chrome")
	base := Entry{Score: Safe, Description: "Chrome cache."}
	got := AnnotateOrphan(base, RootCaches, "com.google.Chrome", ix)
	if got.Orphan {
		t.Error("installed app must not be flagged")
	}
	if got.Description != base.Description {
		t.Errorf("Description = %q, want unchanged", got.Description)
	}
}

// --- protected storage ---------------------------------------------------

// OrbStack's VM storage is shown and explained but never acted on, and
// that has to hold for everything nested inside it — drilling in would
// otherwise reach data.img.raw, whose removal destroys every Docker image,
// container and volume on the machine.
func TestProtectCoversOrbStackAndItsContents(t *testing.T) {
	home := "/Users/x/Library/"
	protected := []string{
		home + "Group Containers/HUAQ24HBR6.dev.orbstack",
		home + "Group Containers/HUAQ24HBR6.dev.orbstack/data",
		home + "Group Containers/HUAQ24HBR6.dev.orbstack/data/data.img.raw",
		home + "Caches/dev.kdrag0n.MacVirt",
		home + "Caches/dev.kdrag0n.MacVirt/anything/deeper",
		"/Applications/OrbStack.app",
		"/Applications/OrbStack.app/Contents/MacOS",
	}
	for _, path := range protected {
		got := Protect(Entry{Score: Safe, Description: "x", Commands: []string{"rm -rf x"}}, path)
		if !got.Protected {
			t.Errorf("%s: want Protected", path)
		}
		if got.CanClean() {
			t.Errorf("%s: CanClean() = true, want false", path)
		}
		if got.CanDelete() {
			t.Errorf("%s: CanDelete() = true, want false (not even the manual override)", path)
		}
		if len(got.Commands) != 0 || len(got.CleanPaths) != 0 || got.Native != nil {
			t.Errorf("%s: clean actions survived protection", path)
		}
	}
}

// Protection must not spill onto unrelated folders that merely look similar.
func TestProtectLeavesOtherPathsAlone(t *testing.T) {
	for _, path := range []string{
		"/Users/x/Library/Caches/Google",
		"/Users/x/Library/Group Containers/2BBY89MBSN.dev.warp",
		"/Users/x/Library/Group Containers/6N38VWS5BX.ru.keepcoder.Telegram",
		"/Users/x/Library/Application Support/JetBrains",
		"/Applications/Orbit.app",
		"/Users/x/orbstack-notes",
	} {
		base := Entry{Score: Safe, Description: "x", Commands: []string{"rm -rf x"}}
		got := Protect(base, path)
		if got.Protected {
			t.Errorf("%s: was protected; the rule is meant to cover OrbStack only", path)
		}
		if !got.CanClean() {
			t.Errorf("%s: lost its clean action", path)
		}
	}
}

func TestProtectExplainsItself(t *testing.T) {
	got := Protect(Entry{Score: Risky}, "/Users/x/Library/Group Containers/HUAQ24HBR6.dev.orbstack")
	if got.Description == "" {
		t.Fatal("a protected entry with no dictionary text should still explain itself")
	}
	if !strings.Contains(got.Description, "OrbStack") {
		t.Errorf("Description should name the owning app, got %q", got.Description)
	}
}

// The Applications tab lists the apps themselves, so "is this app
// installed" is incoherent there — and a bundle's folder name is not its
// identifier, so asking anyway mislabels installed apps. zoom.us.app is
// the real case: it parses as a bundle ID but Zoom ships as us.zoom.xos.
func TestAnnotateOrphanSkipsNonLibraryRoots(t *testing.T) {
	ix := testIndex("us.zoom.xos", "com.google.Chrome")
	for _, root := range []Root{RootApplications, RootHome} {
		got := AnnotateOrphan(Entry{Score: Caution, Description: "x"}, root, "zoom.us.app", ix)
		if got.Orphan {
			t.Errorf("%s/zoom.us.app was flagged as a leftover; Zoom is installed", root)
		}
	}
	// The same name under a Library root still gets the normal treatment.
	if got := AnnotateOrphan(Entry{Score: Caution}, RootCaches, "com.gone.App", ix); !got.Orphan {
		t.Error("Caches/com.gone.App should still be flagged")
	}
}

// Dictionary files are merged per root, so a key defined twice would have
// one definition silently win — and the loser might be the safer one.
func TestNoDuplicateDictionaryKeys(t *testing.T) {
	pairs := []struct {
		root   string
		tables map[string]map[string]Entry
	}{
		{"Caches", map[string]map[string]Entry{
			"cachesDB": cachesDB, "ideCachesDB": ideCachesDB, "appleCachesDB": appleCachesDB}},
		{"Application Support", map[string]map[string]Entry{
			"appSupportDB": appSupportDB, "ideAppSupportDB": ideAppSupportDB, "appleAppSupportDB": appleAppSupportDB}},
		{"Containers", map[string]map[string]Entry{
			"containersDB": containersDB, "appleContainersDB": appleContainersDB}},
	}
	for _, p := range pairs {
		seen := make(map[string]string)
		for tableName, table := range p.tables {
			for key := range table {
				if prev, dup := seen[key]; dup {
					t.Errorf("%s: key %q defined in both %s and %s", p.root, key, prev, tableName)
				}
				seen[key] = tableName
			}
		}
	}
}

// Community editions use different folder prefixes (IdeaIC, PyCharmCE)
// than their commercial counterparts, and newer products were added after
// the pattern was first written. Missing one means that IDE's versioned
// folders silently read as Unknown and are never offered for cleaning.
func TestJetBrainsPatternCoversCommunityAndNewerProducts(t *testing.T) {
	for _, name := range []string{
		"IntelliJIdea2026.1", "IdeaIC2026.1", "PyCharm2026.1", "PyCharmCE2026.1",
		"GoLand2026.2", "WebStorm2026.1", "PhpStorm2026.2", "CLion2026.1",
		"Rider2026.1", "DataGrip2026.1", "DataSpell2026.1", "RubyMine2026.1",
		"RustRover2026.2", "Aqua2026.1", "Writerside2026.1", "MPS2026.1",
	} {
		if _, _, _, ok := parseJetBrainsVersion(name); !ok {
			t.Errorf("%s is not recognised as a JetBrains version folder", name)
		}
		if got := Lookup(RootCaches, name); got.Score == Unknown {
			t.Errorf("Caches/%s resolved to Unknown; the pattern should describe it", name)
		}
	}
	// Things that merely look similar must not match.
	for _, name := range []string{"Goland", "Phpstorm", "JetBrains", "Toolbox", "GoLand", "GoLand2026"} {
		if _, _, _, ok := parseJetBrainsVersion(name); ok {
			t.Errorf("%s matched the versioned-folder pattern but is not one", name)
		}
	}
}
