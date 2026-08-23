// Package ui is the Bubble Tea layer. It is the only package that imports
// bubbletea/lipgloss; internal/scan and internal/knowledge stay UI-agnostic.
package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/docker"
	"lmkdmvlvkn/internal/history"
	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/scan"
)

type tab int

// Tab order runs cleanup-first: the three tabs that free the most space
// come before the ones that mostly inform, and Results is last because
// it's a record of work already done.
const (
	tabSystemData tab = iota
	tabLeftovers
	tabHome
	tabVendors
	tabApplications
	tabDocker
	tabResults
	tabCount
)

func (t tab) String() string {
	switch t {
	case tabSystemData:
		return "System Data"
	case tabLeftovers:
		return "Leftovers"
	case tabHome:
		return "Home"
	case tabVendors:
		return "Vendors"
	case tabApplications:
		return "Applications"
	case tabDocker:
		return "Docker"
	case tabResults:
		return "Results"
	default:
		return "?"
	}
}

// browsable reports whether a tab shows the folder table (and therefore
// supports selection, cleaning and drilling in) rather than rendering
// something of its own.
func (t tab) browsable() bool {
	switch t {
	case tabSystemData, tabLeftovers, tabHome, tabVendors, tabApplications, tabDocker:
		return true
	default:
		return false
	}
}

// batchAll reports whether a tab offers "act on everything here at once".
// It's the whole point of Leftovers — a pile of folders belonging to apps
// that are already gone — and would be reckless anywhere the rows are
// things the user still uses.
func (t tab) batchAll() bool {
	return t == tabLeftovers
}

type mode int

const (
	modeNormal mode = iota
	modeConfirmClean
	modeConfirmNative
	modeConfirmManualDelete
	modeConfirmBatch
	modeConfirmClearHistory
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
	// pinned keeps the selection on the first row while sizes stream in.
	// Rows are measured in the background and the table re-sorts as each
	// result lands, so a selection fixed to one entry drifts down the list
	// as bigger folders overtake it — dragging the viewport, which follows
	// the selection, into the middle of the table. The user lands on the
	// largest item instead; the first deliberate move clears this.
	pinned  bool
	loading bool
	// pending counts listings still in flight for a synthetic frame that
	// merges several sources; loading clears when it reaches zero.
	pending int
	loadErr string // scoped to this frame, so an error in a subdirectory
	// doesn't linger on screen after navigating back up to one that
	// loaded fine
	// scores caches each entry's safety rating. Ratings depend only on the
	// entry set, never on the sizes that stream in afterwards, so they are
	// computed once per listing rather than on every one of the thousand-
	// odd size results — which otherwise meant well over a million regexp
	// lookups for a single scan.
	scores map[*scan.Entry]knowledge.Score
	// orphan caches which rows belong to uninstalled apps, for the same
	// reason scores is cached: the display filter asks this of every row
	// on every repaint, and answering it means a dictionary lookup plus a
	// prefix scan of every installed bundle ID. Recomputing that ~1200
	// times per frame starved the background size scan.
	orphan map[*scan.Entry]bool
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
	f.scores, f.orphan = nil, nil // the entry set changed; sibling context may have too
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

	// pathDB maps a row's path to its dictionary entry, for the tabs whose
	// rows are identified by path rather than by folder name: the curated
	// Home list, Docker objects, and discovered vendor directories. Those
	// can't go through the name-keyed dictionary the Library tabs use.
	pathDB map[string]knowledge.Entry

	// dockerItems keeps the full Docker object behind each row so the
	// detail panel can show its provenance and layers. The Docker tab
	// deliberately renders differently from the folder tabs: a hash and a
	// size tell a developer nothing, and `docker inspect` already exists —
	// this has to be more useful than that or it has no reason to be here.
	dockerItems map[string]docker.Item

	// libraryRoots are the absolute paths of the scanned Library folders,
	// resolved once so the per-row leftover check isn't recomputing them.
	libraryRoots []string

	// dockerReason explains why the Docker tab is empty when the daemon
	// isn't reachable.
	dockerReason string

	// onlySelected filters every listing down to the batch set, so a long
	// selection can be reviewed as a list instead of being scattered
	// through a thousand rows.
	onlySelected bool

	// appIndex backs orphan detection. It shells out to `defaults read`
	// once per installed app, so it's built in the background and is
	// empty (and therefore answers "not an orphan" to everything) until
	// that finishes.
	appIndex knowledge.AppIndex

	// reclaimCache holds, per folder path, how much a granular (CleanPaths)
	// clean action would actually free — computed lazily in the background
	// (see maybeComputeReclaim) since it requires walking the filesystem.
	reclaimCache map[string]reclaimInfo

	// selected is the batch-clean set, keyed by path, and selOrder is the
	// order the user ticked them in. Both are needed: the map answers "is
	// this row selected" while rendering, and the slice preserves the
	// top-to-bottom order the steps were promised to run in.
	selected map[string]batchStep
	selOrder []string

	// batch is the state of a running or just-finished batch, and batchCh
	// streams progress from the worker goroutine.
	batch   batchResult
	batchCh chan batchProgressMsg

	// history is the persisted record of past deletions, loaded once at
	// startup and appended to as the app cleans.
	history      []history.Record
	historyErr   string
	historyTotal int64
	disk         scan.DiskUsage

	scanner *scan.Scanner

	cleaning     bool
	spinnerFrame int
	statusMsg    string

	// confirmStep is the single action awaiting y/n confirmation.
	confirmStep batchStep
}

