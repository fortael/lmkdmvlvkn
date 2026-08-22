// Package ui is the Bubble Tea layer. It is the only package that imports
// bubbletea/lipgloss; internal/scan and internal/knowledge stay UI-agnostic.
package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/scan"
)

type tab int

const (
	tabSystemData tab = iota
	tabDocker
	tabFolders
	tabApplications
	tabCount
)

func (t tab) String() string {
	switch t {
	case tabSystemData:
		return "System Data"
	case tabDocker:
		return "Docker"
	case tabFolders:
		return "Folders"
	case tabApplications:
		return "Applications"
	default:
		return "?"
	}
}

type mode int

const (
	modeNormal mode = iota
	modeConfirmClean
	modeConfirmNative
	modeConfirmManualDelete
)

// sortColumn selects which field the table is ordered by. sortDefault is
// the built-in composite order (size, then safety) shown before the user
// clicks any column header.
type sortColumn int

const (
	sortDefault sortColumn = iota
	sortByName
	sortBySize
	sortByMod
	sortBySafe
)

// navFrame is one level of the directory browser within System Data.
// nav[0] is always the scanned root (e.g. ~/Library/Caches); pressing
// Enter on a directory entry pushes a new frame for its contents, and
// Esc/Backspace pops back up — like a minimal file manager.
type navFrame struct {
	label    string // breadcrumb label (folder name, or root's short name)
	path     string
	source   string // TYPE column label, inherited from the root
	entries  []*scan.Entry
	selected string
	loading  bool
	loadErr  string // scoped to this frame, so an error in a subdirectory
	// doesn't linger on screen after navigating back up to one that
	// loaded fine
}

// removeEntry drops the entry at path from the frame, if present, and
// moves the selection to whatever's now at the same index (or the last
// entry, if that was the last row) so the cursor doesn't end up pointing
// at nothing after a manual delete.
func (f *navFrame) removeEntry(path string) {
	idx := -1
	for i, e := range f.entries {
		if e.Path == path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	f.entries = append(f.entries[:idx], f.entries[idx+1:]...)
	if f.selected != path {
		return
	}
	switch {
	case len(f.entries) == 0:
		f.selected = ""
	case idx < len(f.entries):
		f.selected = f.entries[idx].Path
	default:
		f.selected = f.entries[len(f.entries)-1].Path
	}
}

// tabRegion is a mouse hit-test rectangle for a clickable tab, rebuilt
// every render since tab widths depend on current content (size summary).
type tabRegion struct {
	tab    tab
	x0, x1 int // inclusive column range on the tab bar
}

// Model is the root Bubble Tea model.
type Model struct {
	width, height int
	ready         bool

	activeTab tab
	mode      mode

	nav          []navFrame
	detailScroll int

	sortCol sortColumn
	sortAsc bool

	// reclaimCache holds, per folder path, how much a granular (CleanPaths)
	// clean action would actually free — computed lazily in the background
	// (see maybeComputeReclaim) since it requires walking the filesystem.
	reclaimCache map[string]reclaimInfo

	scanner *scan.Scanner

	cleaning     bool
	spinnerFrame int
	statusMsg    string

	confirmPath       string
	confirmCleanPaths []string

	confirmNativeCmd   string
	confirmNativeLabel string
}

// New builds the model rooted at ~/Library/Caches.
func New() Model {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Library", "Caches")
	return Model{
		nav: []navFrame{{
			label:   "Caches",
			path:    root,
			source:  "Cache",
			loading: true,
		}},
		scanner: scan.NewScanner(2),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadDirCmd(m.nav[0].path, m.nav[0].source), waitForSizeCmd(m.scanner.Results))
}

type spinnerTickMsg struct{}

// spinnerTickCmd drives the "cleaning…" spinner animation. It's only
// rescheduled by the Update handler while m.cleaning is true, so the tick
// chain stops itself once a clean/native-clean finishes rather than
// running forever in the background.
func spinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

type entriesLoadedMsg struct {
	path    string
	entries []*scan.Entry
	err     error
}

func loadDirCmd(path, source string) tea.Cmd {
	return func() tea.Msg {
		entries, err := scan.List(path, source)
		return entriesLoadedMsg{path: path, entries: entries, err: err}
	}
}

type sizeResultMsg scan.SizeResult

func waitForSizeCmd(results <-chan scan.SizeResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-results
		if !ok {
			return nil
		}
		return sizeResultMsg(r)
	}
}

