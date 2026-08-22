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

	case "1":
		m.activeTab = tabSystemData
		m.detailScroll = 0
		return m, nil
	case "2":
		m.activeTab = tabDocker
		m.detailScroll = 0
		return m, nil
	case "3":
		m.activeTab = tabHome
		m.detailScroll = 0
		return m, nil
	case "4":
		m.activeTab = tabApplications
		m.detailScroll = 0
		return m, nil

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
		case modeConfirmClean:
			m.mode = modeNormal
			m.cleaning = true
			m.spinnerFrame = 0
			path := m.confirmPath
			patterns := m.confirmCleanPaths
			m.confirmPath = ""
			m.confirmCleanPaths = nil
			m.statusMsg = "cleaning..."
			return m, tea.Batch(cleanCmd(path, patterns), spinnerTickCmd())
		case modeConfirmNative:
			m.mode = modeNormal
			m.cleaning = true
			m.spinnerFrame = 0
			path := m.confirmPath
			command := m.confirmNativeCmd
			m.confirmPath = ""
			m.confirmNativeCmd = ""
			m.confirmNativeLabel = ""
			m.statusMsg = "running native clean..."
			return m, tea.Batch(nativeCleanCmd(path, command), spinnerTickCmd())
		case modeConfirmManualDelete:
			m.mode = modeNormal
			m.cleaning = true
			m.spinnerFrame = 0
			path := m.confirmPath
			m.confirmPath = ""
			m.statusMsg = "deleting..."
			return m, tea.Batch(manualDeleteCmd(path), spinnerTickCmd())
		}
		return m, nil
	case "n", "esc", "ctrl+c", "q":
		m.mode = modeNormal
		m.confirmPath = ""
		m.confirmCleanPaths = nil
		m.confirmNativeCmd = ""
		m.confirmNativeLabel = ""
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
	m.confirmPath = entry.Path
	m.confirmCleanPaths = k.CleanPaths
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
	if k.Native == nil {
		return m, nil
	}
	m.mode = modeConfirmNative
	m.confirmPath = entry.Path
	m.confirmNativeCmd = k.Native.Command
	m.confirmNativeLabel = entry.Name
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
	m.mode = modeConfirmManualDelete
	m.confirmPath = entry.Path
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
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
	if m.sortCol == col {
		m.sortAsc = !m.sortAsc
	} else {
		m.sortCol = col
		m.sortAsc = col == sortByName
	}
	for ti := range m.navs {
		for fi := range m.navs[ti] {
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
	root := knowledge.Root(e.Root)
	f := navFrame{
		id:      m.newFrameID(),
		label:   e.Name,
		path:    e.Path,
		root:    root,
		source:  e.Source,
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
	m.detailScroll = 0
}
