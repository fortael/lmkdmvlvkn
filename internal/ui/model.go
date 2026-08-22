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
	tabHome
	tabApplications
	tabCount
)

func (t tab) String() string {
	switch t {
	case tabSystemData:
		return "System Data"
	case tabDocker:
		return "Docker"
	case tabHome:
		return "Home"
	case tabApplications:
		return "Applications"
	default:
		return "?"
	}
}

// browsable reports whether a tab shows the folder table (and therefore
// supports selection, cleaning and drilling in) rather than a placeholder.
// Docker is handled by a dedicated implementation and stays a placeholder
// until that lands.
func (t tab) browsable() bool {
	return t == tabSystemData || t == tabHome || t == tabApplications
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

// navFrame is one level of the directory browser. Each browsable tab has
// its own stack of these: nav[0] is the tab's landing listing, pressing
// Enter on a directory pushes a frame for its contents, and Esc/Backspace
// pops back up — like a minimal file manager.
//
// nav[0] is not always a real directory. The System Data tab merges five
// separate Library folders into one listing, and the Home tab is a curated
// list of individual paths rather than any single parent, so a frame with
// an empty path is a synthetic listing assembled from elsewhere.
type navFrame struct {
	// id distinguishes frames for message routing. Listings arrive
	// asynchronously and the aggregate frame receives several, so matching
	// on path alone would be ambiguous (the aggregate has no path of its
	// own, and the same folder can be open at two depths).
	id    int
	label string // breadcrumb label (folder name, or the landing listing's name)
	path  string // "" for a synthetic listing
	root  knowledge.Root
	// source is the TYPE column label inherited by child frames. The
	// aggregate frame leaves it empty since its rows carry their own.
	source   string
	entries  []*scan.Entry
	selected string
	loading  bool
	// pending counts listings still in flight for a synthetic frame that
	// merges several sources; loading clears when it reaches zero.
	pending int
	loadErr string // scoped to this frame, so an error in a subdirectory
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

	// navs holds one independent browser stack per tab, so switching tabs
	// preserves how deep the user had navigated in each.
	navs        [tabCount][]navFrame
	nextFrameID int

	detailScroll int

	sortCol sortColumn
	sortAsc bool

	// homeDB maps a Home-tab item's absolute path to its dictionary entry.
	// The Home tab is curated by path rather than discovered by name, so
	// its lookups can't go through the name-keyed dictionary the other
	// tabs use.
	homeDB map[string]knowledge.Entry

	// appIndex backs orphan detection. It shells out to `defaults read`
	// once per installed app, so it's built in the background and is
	// empty (and therefore answers "not an orphan" to everything) until
	// that finishes.
	appIndex knowledge.AppIndex

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

// New builds the model with a landing frame for each browsable tab.
func New() Model {
	m := Model{
		scanner:      scan.NewScanner(2),
		reclaimCache: make(map[string]reclaimInfo),
		homeDB:       make(map[string]knowledge.Entry),
	}

	m.navs[tabSystemData] = []navFrame{{
		id:      m.newFrameID(),
		label:   "System Data",
		root:    knowledge.RootCaches,
		loading: true,
		pending: len(knowledge.SystemDataRoots()),
	}}

	m.navs[tabApplications] = []navFrame{{
		id:      m.newFrameID(),
		label:   "Applications",
		root:    knowledge.RootApplications,
		source:  "App",
		loading: true,
		pending: 1,
	}}

	m.navs[tabHome] = []navFrame{m.buildHomeFrame()}

	return m
}

func (m *Model) newFrameID() int {
	m.nextFrameID++
	return m.nextFrameID
}

// buildHomeFrame turns the curated Home list into a ready-to-display
// frame. Unlike the other tabs there is nothing to list: the paths are
// known up front, so the frame starts populated and only the sizes are
// left to compute in the background.
func (m *Model) buildHomeFrame() navFrame {
	home, err := os.UserHomeDir()
	if err != nil {
		return navFrame{id: m.newFrameID(), label: "Home", root: knowledge.RootHome, loadErr: err.Error()}
	}
	items := knowledge.HomeItems()
	entries := make([]*scan.Entry, 0, len(items))
	for _, it := range items {
		path := filepath.Join(home, it.RelPath)
		name := it.Display
		if name == "" {
			name = "~/" + it.RelPath
		}
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		m.homeDB[path] = it.Entry
		entries = append(entries, &scan.Entry{
			Name:    name,
			Path:    path,
			Source:  "Home",
			Root:    string(knowledge.RootHome),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
			Size:    -1,
		})
	}
	f := navFrame{
		id:      m.newFrameID(),
		label:   "Home",
		root:    knowledge.RootHome,
		source:  "Home",
		entries: entries,
	}
	if len(entries) > 0 {
		f.selected = entries[0].Path
	}
	return f
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForSizeCmd(m.scanner.Results), indexAppsCmd()}

	sysID := m.navs[tabSystemData][0].id
	for _, r := range knowledge.SystemDataRoots() {
		cmds = append(cmds, loadDirCmd(sysID, r.Path, r.Label, string(r.Root)))
	}
	cmds = append(cmds, loadAppsCmd(m.navs[tabApplications][0].id))

	for _, e := range m.navs[tabHome][0].entries {
		m.scanner.Enqueue(e.Path)
	}
	return tea.Batch(cmds...)
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
	frameID int
	entries []*scan.Entry
	err     error
}

func loadDirCmd(frameID int, path, source, root string) tea.Cmd {
	return func() tea.Msg {
		entries, err := scan.List(path, source, root)
		return entriesLoadedMsg{frameID: frameID, entries: entries, err: err}
	}
}

func loadAppsCmd(frameID int) tea.Cmd {
	return func() tea.Msg {
		entries, err := scan.ListApplications(string(knowledge.RootApplications))
		return entriesLoadedMsg{frameID: frameID, entries: entries, err: err}
	}
}

type appIndexMsg knowledge.AppIndex

// indexAppsCmd builds the installed-application index used to flag
// leftover folders. It's a background command because it spawns a
// short-lived process per installed app.
func indexAppsCmd() tea.Cmd {
	return func() tea.Msg {
		return appIndexMsg(knowledge.IndexInstalledApps())
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
// so apps that expect the folder to already exist keep working. When the
// target is a plain file rather than a directory — the Home tab lists a
// few, such as a stray JVM heap dump — the file itself is what gets
// removed, since "the contents of a file" is meaningless.
func cleanAllChildren(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.Remove(dir)
	}
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
		// A native command's working directory only makes sense if it is
		// one; entries whose target is a file (or has vanished) still run
		// fine from the parent.
		if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
			cmd.Dir = filepath.Dir(dir)
		} else {
			cmd.Dir = dir
		}
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
	if !m.activeTab.browsable() || m.mode != modeNormal {
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

	case appIndexMsg:
		m.appIndex = knowledge.AppIndex(msg)
		return m, nil

	case reclaimSizeMsg:
		if m.reclaimCache == nil {
			m.reclaimCache = make(map[string]reclaimInfo)
		}
		m.reclaimCache[msg.path] = reclaimInfo{ready: true, total: msg.total, perPath: msg.perPath}
		return m, nil

	case entriesLoadedMsg:
		f := m.frameByID(msg.frameID)
		if f == nil {
			return m, nil
		}
		if f.pending > 0 {
			f.pending--
		}
		if f.pending == 0 {
			f.loading = false
		}
		if msg.err != nil {
			// A synthetic frame merging several roots keeps whatever did
			// load: one unreadable Library folder shouldn't blank the
			// whole listing, so the error is only surfaced when nothing
			// arrived at all.
			if len(f.entries) == 0 && f.pending == 0 {
				f.loadErr = msg.err.Error()
			}
			return m, nil
		}
		f.loadErr = ""
		f.entries = append(f.entries, msg.entries...)
		m.sortFrame(f)
		if f.selected == "" && len(f.entries) > 0 {
			f.selected = f.entries[0].Path
		}
		for _, e := range msg.entries {
			m.scanner.Enqueue(e.Path)
		}
		return m, nil

	case sizeResultMsg:
		for ti := range m.navs {
			for fi := range m.navs[ti] {
				var touched bool
				for _, e := range m.navs[ti][fi].entries {
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
					m.sortFrame(&m.navs[ti][fi])
				}
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
		m.invalidateSize(msg.path)
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
		m.invalidateSize(msg.path)
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
		for ti := range m.navs {
			for fi := range m.navs[ti] {
				m.navs[ti][fi].removeEntry(msg.path)
			}
		}
		return m, nil
	}
	return m, nil
}

// invalidateSize marks path's cached size stale everywhere it appears and
// requeues it for measurement, so the table reflects what a clean actually
// freed instead of the pre-clean figure.
func (m *Model) invalidateSize(path string) {
	delete(m.reclaimCache, path)
	for ti := range m.navs {
		for fi := range m.navs[ti] {
			for _, e := range m.navs[ti][fi].entries {
				if e.Path == path {
					e.SizeReady = false
					e.Size = -1
				}
			}
		}
	}
	m.scanner.Enqueue(path)
}

// frameByID finds a frame across every tab's stack. Listings are
// asynchronous, so one can arrive for a tab the user isn't looking at, or
// for a frame that has since been popped — in which case this returns nil
// and the result is discarded.
func (m *Model) frameByID(id int) *navFrame {
	for ti := range m.navs {
		for fi := range m.navs[ti] {
			if m.navs[ti][fi].id == id {
				return &m.navs[ti][fi]
			}
		}
	}
	return nil
}

func (m Model) View() string {
	return m.render()
}

// sortFrame orders one frame's entries by the active sort column, scoring
// each entry through the model so Home's path-keyed entries and orphan
// annotations are accounted for the same way the table renders them.
func (m Model) sortFrame(f *navFrame) {
	scoreOf := make(map[*scan.Entry]knowledge.Score, len(f.entries))
	for _, e := range f.entries {
		scoreOf[e] = m.knowledgeIn(f.entries, e).Score
	}
	sortEntries(f.entries, m.sortCol, m.sortAsc, scoreOf)
}

// sortEntries orders entries by col/asc, using precomputed scores.
//
// "Unknown entries always trail, excluded from the sort" only applies when
// sorting BY safety (sortBySafe): that's the one column where "not
// reviewed yet" genuinely isn't a value worth ordering by. It deliberately
// does NOT apply to name/size/mod — most subfolders (e.g. anything inside
// an opened app cache) contain nothing but Unknown entries, since our
// dictionary is keyed by top-level folder names; excluding them there
// would make sorting by size effectively never do anything below the top
// level. For the default composite sort, Unknown's zero score already
// sorts last on its own as a numeric tie-break, so no special case is
// needed there either.
func sortEntries(entries []*scan.Entry, col sortColumn, asc bool, scoreOf map[*scan.Entry]knowledge.Score) {
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
	nav := m.navs[m.activeTab]
	if len(nav) == 0 {
		return nil
	}
	return &nav[len(nav)-1]
}

func (m Model) currentEntries() []*scan.Entry {
	f := m.currentFrame()
	if f == nil {
		return nil
	}
	return f.entries
}

func (m Model) breadcrumb() string {
	nav := m.navs[m.activeTab]
	labels := make([]string, len(nav))
	for i, f := range nav {
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

// knowledgeFor looks up e's dictionary entry using the entries currently on
// screen as sibling context.
func (m Model) knowledgeFor(e *scan.Entry) knowledge.Entry {
	return m.knowledgeIn(m.currentEntries(), e)
}

// knowledgeIn is knowledgeFor with an explicit sibling set, so sorting can
// score a frame that isn't the visible one.
//
// Siblings are restricted to entries sharing e's root: the System Data tab
// merges five Library folders into one listing, and the JetBrains
// version-supersede rule compares folder names, so letting a Logs entry
// count as a sibling of a Caches entry would compare unrelated things.
func (m Model) knowledgeIn(all []*scan.Entry, e *scan.Entry) knowledge.Entry {
	if k, ok := m.homeDB[e.Path]; ok {
		return k
	}
	siblings := make([]string, 0, len(all))
	for _, s := range all {
		if s.Root == e.Root {
			siblings = append(siblings, s.Name)
		}
	}
	k := knowledge.Effective(knowledge.Root(e.Root), e.Name, siblings)
	return knowledge.AnnotateOrphan(k, e.Name, m.appIndex)
}

// tabTotalSize sums the known sizes of a tab's landing listing, for the
// "(12.3 GB)" summary in the tab bar, regardless of how deep the current
// navigation is.
func (m Model) tabTotalSize(t tab) (total int64, complete bool) {
	nav := m.navs[t]
	if len(nav) == 0 {
		return 0, false
	}
	root := nav[0]
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