type cleanDoneMsg struct {
	path string
	err  error
}

// cleanCmd removes dir's contents. If relPatterns is empty, every direct
// child of dir is removed (the common case). Otherwise only files/dirs
// matching those glob patterns (relative to dir) are removed, leaving the
// rest of dir untouched — used where wiping the whole folder would be too
// broad (e.g. a JetBrains IDE version still in use).
func cleanCmd(path string, relPatterns []string) tea.Cmd {
	return func() tea.Msg {
		err := cleanDir(path, relPatterns)
		return cleanDoneMsg{path: path, err: err}
	}
}

func cleanDir(dir string, relPatterns []string) error {
	if len(relPatterns) == 0 {
		return cleanAllChildren(dir)
	}
	var firstErr error
	for _, pat := range relPatterns {
		matches, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// cleanAllChildren removes the contents of dir (not the directory itself),
// so apps that expect the folder to already exist keep working.
func cleanAllChildren(dir string) error {
	items, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var firstErr error
	for _, it := range items {
		if err := os.RemoveAll(filepath.Join(dir, it.Name())); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type manualDeleteDoneMsg struct {
	path string
	err  error
}

// manualDeleteCmd removes path entirely — the folder itself, not just its
// contents. Unlike cleanCmd, this bypasses the knowledge base completely:
// it's the always-available manual override for when the user wants to
// delete something we haven't researched (or wants to go further than a
// researched entry's own Commands/CleanPaths would), entirely at their own
// risk.
func manualDeleteCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return manualDeleteDoneMsg{path: path, err: os.RemoveAll(path)}
	}
}

type nativeCleanDoneMsg struct {
	path    string
	err     error
	summary string // last non-blank line of output, for a one-line status
}

// nativeCleanCmd runs command (one of the fixed, hand-written strings in
// the knowledge base — never user input) via the shell, with dir as its
// working directory.
//
// Setsid detaches the child into its own session so it has no controlling
// terminal. Some tools (Homebrew's Ruby-based cleanup among them) open
// /dev/tty directly for progress output when they detect an interactive
// terminal, which bypasses CombinedOutput's pipe entirely and writes
// straight into our alt-screen buffer — corrupting it in a way Bubble Tea
// has no way to know about or repaint over. Without a controlling
// terminal, that /dev/tty open fails and the tool falls back to plain
// stdout, which we do capture.
func nativeCleanCmd(dir, command string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		out, err := cmd.CombinedOutput()
		return nativeCleanDoneMsg{path: dir, err: err, summary: lastLine(string(out))}
	}
}

// lastLine returns the last non-blank line of s, for compressing a
// command's full output down to something that fits on the one-line
// status bar.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// reclaimInfo is how much disk space a folder's granular (CleanPaths)
// clean action would free, broken down per pattern. ready is false while
// the background computation is still in flight.
type reclaimInfo struct {
	ready   bool
	total   int64
	perPath map[string]int64
}

type reclaimSizeMsg struct {
	path    string
	total   int64
	perPath map[string]int64
}

// reclaimSizeCmd walks each CleanPaths pattern under basePath to find out
// how much space cleaning it would actually free. This is real filesystem
// I/O (recursive for any pattern matching a directory), so it only ever
// runs in the background, on demand, for whichever entry is currently
// selected — never inline during rendering.
func reclaimSizeCmd(basePath string, patterns []string) tea.Cmd {
	return func() tea.Msg {
		perPath := make(map[string]int64, len(patterns))
		var total int64
		for _, pat := range patterns {
			if strings.HasPrefix(pat, "#") {
				continue
			}
			size, _ := scan.GlobSize(filepath.Join(basePath, pat))
			perPath[pat] = size
			total += size
		}
		return reclaimSizeMsg{path: basePath, total: total, perPath: perPath}
	}
}

// maybeComputeReclaim returns a command to compute reclaimable space for
// the current selection, if it has granular CleanPaths and isn't already
// cached or in flight — nil otherwise. Called after every Update so it
// doesn't need to be threaded through every selection-changing call site
// individually (moveSelection, openSelected, goUp, clickSort, ...).
func (m *Model) maybeComputeReclaim() tea.Cmd {
	if m.activeTab != tabSystemData || m.mode != modeNormal {
		return nil
	}
	e := m.selectedEntry()
	if e == nil {
		return nil
	}
	k := m.knowledgeFor(e)
	if len(k.CleanPaths) == 0 {
		return nil
	}
	if _, ok := m.reclaimCache[e.Path]; ok {
		return nil
	}
	if m.reclaimCache == nil {
		m.reclaimCache = make(map[string]reclaimInfo)
	}
	m.reclaimCache[e.Path] = reclaimInfo{} // placeholder: marks "in flight"
	return reclaimSizeCmd(e.Path, k.CleanPaths)
}

// Update is the public tea.Model entry point. It delegates to dispatch for
// the actual message handling (which works with the concrete Model type so
// handlers can be composed and unit-testable), then opportunistically
// kicks off a background reclaim-size computation for whatever's now
// selected — see maybeComputeReclaim.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.dispatch(msg)
	if extra := newModel.maybeComputeReclaim(); extra != nil {
		cmd = tea.Batch(cmd, extra)
	}
	return newModel, cmd
}

