package ui

// The navigation strip under the tab bar, and the scroll controls inside
// the detail panel.
//
// Both exist because the keyboard shortcuts they duplicate are not
// universally available: compact Mac keyboards have no PgUp/PgDn, Home or
// End at all, which left "scroll the description" and "jump to the end of
// a thousand-row table" reachable only by holding an arrow key. Every
// control here is clickable, and each still names its key binding for
// people who do have one.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type navAction int

const (
	navNone navAction = iota
	navTop
	navBottom
	navResetSort
	navToggleSelected
	navScrollUp
	navScrollDown
)

// navRegion is a mouse hit-test rectangle for one clickable control.
type navRegion struct {
	action navAction
	x0, x1 int
}

type navButton struct {
	action navAction
	label  string
	active bool
}

var (
	navButtonStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted))
	navButtonActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cAccent))
)

const navButtonGap = 1

// navButtons are the controls on the strip under the tab bar, in render
// order (left to right).
func (m Model) navButtons() []navButton {
	btns := []navButton{
		{action: navTop, label: " ⇱ TOP (g) "},
		{action: navBottom, label: " ⇲ END (G) "},
		{action: navResetSort, label: " ↺ SORT (s) "},
	}
	if n := len(m.selOrder); n > 0 {
		label := " ☐ ONLY SELECTED " + itoa(n) + " (f) "
		if m.onlySelected {
			label = " ☑ ONLY SELECTED " + itoa(n) + " (f) "
		}
		btns = append(btns, navButton{action: navToggleSelected, label: label, active: m.onlySelected})
	}
	return btns
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// navBarWidth is the total width the controls occupy.
func (m Model) navBarWidth() int {
	btns := m.navButtons()
	w := 0
	for i, b := range btns {
		if i > 0 {
			w += navButtonGap
		}
		w += displayWidth(b.label)
	}
	return w
}

// navRegions computes click targets, right-aligned to match the render.
func (m Model) navRegions() []navRegion {
	btns := m.navButtons()
	x := m.width - m.navBarWidth()
	if x < 0 {
		x = 0
	}
	regions := make([]navRegion, 0, len(btns))
	for i, b := range btns {
		if i > 0 {
			x += navButtonGap
		}
		w := displayWidth(b.label)
		regions = append(regions, navRegion{action: b.action, x0: x, x1: x + w - 1})
		x += w
	}
	return regions
}

// renderNavBar draws the breadcrumb on the left and the controls on the
// right, on a single line so the table keeps its height.
func (m Model) renderNavBar(width int) string {
	btns := m.navButtons()
	controlsW := m.navBarWidth()

	text := m.breadcrumb()
	if len(m.navs[m.activeTab]) > 1 {
		text += "   (Esc/⌫ to go up)"
	}
	left := truncate(text, width-controlsW-2)

	var b strings.Builder
	b.WriteString(dimStyle.Render(left))
	pad := width - controlsW - displayWidth(left)
	if pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	for i, btn := range btns {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", navButtonGap))
		}
		style := navButtonStyle
		if btn.active {
			style = navButtonActiveStyle
		}
		b.WriteString(style.Render(btn.label))
	}
	return b.String()
}

// --- detail panel scroll controls ----------------------------------------

const (
	detailScrollUpLabel   = " ▲ "
	detailScrollDownLabel = " ▼ "
)

// detailToolbarY is the screen row the detail panel's scroll controls
// render on: the first content line inside its box.
func (m Model) detailToolbarY() int {
	tableH, _ := m.contentLayout()
	return tabBarHeight + navBarHeight + tableH + 1
}

// detailScrollRegions computes click targets for the ▲/▼ controls,
// right-aligned inside the detail box.
func (m Model) detailScrollRegions() []navRegion {
	up := displayWidth(detailScrollUpLabel)
	down := displayWidth(detailScrollDownLabel)
	// width-2 is the box's inner right edge; one more column of margin
	// keeps the controls off the border.
	x1 := m.width - 3
	downX0 := x1 - down + 1
	upX0 := downX0 - navButtonGap - up
	return []navRegion{
		{action: navScrollUp, x0: upX0, x1: upX0 + up - 1},
		{action: navScrollDown, x0: downX0, x1: x1},
	}
}

// renderDetailToolbar is the right-aligned scroll control line placed at
// the top of the detail panel. more reports whether there is anything
// below to scroll to, so the controls can be dimmed when they'd do
// nothing.
func (m Model) renderDetailToolbar(innerW int, canUp, canDown bool) string {
	up := navButtonStyle.Render(detailScrollUpLabel)
	if canUp {
		up = accentStyle.Render(detailScrollUpLabel)
	}
	down := navButtonStyle.Render(detailScrollDownLabel)
	if canDown {
		down = accentStyle.Render(detailScrollDownLabel)
	}

	hint := "scroll: wheel, PgUp/PgDn, or click →"
	controls := up + strings.Repeat(" ", navButtonGap) + down
	pad := innerW - displayWidth(hint) - displayWidth(detailScrollUpLabel) -
		displayWidth(detailScrollDownLabel) - navButtonGap - 1
	if pad < 1 {
		return faintStyle.Render(strings.Repeat(" ", maxInt(innerW-6, 0))) + controls
	}
	return faintStyle.Render(hint) + strings.Repeat(" ", pad) + controls
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// applyNavAction runs one control, from either a click or its key.
func (m Model) applyNavAction(a navAction) (Model, tea.Cmd) {
	switch a {
	case navTop:
		m.jumpSelection(0)
	case navBottom:
		m.jumpSelection(len(m.currentEntries()) - 1)
	case navResetSort:
		// Back to the built-in composite order: biggest first, safest
		// first among equals — which is where you want to start reading.
		return m.clickSortTo(sortDefault, false), nil
	case navToggleSelected:
		m.onlySelected = !m.onlySelected
		m.detailScroll = 0
		// The cursor may have been on a row the filter just hid.
		m.reconcileSelection()
	case navScrollUp:
		m.detailScroll -= detailScrollStep
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case navScrollDown:
		m.detailScroll += detailScrollStep
	}
	return m, nil
}

// jumpSelection moves the cursor to an absolute row index.
func (m *Model) jumpSelection(idx int) {
	entries := m.currentEntries()
	if len(entries) == 0 {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(entries) {
		idx = len(entries) - 1
	}
	f := m.currentFrame()
	if f == nil {
		return
	}
	f.selected = entries[idx].Path
	f.pinned = false
	m.detailScroll = 0
}

// reconcileSelection puts the cursor back on a visible row after a filter
// change, so it can never point at something that isn't on screen.
func (m *Model) reconcileSelection() {
	f := m.currentFrame()
	if f == nil {
		return
	}
	entries := m.currentEntries()
	if len(entries) == 0 {
		f.selected = ""
		return
	}
	for _, e := range entries {
		if e.Path == f.selected {
			return
		}
	}
	f.selected = entries[0].Path
}
