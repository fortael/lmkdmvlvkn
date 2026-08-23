package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/knowledge"
)

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.mode != modeNormal {
		return m.handleConfirmKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "tab", "right", "l":
		m.activeTab = (m.activeTab + 1) % tabCount
		m.detailScroll = 0
		return m, nil

	case "shift+tab", "left", "h":
		m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
		m.detailScroll = 0
		return m, nil

	case "1", "2", "3", "4", "5", "6", "7":
		if n := int(msg.String()[0] - '1'); n < int(tabCount) {
			m.activeTab = tab(n)
			m.detailScroll = 0
		}
		return m, nil

	case " ":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.toggleSelection()

	case "a":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.selectAll()

	case "c":
		if m.activeTab == tabResults {
			m.mode = modeConfirmClearHistory
			return m, nil
		}
		return m.startBatch()

	case "x":
		m.clearSelection()
		if m.onlySelected {
			m.onlySelected = false
			m.reconcileSelection()
		}
		return m, nil

	case "g", "home":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.applyNavAction(navTop)

	case "G", "end":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.applyNavAction(navBottom)

	case "s":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.applyNavAction(navResetSort)

	case "f":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.applyNavAction(navToggleSelected)

	case "up", "k":
		if m.activeTab.browsable() {
			m.moveSelection(-1)
		}
		return m, nil

	case "down", "j":
		if m.activeTab.browsable() {
			m.moveSelection(1)
		}
		return m, nil

	case "pgup", "K":
		if m.activeTab.browsable() {
			m.detailScroll -= detailScrollStep
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
		}
		return m, nil

	case "pgdown", "J":
		if m.activeTab.browsable() {
			m.detailScroll += detailScrollStep
		}
		return m, nil

	case "enter":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.openSelected()

	case "esc", "backspace":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.goUp(), nil

	case "r":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.rescan()

	case "d", "delete":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.startClean()

	case "n":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.startNativeClean()

	case "D":
		if !m.activeTab.browsable() {
			return m, nil
		}
		return m.startManualDelete()
	}
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		switch m.mode {
		case modeConfirmClean, modeConfirmNative, modeConfirmManualDelete:
			m.mode = modeNormal
			m.cleaning = true
			m.spinnerFrame = 0
			step := m.confirmStep
			m.confirmStep = batchStep{}
			m.statusMsg = step.action.String() + "ing..."
			return m, tea.Batch(runStepCmd(step), spinnerTickCmd())

		case modeConfirmBatch:
			m.mode = modeNormal
			m.cleaning = true
			m.spinnerFrame = 0
			steps := m.orderedSteps()
			if len(steps) == 0 {
				m.cleaning = false
				return m, nil
			}
			m.batch = batchResult{running: true, total: len(steps)}
			// Buffered so a slow repaint can never stall the worker
			// between steps; the UI drains it one message at a time.
			m.batchCh = make(chan batchProgressMsg, 64)
			m.statusMsg = "running batch..."
			return m, tea.Batch(spawnBatchCmd(steps, m.batchCh), waitForBatchCmd(m.batchCh), spinnerTickCmd())

		case modeConfirmClearHistory:
			m.mode = modeNormal
			return m, clearHistoryCmd()
		}
		return m, nil

	case "n", "esc", "ctrl+c", "q":
		m.mode = modeNormal
		m.confirmStep = batchStep{}
		return m, nil
	}
	return m, nil
}