// New builds the model with a landing frame for each browsable tab.
func New() Model {
	m := Model{
		// Four workers rather than a token pair: the five Library roots
		// list well over a thousand entries between them, and ~/Library/
		// Containers alone contributes hundreds of small folders that are
		// pure latency to walk one at a time.
		scanner:      scan.NewScanner(4),
		reclaimCache: make(map[string]reclaimInfo),
		pathDB:       make(map[string]knowledge.Entry),
		selected:     make(map[string]batchStep),
		dockerItems:  make(map[string]docker.Item),
	}

	m.navs[tabLeftovers] = []navFrame{{
		id:    m.newFrameID(),
		label: "Leftovers",
		root:  knowledge.RootCaches,
	}}

	for _, r := range knowledge.SystemDataRoots() {
		m.libraryRoots = append(m.libraryRoots, r.Path)
	}

	m.navs[tabSystemData] = []navFrame{{
		id:      m.newFrameID(),
		label:   "System Data",
		root:    knowledge.RootCaches,
		pinned:  true,
		loading: true,
		pending: len(knowledge.SystemDataRoots()),
	}}

	m.navs[tabApplications] = []navFrame{{
		id:      m.newFrameID(),
		label:   "Applications",
		root:    knowledge.RootApplications,
		source:  "App",
		pinned:  true,
		loading: true,
		pending: 1,
	}}

	m.navs[tabVendors] = []navFrame{{
		id:      m.newFrameID(),
		label:   "Vendors",
		root:    knowledge.RootVendors,
		pinned:  true,
		loading: true,
		pending: 1,
	}}

	m.navs[tabDocker] = []navFrame{{
		id:      m.newFrameID(),
		label:   "Docker",
		root:    knowledge.RootDocker,
		pinned:  true,
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
		m.pathDB[path] = it.Entry
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
		pinned:  true,
		entries: entries,
	}
	if len(entries) > 0 {
		f.selected = entries[0].Path
	}
	return f
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForSizeCmd(m.scanner.Results), indexAppsCmd(), loadHistoryCmd()}

	sysID := m.navs[tabSystemData][0].id
	for _, r := range knowledge.SystemDataRoots() {
		cmds = append(cmds, loadDirCmd(sysID, r.Path, r.Label, string(r.Root)))
	}
	cmds = append(cmds, loadAppsCmd(m.navs[tabApplications][0].id), loadDockerCmd(), loadVendorsCmd())

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

type historyLoadedMsg struct {
	records []history.Record
	disk    scan.DiskUsage
	err     error
}

// loadHistoryCmd reads the persisted deletion log and the volume's
// capacity, both of which back the Results tab.
func loadHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		recs, err := history.Load()
		home, herr := os.UserHomeDir()
		if herr != nil {
			home = "/"
		}
		disk, _ := scan.Volume(home)
		return historyLoadedMsg{records: recs, disk: disk, err: err}
	}
}

type historyClearedMsg struct{ err error }

func clearHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		return historyClearedMsg{err: history.Clear()}
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

// stepDoneMsg reports a single-item clean/native/delete finishing.
type stepDoneMsg struct {
	path   string
	name   string
	action batchAction
	freed  int64
	err    error
}

// runStepCmd performs one removal in the background. Single-item actions
// go through exactly the same runStep as a batch, so what gets measured
// and what gets written to the history log can't diverge between them.
func runStepCmd(s batchStep) tea.Cmd {
	return func() tea.Msg {
		freed, err := runStep(s)
		return stepDoneMsg{path: s.path, name: s.name, action: s.action, freed: freed, err: err}
	}
}

