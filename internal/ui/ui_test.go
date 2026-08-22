package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/history"
	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/scan"
)

// sized returns a model that has been told the terminal dimensions, which
// is the precondition for View rendering anything but a placeholder.
func sized(t *testing.T, w, h int) Model {
	t.Helper()
	m := New()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

// entry builds a measured table row for tests.
func entry(name, path, source string, root knowledge.Root, size int64) *scan.Entry {
	return &scan.Entry{
		Name:      name,
		Path:      path,
		Source:    source,
		Root:      string(root),
		IsDir:     true,
		Size:      size,
		SizeReady: true,
	}
}

// Every tab must render without panicking, at both the narrow and wide
// detail-panel layouts, before and after data arrives.
func TestAllTabsRender(t *testing.T) {
	for _, dims := range [][2]int{{80, 30}, {160, 50}} {
		m := sized(t, dims[0], dims[1])
		for tb := tab(0); tb < tabCount; tb++ {
			m.activeTab = tb
			out := m.View()
			if strings.TrimSpace(out) == "" {
				t.Errorf("%dx%d tab %s rendered empty", dims[0], dims[1], tb)
			}
		}
	}
}

func TestRenderBeforeWindowSize(t *testing.T) {
	if got := New().View(); strings.TrimSpace(got) == "" {
		t.Error("View before WindowSizeMsg rendered empty; want a loading placeholder")
	}
}

// The System Data landing frame merges several Library roots into one
// table, so its listings arrive as several messages that must accumulate
// rather than replace one another.
func TestSystemDataMergesRootListings(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	roots := knowledge.SystemDataRoots()
	if len(roots) < 2 {
		t.Fatal("test setup: expected several System Data roots")
	}

	for i, r := range roots {
		msg := entriesLoadedMsg{
			frameID: id,
			entries: []*scan.Entry{entry("Folder"+string(rune('A'+i)), "/tmp/x"+string(rune('A'+i)), r.Label, r.Root, 1<<20)},
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}

	f := m.navs[tabSystemData][0]
	if len(f.entries) != len(roots) {
		t.Errorf("merged %d entries, want %d (later listings replaced earlier ones)", len(f.entries), len(roots))
	}
	if f.loading {
		t.Error("frame still loading after every root reported")
	}
	if f.selected == "" {
		t.Error("nothing selected after the first listing arrived")
	}
}

// One unreadable Library folder must not blank out the whole merged
// listing — the other roots still have something to show.
func TestSystemDataKeepsEntriesWhenOneRootFails(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	roots := knowledge.SystemDataRoots()

	next, _ := m.Update(entriesLoadedMsg{
		frameID: id,
		entries: []*scan.Entry{entry("Good", "/tmp/good", "Cache", knowledge.RootCaches, 1<<20)},
	})
	m = next.(Model)
	for range roots[1:] {
		next, _ = m.Update(entriesLoadedMsg{frameID: id, err: errFake{}})
		m = next.(Model)
	}

	f := m.navs[tabSystemData][0]
	if len(f.entries) != 1 {
		t.Errorf("entries = %d, want the 1 that loaded successfully", len(f.entries))
	}
	if f.loadErr != "" {
		t.Errorf("loadErr = %q, want empty while some roots did load", f.loadErr)
	}
}

type errFake struct{}

func (errFake) Error() string { return "permission denied" }

// Each tab keeps its own browser stack, so drilling into a folder on one
// tab and switching away must not disturb the other.
func TestNavigationIsPerTab(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	next, _ := m.Update(entriesLoadedMsg{
		frameID: id,
		entries: []*scan.Entry{entry("JetBrains", "/tmp/jb", "Cache", knowledge.RootCaches, 1<<20)},
	})
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if len(m.navs[tabSystemData]) != 2 {
		t.Fatalf("System Data stack depth = %d, want 2 after Enter", len(m.navs[tabSystemData]))
	}

	m.activeTab = tabHome
	if len(m.navs[tabHome]) != 1 {
		t.Errorf("Home stack depth = %d, want 1 (System Data's navigation leaked)", len(m.navs[tabHome]))
	}
	// Esc on Home must not pop System Data's frame.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if len(m.navs[tabSystemData]) != 2 {
		t.Errorf("System Data stack depth = %d after Esc on Home, want 2", len(m.navs[tabSystemData]))
	}
}

// Drilling in from the merged listing has to carry the row's own root
// forward, or a folder opened from Application Support would be described
// by the Caches dictionary.
func TestOpenSelectedInheritsRowRoot(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	next, _ := m.Update(entriesLoadedMsg{
		frameID: id,
		entries: []*scan.Entry{entry("Google", "/tmp/gs", "AppSupp", knowledge.RootAppSupport, 1<<20)},
	})
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	child := m.navs[tabSystemData][len(m.navs[tabSystemData])-1]
	if child.root != knowledge.RootAppSupport {
		t.Errorf("child frame root = %q, want %q", child.root, knowledge.RootAppSupport)
	}
	if child.source != "AppSupp" {
		t.Errorf("child frame source = %q, want AppSupp", child.source)
	}
}

// An unresearched folder must never reach the confirm dialog, whichever
// key or button asks for it.
func TestCleanRefusesUnknownEntry(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	next, _ := m.Update(entriesLoadedMsg{
		frameID: id,
		entries: []*scan.Entry{entry("totally-unknown-folder", "/tmp/unk", "Cache", knowledge.RootCaches, 1<<20)},
	})
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	if m.mode != modeNormal {
		t.Errorf("mode = %v after cleaning an Unknown folder, want modeNormal (no confirm offered)", m.mode)
	}

	// The manual override is deliberately still available for it.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = next.(Model)
	if m.mode != modeConfirmManualDelete {
		t.Errorf("mode = %v after manual delete, want modeConfirmManualDelete", m.mode)
	}
}

// The Home tab is a curated list resolved at startup, not a directory
// listing, so it arrives populated rather than loading.
func TestHomeTabIsPrepopulated(t *testing.T) {
	m := sized(t, 120, 40)
	f := m.navs[tabHome][0]
	if f.loading {
		t.Error("Home frame is loading; it should be built synchronously")
	}
	for _, e := range f.entries {
		if e.Root != string(knowledge.RootHome) {
			t.Errorf("Home entry %q has root %q, want %q", e.Name, e.Root, knowledge.RootHome)
		}
		if m.knowledgeFor(e).Description == "" {
			t.Errorf("Home entry %q has no description; the curated list should describe everything it lists", e.Name)
		}
	}
}

// Sorting must not crash or lose rows on a table mixing roots, including
// while some sizes are still unmeasured.
func TestSortAcrossMixedRoots(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	entries := []*scan.Entry{
		entry("JetBrains", "/tmp/a", "Cache", knowledge.RootCaches, 3<<20),
		entry("JetBrains", "/tmp/b", "AppSupp", knowledge.RootAppSupport, 9<<20),
		entry("JetBrains", "/tmp/c", "Log", knowledge.RootLogs, 1<<20),
	}
	entries = append(entries, &scan.Entry{
		Name: "Pending", Path: "/tmp/d", Source: "Cache", Root: string(knowledge.RootCaches), IsDir: true, Size: -1,
	})
	next, _ := m.Update(entriesLoadedMsg{frameID: id, entries: entries})
	m = next.(Model)

	for _, col := range []sortColumn{sortByName, sortBySize, sortByMod, sortBySafe, sortDefault} {
		m = m.clickSort(col)
		if got := len(m.navs[tabSystemData][0].entries); got != len(entries) {
			t.Fatalf("sorting by column %v left %d entries, want %d", col, got, len(entries))
		}
		if strings.TrimSpace(m.View()) == "" {
			t.Fatalf("View empty after sorting by column %v", col)
		}
	}
}

// Same-named folders from different roots must resolve to different
// dictionary entries; this is the collision the root scoping exists for.
func TestSameNameDifferentRootsGetDifferentKnowledge(t *testing.T) {
	m := sized(t, 120, 40)
	id := m.navs[tabSystemData][0].id
	cache := entry("Google", "/tmp/cache-google", "Cache", knowledge.RootCaches, 1<<20)
	appSupport := entry("Google", "/tmp/as-google", "AppSupp", knowledge.RootAppSupport, 1<<20)
	next, _ := m.Update(entriesLoadedMsg{frameID: id, entries: []*scan.Entry{cache, appSupport}})
	m = next.(Model)

	kc := m.knowledgeFor(cache)
	ka := m.knowledgeFor(appSupport)
	if kc.Description == ka.Description {
		t.Error("Caches/Google and Application Support/Google resolved to the same entry")
	}
	if len(ka.CleanPaths) == 0 {
		t.Error("Application Support/Google should clean specific subpaths, never the whole profile")
	}
}

// --- batch selection -----------------------------------------------------

// sysDataWith builds a model whose System Data listing holds the given
// rows, with the terminal already sized.
func sysDataWith(t *testing.T, entries ...*scan.Entry) Model {
	t.Helper()
	m := sized(t, 150, 45)
	next, _ := m.Update(entriesLoadedMsg{frameID: m.navs[tabSystemData][0].id, entries: entries})
	return next.(Model)
}

func pressKey(t *testing.T, m Model, s string) Model {
	t.Helper()
	var msg tea.KeyMsg
	if s == " " {
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	} else {
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

// Space ticks a row into the batch; pressing it again unticks.
func TestSpaceTogglesSelection(t *testing.T) {
	m := sysDataWith(t, entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 1<<30))

	m = pressKey(t, m, " ")
	if len(m.selOrder) != 1 {
		t.Fatalf("selected %d rows after space, want 1", len(m.selOrder))
	}
	m = pressKey(t, m, " ")
	if len(m.selOrder) != 0 {
		t.Errorf("selected %d rows after a second space, want 0", len(m.selOrder))
	}
}

// A row the dictionary knows nothing about offers no action, so it can't
// join a batch — the same gate that blocks the single-item clean.
func TestUnknownRowCannotBeSelected(t *testing.T) {
	m := sysDataWith(t, entry("no-such-folder-anywhere", "/tmp/unk", "Cache", knowledge.RootCaches, 1<<20))
	m = pressKey(t, m, " ")
	if len(m.selOrder) != 0 {
		t.Error("an unresearched row was added to the batch")
	}
	if m.statusMsg == "" {
		t.Error("selecting an unusable row should explain why nothing happened")
	}
}

// Protected storage is excluded from every path, batch included.
func TestProtectedRowCannotBeSelected(t *testing.T) {
	home, _ := os.UserHomeDir()
	orb := entry("HUAQ24HBR6.dev.orbstack",
		home+"/Library/Group Containers/HUAQ24HBR6.dev.orbstack", "GroupC", knowledge.RootGroupContainers, 12<<30)
	m := sysDataWith(t, orb)
	m = pressKey(t, m, " ")
	if len(m.selOrder) != 0 {
		t.Error("OrbStack storage was added to a batch; it must never be actionable")
	}
}

// The steps must run in the order the user ticked them, which is the
// order they walked the table in.
func TestBatchPreservesSelectionOrder(t *testing.T) {
	m := sysDataWith(t,
		entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 3<<30),
		entry("Homebrew", "/tmp/brew", "Cache", knowledge.RootCaches, 2<<30),
		entry("node-gyp", "/tmp/gyp", "Cache", knowledge.RootCaches, 1<<30),
	)
	// Walk down the table ticking every row.
	for i := 0; i < 3; i++ {
		m = pressKey(t, m, " ")
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}

	steps := m.orderedSteps()
	if len(steps) != 3 {
		t.Fatalf("queued %d steps, want 3", len(steps))
	}
	for i, want := range []string{"go-build", "Homebrew", "node-gyp"} {
		if steps[i].name != want {
			t.Errorf("step %d = %q, want %q (selection order not preserved)", i, steps[i].name, want)
		}
	}
}

// Where a tool ships its own cleanup command it must be preferred over
// deleting files ourselves — that was the explicit requirement.
func TestBatchPrefersNativeCleanWhenAvailable(t *testing.T) {
	m := sysDataWith(t, entry("Homebrew", "/tmp/brew", "Cache", knowledge.RootCaches, 1<<30))
	m = pressKey(t, m, " ")

	steps := m.orderedSteps()
	if len(steps) != 1 {
		t.Fatalf("queued %d steps, want 1", len(steps))
	}
	if steps[0].action != batchNative {
		t.Errorf("action = %v, want batchNative (Homebrew defines one)", steps[0].action)
	}
	if steps[0].command == "" {
		t.Error("a native step must carry the command to run")
	}
}

// Without a native command the step falls back to the dictionary's own
// clean paths.
func TestBatchFallsBackToCleanWithoutNative(t *testing.T) {
	m := sysDataWith(t, entry("node-gyp", "/tmp/gyp", "Cache", knowledge.RootCaches, 1<<30))
	m = pressKey(t, m, " ")
	steps := m.orderedSteps()
	if len(steps) != 1 || steps[0].action != batchClean {
		t.Fatalf("steps = %+v, want a single batchClean", steps)
	}
}

// The button has to show what the user is about to get back.
func TestSelectionTotalSumsWholeFolderEstimates(t *testing.T) {
	m := sysDataWith(t,
		entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 3<<30),
		entry("node-gyp", "/tmp/gyp", "Cache", knowledge.RootCaches, 1<<30),
	)
	m = pressKey(t, m, " ")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	m = pressKey(t, m, " ")

	total, complete := m.selectionTotal()
	if !complete {
		t.Error("both rows have known sizes and no CleanPaths; the total should be exact")
	}
	if total != 4<<30 {
		t.Errorf("total = %d, want %d", total, int64(4)<<30)
	}
}

