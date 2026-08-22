package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