// resolveCleanPath turns one CleanPaths pattern into a real path.
//
// Patterns are normally relative to the entry's own folder, which is what
// makes them readable in the dictionary. An absolute pattern is passed
// through untouched: some entries name specific files scattered across a
// tree rather than a subtree — removing one Ollama model means deleting
// its manifest under manifests/ and its blobs under blobs/, which no
// single relative glob can express.
func resolveCleanPath(base, pattern string) string {
	if filepath.IsAbs(pattern) {
		return pattern
	}
	return filepath.Join(base, pattern)
}

func cleanDir(dir string, relPatterns []string) error {
	if len(relPatterns) == 0 {
		return cleanAllChildren(dir)
	}
	var firstErr error
	for _, pat := range relPatterns {
		matches, err := filepath.Glob(resolveCleanPath(dir, pat))
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
			size, _ := scan.GlobSize(resolveCleanPath(basePath, pat))
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

	case dockerLoadedMsg:
		f := &m.navs[tabDocker][0]
		f.loading = false
		f.pending = 0
		f.scores, f.orphan = nil, nil
		m.dockerReason = msg.reason
		switch {
		case !msg.available:
			f.entries = nil
			f.loadErr = ""
		case msg.err != nil:
			f.loadErr = msg.err.Error()
		default:
			f.loadErr = ""
			f.entries = m.dockerEntries(msg.items)
			m.sortFrame(f)
			if len(f.entries) > 0 {
				f.selected = f.entries[0].Path
			}
		}
		return m, nil

	case ollamaLoadedMsg:
		f := m.frameByID(msg.frameID)
		if f == nil {
			return m, nil
		}
		f.loading = false
		f.pending = 0
		f.scores, f.orphan = nil, nil
		if msg.err != nil {
			f.loadErr = msg.err.Error()
			return m, nil
		}
		f.loadErr = ""
		f.entries = m.ollamaEntries(msg.models, msg.orphans, msg.orphanB)
		m.sortFrame(f)
		if len(f.entries) > 0 && f.selected == "" {
			f.selected = f.entries[0].Path
		}
		return m, nil

	case vendorsLoadedMsg:
		f := &m.navs[tabVendors][0]
		f.loading = false
		f.pending = 0
		f.scores, f.orphan = nil, nil
		if msg.err != nil {
			f.loadErr = msg.err.Error()
			return m, nil
		}
		f.loadErr = ""
		f.entries = m.vendorEntries(msg.items)
		m.sortFrame(f)
		if len(f.entries) > 0 {
			f.selected = f.entries[0].Path
		}
		for _, e := range f.entries {
			m.scanner.Enqueue(e.Path)
		}
		return m, nil

	case appIndexMsg:
		m.appIndex = knowledge.AppIndex(msg)
		// Orphan status is what the Leftovers tab is made of, and it only
		// becomes knowable once the installed-app index lands.
		m.rebuildLeftovers()
		return m, nil

	case historyLoadedMsg:
		m.history = msg.records
		m.historyTotal = history.TotalFreed(msg.records)
		m.disk = msg.disk
		if msg.err != nil {
			m.historyErr = msg.err.Error()
		}
		return m, nil

	case historyClearedMsg:
		m.mode = modeNormal
		if msg.err != nil {
			m.statusMsg = "could not clear history: " + msg.err.Error()
			return m, nil
		}
		m.history = nil
		m.historyTotal = 0
		m.historyErr = ""
		m.statusMsg = "history cleared"
		return m, nil

	case batchProgressMsg:
		return m.handleBatchProgress(msg)

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
		f.scores, f.orphan = nil, nil // new rows change the sibling context of existing ones
		m.sortFrame(f)
		if f.selected == "" && len(f.entries) > 0 {
			f.selected = f.entries[0].Path
		}
		for _, e := range msg.entries {
			m.scanner.Enqueue(e.Path)
		}
		// The Leftovers tab is a filtered view of this listing, so it has
		// to be refreshed whenever the listing grows.
		if len(m.navs[tabSystemData]) > 0 && m.navs[tabSystemData][0].id == msg.frameID {
			m.rebuildLeftovers()
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

	case stepDoneMsg:
		m.cleaning = false
		m.mode = modeNormal
		switch {
		case msg.err != nil:
			m.statusMsg = msg.action.String() + " failed: " + msg.err.Error()
		case msg.freed > 0:
			m.statusMsg = msg.action.String() + "ed " + msg.name + ", freed " + formatSize(msg.freed)
		default:
			m.statusMsg = msg.action.String() + "ed " + msg.name
		}

		// A delete removes the thing itself, so its row goes; a clean
		// empties a folder that still exists, so the row stays and is
		// re-measured.
		if msg.action == batchDelete && msg.err == nil {
			delete(m.reclaimCache, msg.path)
			for ti := range m.navs {
				for fi := range m.navs[ti] {
					m.navs[ti][fi].removeEntry(msg.path)
				}
			}
		} else {
			m.invalidateSize(msg.path)
		}
		m.deselect(msg.path)
		m.rebuildLeftovers()
		return m, loadHistoryCmd()
	}
	return m, nil
}

// handleBatchProgress folds one finished step into the running summary,
// records it in the persistent history, and re-arms the listener.
func (m Model) handleBatchProgress(msg batchProgressMsg) (Model, tea.Cmd) {
	if msg.done {
		m.cleaning = false
		m.batch.running = false
		m.clearSelection()
		switch {
		case len(m.batch.failures) == 0:
			m.statusMsg = "cleaned " + strconv.Itoa(m.batch.completed) + " items, freed " + formatSize(m.batch.freed)
		default:
			m.statusMsg = "cleaned " + strconv.Itoa(m.batch.completed) + " items, freed " +
				formatSize(m.batch.freed) + "; " + strconv.Itoa(len(m.batch.failures)) + " failed"
		}
		m.rebuildLeftovers()
		return m, loadHistoryCmd()
	}

	m.batch.index = msg.index + 1
	m.batch.total = msg.total
	m.batch.current = msg.name
	if msg.err != nil {
		m.batch.failures = append(m.batch.failures, msg.name+": "+msg.err.Error())
	} else {
		m.batch.completed++
		m.batch.freed += msg.freed
	}
	m.invalidateSize(m.stepPath(msg.index))
	return m, waitForBatchCmd(m.batchCh)
}

// stepPath recovers the path of the step at index, so a finished step can
// have its row re-measured. The order slice is cleared when the batch
// ends, so this is only ever called mid-run.
func (m Model) stepPath(index int) string {
	if index < 0 || index >= len(m.selOrder) {
		return ""
	}
	return m.selOrder[index]
}

// clearSelection empties the batch set.
func (m *Model) clearSelection() {
	m.selected = make(map[string]batchStep)
	m.selOrder = nil
}

// selectionTotal sums what the current selection is expected to free, and
// reports whether every step's estimate is known yet — granular cleans
// need a background measurement first, and reporting a partial sum as if
// it were final would understate the result.
func (m Model) selectionTotal() (total int64, complete bool) {
	complete = true
	for _, p := range m.selOrder {
		s, ok := m.selected[p]
		if !ok {
			continue
		}
		if !s.estimateReady {
			complete = false
			continue
		}
		total += s.estimate
	}
	return total, complete
}

// orderedSteps materialises the batch in the order the user built it.
func (m Model) orderedSteps() []batchStep {
	steps := make([]batchStep, 0, len(m.selOrder))
	for _, p := range m.selOrder {
		if s, ok := m.selected[p]; ok {
			steps = append(steps, s)
		}
	}
	return steps
}

// rebuildLeftovers refreshes the Leftovers tab from the System Data scan.
//
// It reuses the very same *scan.Entry pointers rather than copying them,
// so sizes measured for the main table show up here for free and a row
// can never disagree with itself across two tabs.
func (m *Model) rebuildLeftovers() {
	src := m.navs[tabSystemData]
	if len(src) == 0 {
		return
	}
	all := src[0].entries
	var entries []*scan.Entry
	for _, e := range all {
		// Deliberately reads the unfiltered listing: System Data hides
		// leftovers precisely because they belong here instead.
		if m.knowledgeIn(all, e).Orphan {
			entries = append(entries, e)
		}
	}

	f := &m.navs[tabLeftovers][0]
	prev := f.selected
	f.entries = entries
	f.scores, f.orphan = nil, nil
	f.loading = src[0].loading
	// Keep the cursor where it was if that row is still a leftover.
	f.selected = ""
	for _, e := range entries {
		if e.Path == prev {
			f.selected = prev
			break
		}
	}
	m.sortFrame(f)
	if f.selected == "" && len(entries) > 0 {
		f.selected = entries[0].Path
	}
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
//
// Scores are cached on the frame: this runs once per size result — over a
// thousand times during a full scan — while the ratings themselves depend
// only on the entry set.
func (m Model) sortFrame(f *navFrame) {
	m.ensureFrameCache(f)
	sortEntries(f.entries, m.sortCol, m.sortAsc, f.scores)
	if f.pinned && len(f.entries) > 0 {
		f.selected = f.entries[0].Path
	}
}

// ensureFrameCache computes the per-entry score and orphan flags once per
// entry set. Both depend only on the listing and the installed-app index,
// never on the sizes that stream in afterwards.
func (m Model) ensureFrameCache(f *navFrame) {
	if f.scores != nil && f.orphan != nil {
		return
	}
	f.scores = make(map[*scan.Entry]knowledge.Score, len(f.entries))
	f.orphan = make(map[*scan.Entry]bool, len(f.entries))
	for _, e := range f.entries {
		k := m.knowledgeIn(f.entries, e)
		f.scores[e] = k.Score
		f.orphan[e] = k.Orphan
	}
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

// hiddenAppleMax is the size below which an Apple system folder is not
// worth a row.
//
// ~/Library/Containers alone holds 738 folders, nearly all of them Apple
// extension hosts and daemons occupying 48-64 KB each. Together they are
// a rounding error on a 228 GB disk, but they bury the handful of rows
// that actually matter. Nothing is deleted by hiding them — they simply
// stop competing for the user's attention.
const hiddenAppleMax = 100 << 10

// currentEntries is the rows the active listing actually shows, after
// filtering. Everything downstream — selection, the cursor, the table —
// uses this, so the filtered view is consistent and the cursor can never
// land on a hidden row.
func (m Model) currentEntries() []*scan.Entry {
	f := m.currentFrame()
	if f == nil {
		return nil
	}
	return m.visibleEntries(f)
}

// visibleEntries applies the display filters to one frame.
func (m Model) visibleEntries(f *navFrame) []*scan.Entry {
	// Filters only apply to a tab's landing listing. Once the user has
	// deliberately drilled into a folder they asked to see its contents,
	// and hiding part of what is in there would be lying about it.
	atRoot := len(m.navs[m.activeTab]) == 1

	if !m.onlySelected && !atRoot {
		return f.entries
	}

	m.ensureFrameCache(f)
	out := make([]*scan.Entry, 0, len(f.entries))
	for _, e := range f.entries {
		if m.onlySelected {
			if _, ok := m.selected[e.Path]; !ok {
				continue
			}
		}
		if atRoot && m.hiddenAtRoot(f, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// hiddenAtRoot reports whether e is filtered out of a landing listing.
func (m Model) hiddenAtRoot(f *navFrame, e *scan.Entry) bool {
	if m.activeTab != tabSystemData {
		return false
	}
	// Leftovers have their own tab; showing them twice makes the System
	// Data list longer without telling the user anything new.
	if f.orphan[e] {
		return true
	}
	// Apple's sub-100 KB system folders — see hiddenAppleMax. Only hidden
	// once measured, so nothing disappears on the strength of a guess.
	if strings.HasPrefix(e.Name, "com.apple.") && e.SizeReady && e.Size < hiddenAppleMax {
		return true
	}
	return false
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
	if k, ok := m.pathDB[e.Path]; ok {
		return knowledge.Protect(k, e.Path)
	}
	siblings := make([]string, 0, len(all))
	for _, s := range all {
		if s.Root == e.Root {
			siblings = append(siblings, s.Name)
		}
	}
	k := knowledge.Effective(knowledge.Root(e.Root), e.Name, siblings)
	// Leftover detection only makes sense for a folder sitting directly
	// inside a Library root: that is what an uninstalled app abandons.
	// A nested subdirectory belongs to whatever contains it, and flagging
	// one would also be unreachable — the Leftovers tab is built from the
	// root listing, so a row flagged deeper down could never appear there.
	if m.isLibraryRootChild(e) {
		k = knowledge.AnnotateOrphan(k, knowledge.Root(e.Root), e.Name, m.appIndex)
	}
	return knowledge.Protect(k, e.Path)
}

// isLibraryRootChild reports whether e sits directly inside one of the
// scanned Library roots, rather than deeper in a subtree.
func (m Model) isLibraryRootChild(e *scan.Entry) bool {
	parent := filepath.Dir(e.Path)
	for _, r := range m.libraryRoots {
		if parent == r {
			return true
		}
	}
	return false
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