// rescan reloads the current frame. Synthetic frames (the System Data
// aggregate, the curated Home list) can't just re-list a single directory,
// so each is rebuilt the same way it was first assembled — reusing the
// frame's existing id so the in-flight results still route back to it.
func (m Model) rescan() (Model, tea.Cmd) {
	f := m.currentFrame()
	if f == nil || f.loading {
		return m, nil
	}
	m.statusMsg = ""

	// The curated Home list is built from a fixed list of paths with no
	// directory listing behind it, so a rescan just re-resolves which of
	// them still exist.
	if m.activeTab == tabHome && len(m.navs[tabHome]) == 1 {
		m.navs[tabHome] = []navFrame{m.buildHomeFrame()}
		// Sizes are measured by the shared background scanner rather than
		// a command, so there is nothing for Bubble Tea to run here.
		for _, e := range m.navs[tabHome][0].entries {
			m.scanner.Enqueue(e.Path)
		}
		return m, nil
	}

	f.entries = nil
	f.selected = ""
	f.loadErr = ""
	f.loading = true
	f.pinned = true
	f.scores, f.orphan = nil, nil

	// The System Data landing frame merges several roots; everything else
	// — including any folder drilled into from it — is one real directory.
	if f.path == "" && m.activeTab == tabSystemData {
		roots := knowledge.SystemDataRoots()
		f.pending = len(roots)
		cmds := make([]tea.Cmd, 0, len(roots))
		for _, r := range roots {
			cmds = append(cmds, loadDirCmd(f.id, r.Path, r.Label, string(r.Root)))
		}
		return m, tea.Batch(cmds...)
	}

	if f.path == "" && m.activeTab == tabApplications {
		f.pending = 1
		return m, loadAppsCmd(f.id)
	}

	if m.activeTab == tabDocker {
		f.pending = 1
		return m, loadDockerCmd()
	}

	if m.activeTab == tabVendors && f.path == "" {
		f.pending = 1
		return m, loadVendorsCmd()
	}

	if ollamaRootParent() != "" && f.path == ollamaRootParent() {
		f.pending = 1
		return m, loadOllamaCmd(f.id)
	}

	f.pending = 1
	return m, loadDirCmd(f.id, f.path, f.source, string(f.root))
}

// startClean validates the current selection can be cleaned and, if so,
// enters the confirm-before-delete flow. Shared by the "d" key and a click
// on the CLEAN button.
func (m Model) startClean() (Model, tea.Cmd) {
	if m.cleaning {
		return m, nil
	}
	entry := m.selectedEntry()
	if entry == nil {
		return m, nil
	}
	k := m.knowledgeFor(entry)
	if !k.CanClean() {
		return m, nil
	}
	m.mode = modeConfirmClean
	m.confirmStep = batchStep{
		path:       entry.Path,
		name:       entry.Name,
		source:     m.activeTab.String(),
		action:     batchClean,
		cleanPaths: k.CleanPaths,
	}
	return m, nil
}

// startNativeClean is startClean's counterpart for the native-clean
// action. Shared by the "n" key and a click on the NATIVE CLEAN button.
func (m Model) startNativeClean() (Model, tea.Cmd) {
	if m.cleaning {
		return m, nil
	}
	entry := m.selectedEntry()
	if entry == nil {
		return m, nil
	}
	k := m.knowledgeFor(entry)
	if k.Native == nil || k.Protected {
		return m, nil
	}
	m.mode = modeConfirmNative
	m.confirmStep = batchStep{
		path:    entry.Path,
		name:    entry.Name,
		source:  m.activeTab.String(),
		action:  batchNative,
		command: k.Native.Command,
	}
	return m, nil
}

// startManualDelete enters the confirm flow for permanently deleting the
// whole selected folder. Unlike startClean/startNativeClean, this is never
// gated by the knowledge base — it's the always-available manual override,
// entirely at the user's own risk, for anything (including folders we
// know nothing about). Shared by the "D" key and a click on the MANUALLY
// DELETE button.
func (m Model) startManualDelete() (Model, tea.Cmd) {
	if m.cleaning {
		return m, nil
	}
	entry := m.selectedEntry()
	if entry == nil {
		return m, nil
	}
	if !m.knowledgeFor(entry).CanDelete() {
		return m, nil
	}
	// Docker objects have no file to delete; removing one means running
	// Docker's own command, which is the native action.
	if entry.Root == string(knowledge.RootDocker) {
		m.statusMsg = "remove Docker objects with the native command (n)"
		return m, nil
	}
	m.mode = modeConfirmManualDelete
	m.confirmStep = batchStep{
		path:   entry.Path,
		name:   entry.Name,
		source: m.activeTab.String(),
		action: batchDelete,
	}
	return m, nil
}

// toggleSelection adds or removes the current row from the batch set.
// Rows that offer no action at all — unresearched, protected, plain
// containers — simply can't be ticked, and say so.
func (m Model) toggleSelection() (Model, tea.Cmd) {
	if m.cleaning {
		return m, nil
	}
	e := m.selectedEntry()
	if e == nil {
		return m, nil
	}
	if _, ok := m.selected[e.Path]; ok {
		m.deselect(e.Path)
		return m, nil
	}
	step, ok := m.selectableStep(e)
	if !ok {
		m.statusMsg = "nothing to clean for " + e.Name
		return m, nil
	}
	step.source = m.activeTab.String()
	m.selected[e.Path] = step
	m.selOrder = append(m.selOrder, e.Path)

	// A granular clean's yield has to be measured before the button can
	// show a total, so kick that off as soon as the row is ticked rather
	// than waiting for the cursor to happen to rest on it.
	if !step.estimateReady {
		if k := m.knowledgeFor(e); len(k.CleanPaths) > 0 {
			if _, pending := m.reclaimCache[e.Path]; !pending {
				m.reclaimCache[e.Path] = reclaimInfo{}
				return m, reclaimSizeCmd(e.Path, k.CleanPaths)
			}
		}
	}
	return m, nil
}