// x drops the whole queue without running anything.
func TestClearSelectionEmptiesTheQueue(t *testing.T) {
	m := sysDataWith(t, entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 1<<30))
	m = pressKey(t, m, " ")
	m = pressKey(t, m, "x")
	if len(m.selOrder) != 0 || len(m.selected) != 0 {
		t.Errorf("selection survived x: %d order / %d map", len(m.selOrder), len(m.selected))
	}
}

// c opens the confirm screen rather than deleting immediately.
func TestRunBatchAsksForConfirmationFirst(t *testing.T) {
	m := sysDataWith(t, entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 1<<30))
	m = pressKey(t, m, " ")
	m = pressKey(t, m, "c")
	if m.mode != modeConfirmBatch {
		t.Fatalf("mode = %v, want modeConfirmBatch", m.mode)
	}
	if m.cleaning {
		t.Error("the batch started without confirmation")
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Error("the batch confirm screen rendered empty")
	}
}

// An empty queue has nothing to confirm.
func TestRunBatchDoesNothingWithEmptySelection(t *testing.T) {
	m := sysDataWith(t, entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 1<<30))
	m = pressKey(t, m, "c")
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.mode)
	}
}

// --- leftovers -----------------------------------------------------------

// The Leftovers tab is a live projection of the System Data scan: only
// rows whose owning app is gone, sharing the very same entry pointers.
func TestLeftoversTabCollectsOrphansOnly(t *testing.T) {
	m := sized(t, 150, 45)
	m.appIndex = knowledge.AppIndexForTest("com.google.Chrome")

	live := entry("com.google.Chrome", "/tmp/chrome", "Cache", knowledge.RootCaches, 1<<20)
	gone := entry("com.vanished.EditorPro", "/tmp/gone", "Cache", knowledge.RootCaches, 5<<20)
	next, _ := m.Update(entriesLoadedMsg{frameID: m.navs[tabSystemData][0].id, entries: []*scan.Entry{live, gone}})
	m = next.(Model)

	got := m.navs[tabLeftovers][0].entries
	if len(got) != 1 {
		t.Fatalf("Leftovers holds %d rows, want 1", len(got))
	}
	if got[0].Name != "com.vanished.EditorPro" {
		t.Errorf("Leftovers holds %q, want the uninstalled app's folder", got[0].Name)
	}
	if got[0] != gone {
		t.Error("Leftovers should reuse the System Data entry pointer so sizes stay in sync")
	}
}