func (m Model) dispatch(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case spinnerTickMsg:
		if !m.cleaning {
			return m, nil
		}
		m.spinnerFrame++
		return m, spinnerTickCmd()

	case reclaimSizeMsg:
		if m.reclaimCache == nil {
			m.reclaimCache = make(map[string]reclaimInfo)
		}
		m.reclaimCache[msg.path] = reclaimInfo{ready: true, total: msg.total, perPath: msg.perPath}
		return m, nil

	case entriesLoadedMsg:
		for i := range m.nav {
			if m.nav[i].path != msg.path {
				continue
			}
			m.nav[i].loading = false
			if msg.err != nil {
				m.nav[i].loadErr = msg.err.Error()
				break
			}
			m.nav[i].loadErr = ""
			entries := msg.entries
			sortEntries(entries, m.sortCol, m.sortAsc)
			m.nav[i].entries = entries
			if len(entries) > 0 {
				m.nav[i].selected = entries[0].Path
			}
			for _, e := range entries {
				m.scanner.Enqueue(e.Path)
			}
			break
		}
		return m, nil

	case sizeResultMsg:
		for fi := range m.nav {
			var touched bool
			for _, e := range m.nav[fi].entries {
				if e.Path == msg.Path {
					e.Size = msg.Size
					if !msg.ModTime.IsZero() {
						e.ModTime = msg.ModTime
					}
					e.SizeErr = msg.Err
					e.SizeReady = true
					touched = true
					break
				}
			}
			if touched {
				sortEntries(m.nav[fi].entries, m.sortCol, m.sortAsc)
			}
		}
		return m, waitForSizeCmd(m.scanner.Results)

	case cleanDoneMsg:
		m.cleaning = false
		m.mode = modeNormal
		if msg.err != nil {
			m.statusMsg = "clean failed: " + msg.err.Error()
		} else {
			m.statusMsg = "cleaned " + filepath.Base(msg.path)
		}
		delete(m.reclaimCache, msg.path)
		for fi := range m.nav {
			for _, e := range m.nav[fi].entries {
				if e.Path == msg.path {
					e.SizeReady = false
					e.Size = -1
				}
			}
		}
		m.scanner.Enqueue(msg.path)
		return m, nil

	case nativeCleanDoneMsg:
		m.cleaning = false
		m.mode = modeNormal
		switch {
		case msg.err != nil:
			m.statusMsg = "native clean failed: " + msg.err.Error()
		case msg.summary != "":
			m.statusMsg = "native clean done: " + msg.summary
		default:
			m.statusMsg = "native clean done"
		}
		delete(m.reclaimCache, msg.path)
		for fi := range m.nav {
			for _, e := range m.nav[fi].entries {
				if e.Path == msg.path {
					e.SizeReady = false
					e.Size = -1
				}
			}
		}
		m.scanner.Enqueue(msg.path)
		return m, nil

	case manualDeleteDoneMsg:
		m.cleaning = false
		m.mode = modeNormal
		if msg.err != nil {
			m.statusMsg = "delete failed: " + msg.err.Error()
			return m, nil
		}
		m.statusMsg = "deleted " + filepath.Base(msg.path)
		delete(m.reclaimCache, msg.path)
		for fi := range m.nav {
			m.nav[fi].removeEntry(msg.path)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	return m.render()
}

// sortEntries orders entries by col/asc. Score is computed with each
// entry's own listing as sibling context, so JetBrains-style version
// comparisons stay correct no matter which directory is being sorted.
//
// "Unknown entries always trail, excluded from the sort" only applies when
// sorting BY safety (sortBySafe): that's the one column where "not
// reviewed yet" genuinely isn't a value worth ordering by. It deliberately
// does NOT apply to name/size/mod — most subfolders (e.g. anything inside
// an opened app cache) contain nothing but Unknown entries, since our
// dictionary is keyed by top-level cache folder names; excluding them
// there would make sorting by size effectively never do anything below
// the top level. For the default composite sort, Unknown's zero score
// already sorts last on its own as a numeric tie-break, so no special
// case is needed there either.
func sortEntries(entries []*scan.Entry, col sortColumn, asc bool) {
	siblings := make([]string, len(entries))
	for i, e := range entries {
		siblings[i] = e.Name
	}
	scoreOf := make(map[*scan.Entry]knowledge.Score, len(entries))
	for _, e := range entries {
		scoreOf[e] = knowledge.Effective(e.Name, siblings).Score
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]

		if col == sortBySafe {
			as, bs := scoreOf[a], scoreOf[b]
			aUnknown, bUnknown := as == knowledge.Unknown, bs == knowledge.Unknown
			if aUnknown != bUnknown {
				return !aUnknown
			}
			if aUnknown && bUnknown {
				return a.Name < b.Name
			}
			if as != bs {
				if asc {
					return as < bs
				}
				return as > bs
			}
			return a.Name < b.Name
		}

		switch col {
		case sortByName:
			if asc {
				return a.Name < b.Name
			}
			return a.Name > b.Name
		case sortByMod:
			if asc {
				return a.ModTime.Before(b.ModTime)
			}
			return a.ModTime.After(b.ModTime)
		default: // sortDefault, sortBySize
			if a.SizeReady != b.SizeReady {
				return a.SizeReady
			}
			if a.SizeReady && b.SizeReady && a.Size != b.Size {
				if asc {
					return a.Size < b.Size
				}
				return a.Size > b.Size
			}
			if col == sortDefault {
				if as, bs := scoreOf[a], scoreOf[b]; as != bs {
					return as > bs
				}
			}
			return a.Name < b.Name
		}
	})
}