// deselect drops one path from the batch set, keeping the order slice in
// step with the map.
func (m *Model) deselect(path string) {
	if _, ok := m.selected[path]; !ok {
		return
	}
	delete(m.selected, path)
	for i, p := range m.selOrder {
		if p == path {
			m.selOrder = append(m.selOrder[:i], m.selOrder[i+1:]...)
			break
		}
	}
}

// startBatch opens the confirm screen for the queued selection.
func (m Model) startBatch() (Model, tea.Cmd) {
	if m.cleaning || len(m.selOrder) == 0 {
		return m, nil
	}
	m.mode = modeConfirmBatch
	return m, nil
}

// selectAll ticks every actionable row in the current listing — the
// "delete all" affordance on the Leftovers tab, where the rows are
// folders belonging to apps that are no longer installed.
func (m Model) selectAll() (Model, tea.Cmd) {
	if m.cleaning {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, e := range m.currentEntries() {
		if _, already := m.selected[e.Path]; already {
			continue
		}
		step, ok := m.selectableStep(e)
		if !ok {
			continue
		}
		step.source = m.activeTab.String()
		m.selected[e.Path] = step
		m.selOrder = append(m.selOrder, e.Path)
		if !step.estimateReady {
			if k := m.knowledgeFor(e); len(k.CleanPaths) > 0 {
				if _, pending := m.reclaimCache[e.Path]; !pending {
					m.reclaimCache[e.Path] = reclaimInfo{}
					cmds = append(cmds, reclaimSizeCmd(e.Path, k.CleanPaths))
				}
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	// The wheel is the main reason this exists: compact Mac keyboards have
	// no PgUp/PgDn, so without it the description panel could only be
	// scrolled by a key the user does not have. Over the table it moves the
	// cursor; over anything else it scrolls the description.
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		if !m.activeTab.browsable() && m.activeTab != tabResults {
			return m, nil
		}
		up := msg.Button == tea.MouseButtonWheelUp
		overTable := m.activeTab.browsable() && msg.Y > tableHeaderY && msg.Y < m.detailToolbarY()-1
		switch {
		case overTable && up:
			m.moveSelection(-1)
		case overTable:
			m.moveSelection(1)
		case up:
			return m.applyNavAction(navScrollUp)
		default:
			return m.applyNavAction(navScrollDown)
		}
		return m, nil
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	if msg.Y < tabBarHeight {
		for _, r := range m.tabRegionsFor() {
			if msg.X >= r.x0 && msg.X <= r.x1 {
				m.activeTab = r.tab
				m.detailScroll = 0
				return m, nil
			}
		}
		return m, nil
	}

	if !m.activeTab.browsable() || m.mode != modeNormal || m.cleaning {
		return m, nil
	}

	// Navigation controls sit on the strip between the tab bar and table.
	if msg.Y == tabBarHeight {
		for _, r := range m.navRegions() {
			if msg.X >= r.x0 && msg.X <= r.x1 {
				return m.applyNavAction(r.action)
			}
		}
		return m, nil
	}

	if msg.Y == m.detailToolbarY() {
		for _, r := range m.detailScrollRegions() {
			if msg.X >= r.x0 && msg.X <= r.x1 {
				return m.applyNavAction(r.action)
			}
		}
		return m, nil
	}

	// Clicking a table row selects it.
	if msg.Y > tableHeaderY && msg.Y < m.detailToolbarY()-1 {
		if idx := m.rowIndexAt(msg.Y); idx >= 0 {
			m.jumpSelection(idx)
		}
		return m, nil
	}

	if msg.Y == tableHeaderY {
		for _, r := range m.headerRegions() {
			if msg.X >= r.x0 && msg.X <= r.x1 {
				return m.clickSort(r.col), nil
			}
		}
		return m, nil
	}

	rowY := m.buttonRowY()
	if msg.Y >= rowY && msg.Y < rowY+cleanButtonHeight {
		for _, r := range m.buttonRegions(m.width) {
			if msg.X >= r.x0 && msg.X <= r.x1 {
				switch r.action {
				case actionClean:
					return m.startClean()
				case actionNativeClean:
					return m.startNativeClean()
				case actionManualDelete:
					return m.startManualDelete()
				case actionRunBatch:
					return m.startBatch()
				case actionSelectAll:
					return m.selectAll()
				case actionClearHistory:
					m.mode = modeConfirmClearHistory
					return m, nil
				}
				return m, nil
			}
		}
	}

	return m, nil
}

// clickSort makes col the active sort column, toggling direction if it's
// already active. NAME defaults to ascending (A→Z first); the size/mod/
// safety columns default to descending (biggest/newest/safest first) —
// whichever reads naturally on a first click. Every currently-loaded
// frame, on every tab, is re-sorted immediately so navigating back into
// one respects the new order without waiting on a fresh scan message.
func (m Model) clickSort(col sortColumn) Model {
	asc := col == sortByName
	if m.sortCol == col {
		asc = !m.sortAsc
	}
	return m.clickSortTo(col, asc)
}

// clickSortTo applies an explicit column and direction, without the
// toggling clickSort does — used by the "reset sort" control, which has a
// specific order in mind rather than "the other way round from now".
func (m Model) clickSortTo(col sortColumn, asc bool) Model {
	m.sortCol = col
	m.sortAsc = asc
	for ti := range m.navs {
		for fi := range m.navs[ti] {
			// Re-pin: asking to sort by a column means wanting to see what
			// is at the top of it, not to follow the old selection to
			// wherever the new order sent it.
			m.navs[ti][fi].pinned = true
			m.sortFrame(&m.navs[ti][fi])
		}
	}
	return m
}

// openSelected drills into the selected directory, pushing a new nav
// frame and kicking off a listing for it. Non-directory entries (stray
// files, and the handful of plain files on the Home tab) have nothing to
// open.
//
// The child inherits the parent row's own root and TYPE label rather than
// the frame's, which is what makes drilling in from the merged System Data
// listing work: a row from Application Support keeps answering to the
// Application Support dictionary once opened.
func (m Model) openSelected() (Model, tea.Cmd) {
	e := m.selectedEntry()
	if e == nil || !e.IsDir {
		return m, nil
	}
	// Opening ~/.ollama lists installed models instead of the directory
	// tree underneath it, which is otherwise a dead end: models/blobs is
	// nothing but sha256-<64 hex> files with no indication of which model
	// any of them belongs to.
	if ollamaRootParent() != "" && e.Path == ollamaRootParent() {
		f := navFrame{
			id:      m.newFrameID(),
			label:   e.Name,
			path:    e.Path,
			root:    knowledge.RootHome,
			source:  "model",
			pinned:  true,
			loading: true,
			pending: 1,
		}
		m.navs[m.activeTab] = append(m.navs[m.activeTab], f)
		m.detailScroll = 0
		return m, loadOllamaCmd(f.id)
	}

	root := knowledge.Root(e.Root)
	f := navFrame{
		id:      m.newFrameID(),
		label:   e.Name,
		path:    e.Path,
		root:    root,
		source:  e.Source,
		pinned:  true,
		loading: true,
		pending: 1,
	}
	m.navs[m.activeTab] = append(m.navs[m.activeTab], f)
	m.detailScroll = 0
	return m, loadDirCmd(f.id, e.Path, e.Source, e.Root)
}

// goUp pops back to the parent directory, if any.
func (m Model) goUp() Model {
	if nav := m.navs[m.activeTab]; len(nav) > 1 {
		m.navs[m.activeTab] = nav[:len(nav)-1]
		m.detailScroll = 0
	}
	return m
}

func (m *Model) moveSelection(delta int) {
	f := m.currentFrame()
	if f == nil {
		return
	}
	idx := m.selectedIndex()
	if idx < 0 {
		if len(f.entries) > 0 {
			f.selected = f.entries[0].Path
		}
		return
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(f.entries) {
		idx = len(f.entries) - 1
	}
	if idx >= 0 {
		f.selected = f.entries[idx].Path
	}
	// A deliberate move takes over from the auto-follow-the-top behaviour
	// that keeps the cursor on the biggest row while sizes stream in.
	f.pinned = false
	m.detailScroll = 0
}