// Select-all is what makes the tab worth having; every row must queue as
// a whole-folder delete, since the owning app is already gone.
func TestLeftoversSelectAllQueuesDeletes(t *testing.T) {
	m := sized(t, 150, 45)
	m.appIndex = knowledge.AppIndexForTest("com.google.Chrome")
	next, _ := m.Update(entriesLoadedMsg{
		frameID: m.navs[tabSystemData][0].id,
		entries: []*scan.Entry{
			entry("com.gone.One", "/tmp/one", "Cache", knowledge.RootCaches, 1<<20),
			entry("com.gone.Two", "/tmp/two", "Cache", knowledge.RootCaches, 2<<20),
		},
	})
	m = next.(Model)
	m.activeTab = tabLeftovers

	m = pressKey(t, m, "a")
	steps := m.orderedSteps()
	if len(steps) != 2 {
		t.Fatalf("queued %d steps, want 2", len(steps))
	}
	for _, s := range steps {
		if s.action != batchDelete {
			t.Errorf("%s queued as %v, want batchDelete", s.name, s.action)
		}
	}
	if total, _ := m.selectionTotal(); total != 3<<20 {
		t.Errorf("total = %d, want %d", total, int64(3)<<20)
	}
}

// --- results -------------------------------------------------------------

func TestResultsTabRendersEmptyAndPopulated(t *testing.T) {
	m := sized(t, 150, 45)
	m.activeTab = tabResults
	if strings.TrimSpace(m.View()) == "" {
		t.Error("Results rendered empty with no history")
	}

	next, _ := m.Update(historyLoadedMsg{
		records: []history.Record{
			{Time: time.Now(), Name: "go-build", Path: "/tmp/gb", Source: "System Data",
				Action: history.ActionNative, Freed: 2 << 30},
			{Time: time.Now().Add(-time.Hour), Name: "broken", Path: "/tmp/b",
				Action: history.ActionClean, Err: "permission denied"},
		},
		disk: scan.DiskUsage{Total: 200 << 30, Free: 20 << 30, Used: 180 << 30},
	})
	m = next.(Model)

	if m.historyTotal != 2<<30 {
		t.Errorf("historyTotal = %d, want %d (failures contribute nothing)", m.historyTotal, int64(2)<<30)
	}
	out := m.View()
	if !strings.Contains(out, "go-build") {
		t.Error("Results should list the cleaned entry")
	}
	if strings.TrimSpace(out) == "" {
		t.Error("Results rendered empty with history present")
	}
}

