package ui

import (
	tea "github.com/charmbracelet/bubbletea"
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
		return m, nil

	case "shift+tab", "left", "h":
		m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
		return m, nil

	case "1":
		m.activeTab = tabSystemData
		return m, nil
	case "2":
		m.activeTab = tabDocker
		return m, nil
	case "3":
		m.activeTab = tabFolders
		return m, nil
	case "4":
		m.activeTab = tabApplications
		return m, nil

	case "up", "k":
		if m.activeTab == tabSystemData {
			m.moveSelection(-1)
		}
		return m, nil

	case "down", "j":
		if m.activeTab == tabSystemData {
			m.moveSelection(1)
		}
		return m, nil

	case "pgup", "K":
		if m.activeTab == tabSystemData {
			m.detailScroll -= detailScrollStep
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
		}
		return m, nil

	case "pgdown", "J":
		if m.activeTab == tabSystemData {
			m.detailScroll += detailScrollStep
		}
		return m, nil

	case "enter":
		if m.activeTab != tabSystemData {
			return m, nil
		}
		return m.openSelected()

	case "esc", "backspace":
		if m.activeTab != tabSystemData {
			return m, nil
		}
		return m.goUp(), nil

	case "r":
		if m.activeTab != tabSystemData {
			return m, nil
		}
		f := m.currentFrame()
		if f == nil || f.loading {
			return m, nil
		}
		f.loading = true
		m.statusMsg = ""
		return m, loadDirCmd(f.path, f.source)

	case "d", "delete":
		if m.activeTab != tabSystemData {
			return m, nil
		}
		return m.startClean()

	case "n":
		if m.activeTab != tabSystemData {
			return m, nil
		}
		return m.startNativeClean()

	case "D":
		if m.activeTab != tabSystemData {
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
				return m, nil
			}
		}
		return m, nil
	}

	if m.activeTab != tabSystemData || m.mode != modeNormal || m.cleaning {
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
// directory is re-sorted immediately so navigating back into one respects
// the new order without waiting on a fresh scan message.
func (m Model) clickSort(col sortColumn) Model {
	if m.sortCol == col {
		m.sortAsc = !m.sortAsc
	} else {
		m.sortCol = col
		m.sortAsc = col == sortByName
	}
	for fi := range m.nav {
		sortEntries(m.nav[fi].entries, m.sortCol, m.sortAsc)
	}
	return m
}

// openSelected drills into the selected directory, pushing a new nav
// frame and kicking off a listing for it. Non-directory entries (stray
// files directly under Caches) have nothing to open.
func (m Model) openSelected() (Model, tea.Cmd) {
	e := m.selectedEntry()
	if e == nil || !e.IsDir {
		return m, nil
	}
	f := m.currentFrame()
	source := "Cache"
	if f != nil {
		source = f.source
	}
	m.nav = append(m.nav, navFrame{
		label:   e.Name,
		path:    e.Path,
		source:  source,
		loading: true,
	})
	m.detailScroll = 0
	return m, loadDirCmd(e.Path, source)
}

// goUp pops back to the parent directory, if any.
func (m Model) goUp() Model {
	if len(m.nav) > 1 {
		m.nav = m.nav[:len(m.nav)-1]
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