func (m Model) currentFrame() *navFrame {
	if len(m.nav) == 0 {
		return nil
	}
	return &m.nav[len(m.nav)-1]
}

func (m Model) currentEntries() []*scan.Entry {
	f := m.currentFrame()
	if f == nil {
		return nil
	}
	return f.entries
}

func (m Model) breadcrumb() string {
	labels := make([]string, len(m.nav))
	for i, f := range m.nav {
		labels[i] = f.label
	}
	return strings.Join(labels, " › ")
}

func (m Model) selectedIndex() int {
	f := m.currentFrame()
	if f == nil {
		return -1
	}
	for i, e := range f.entries {
		if e.Path == f.selected {
			return i
		}
	}
	return -1
}

func (m Model) selectedEntry() *scan.Entry {
	idx := m.selectedIndex()
	entries := m.currentEntries()
	if idx < 0 || idx >= len(entries) {
		return nil
	}
	return entries[idx]
}

// knowledgeFor looks up e's dictionary entry with sibling context — e.g. so
// a JetBrains IDE version folder knows whether a newer install of the same
// IDE exists among its siblings and can be rated/cleaned accordingly. It's
// safe to call for any entry: names that don't need sibling context just
// fall through to a plain lookup.
func (m Model) knowledgeFor(e *scan.Entry) knowledge.Entry {
	entries := m.currentEntries()
	siblings := make([]string, len(entries))
	for i, s := range entries {
		siblings[i] = s.Name
	}
	return knowledge.Effective(e.Name, siblings)
}

// rootTotalSize sums the known sizes of the top-level scan root, used for
// the "(12.3 GB)" summary next to the System Data tab regardless of how
// deep the current navigation is.
func (m Model) rootTotalSize() (total int64, complete bool) {
	if len(m.nav) == 0 {
		return 0, false
	}
	root := m.nav[0]
	complete = !root.loading && len(root.entries) > 0
	for _, e := range root.entries {
		if !e.SizeReady {
			complete = false
			continue
		}
		total += e.Size
	}
	return total, complete
}

func maxSize(entries []*scan.Entry) int64 {
	var max int64
	for _, e := range entries {
		if e.SizeReady && e.Size > max {
			max = e.Size
		}
	}
	return max
}

// timeAgo renders a short relative-time string like claude-keeper's status
// lines ("5m", "3h", "12d").
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}