func TestClearHistoryAsksFirst(t *testing.T) {
	m := sized(t, 150, 45)
	m.activeTab = tabResults
	next, _ := m.Update(historyLoadedMsg{
		records: []history.Record{{Time: time.Now(), Name: "x", Freed: 1 << 20}},
		disk:    scan.DiskUsage{Total: 100 << 30},
	})
	m = next.(Model)

	m = pressKey(t, m, "c")
	if m.mode != modeConfirmClearHistory {
		t.Fatalf("mode = %v, want modeConfirmClearHistory", m.mode)
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Error("the clear-history confirm rendered empty")
	}
}

// Seven tabs overflow a normal terminal once every label carries a size
// suffix; the bar must shed the suffixes rather than wrap.
func TestTabBarDropsSizesWhenTooNarrow(t *testing.T) {
	m := sized(t, 60, 45)
	next, _ := m.Update(entriesLoadedMsg{
		frameID: m.navs[tabSystemData][0].id,
		entries: []*scan.Entry{entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 9<<30)},
	})
	m = next.(Model)

	if got := m.tabLabel(tabSystemData); got != "System Data" {
		t.Errorf("narrow tab label = %q, want the bare name", got)
	}

	wide := sized(t, 220, 45)
	next, _ = wide.Update(entriesLoadedMsg{
		frameID: wide.navs[tabSystemData][0].id,
		entries: []*scan.Entry{entry("go-build", "/tmp/gb", "Cache", knowledge.RootCaches, 9<<30)},
	})
	wide = next.(Model)
	if got := wide.tabLabel(tabSystemData); !strings.Contains(got, "GB") {
		t.Errorf("wide tab label = %q, want a size summary", got)
	}
}

// --- docker rows are not files -------------------------------------------

func dockerEntry(name, id string, size int64) *scan.Entry {
	return &scan.Entry{
		Name: name, Path: "docker://Image/" + id, Source: "Image",
		Root: string(knowledge.RootDocker), Size: size, SizeReady: true,
	}
}

// A Docker object's "path" is an identifier. It must never reach
// os.RemoveAll, which is what the manual-delete override would do.
func TestDockerRowRefusesManualDelete(t *testing.T) {
	m := sized(t, 150, 45)
	m.activeTab = tabDocker
	f := &m.navs[tabDocker][0]
	f.loading = false
	f.entries = []*scan.Entry{dockerEntry("postgres:16", "abc123", 400<<20)}
	f.selected = f.entries[0].Path

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = next.(Model)
	if m.mode == modeConfirmManualDelete {
		t.Error("manual delete was offered for a Docker object; there is no file to delete")
	}
	if m.statusMsg == "" {
		t.Error("refusing manual delete should say why")
	}
}

// Selecting a Docker row queues its own CLI command, flagged virtual so
// runStep never stats or removes the synthetic path.
func TestDockerStepIsVirtualAndNative(t *testing.T) {
	m := sized(t, 150, 45)
	m.activeTab = tabDocker
	e := dockerEntry("dangling", "def456", 700<<20)
	m.pathDB[e.Path] = knowledge.Entry{
		Score:       knowledge.Safe,
		Description: "A dangling image.",
		Native:      &knowledge.NativeClean{Description: "d", Command: "docker rmi def456"},
	}
	f := &m.navs[tabDocker][0]
	f.loading = false
	f.entries = []*scan.Entry{e}
	f.selected = e.Path

	step, ok := m.selectableStep(e)
	if !ok {
		t.Fatal("a removable Docker object should be selectable")
	}
	if !step.virtual {
		t.Error("Docker steps must be marked virtual")
	}
	if step.action != batchNative || step.command != "docker rmi def456" {
		t.Errorf("step = %v/%q, want batchNative running Docker's own command", step.action, step.command)
	}
	if step.estimate != 700<<20 || !step.estimateReady {
		t.Errorf("estimate = %d/%v, want Docker's reported size", step.estimate, step.estimateReady)
	}
}

// The guard itself: a virtual step that somehow asks for a filesystem
// removal must fail rather than touch the disk.
func TestRunStepRefusesVirtualFilesystemRemoval(t *testing.T) {
	dir := t.TempDir()
	freed, err := runStep(batchStep{path: dir, name: "x", virtual: true, action: batchDelete})
	if err == nil {
		t.Fatal("a virtual delete step must be refused")
	}
	if freed != 0 {
		t.Errorf("freed = %d, want 0", freed)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Error("the refused step deleted the directory anyway")
	}
}
