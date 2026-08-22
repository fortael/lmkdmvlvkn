package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/scan"
)

// Palette, mirrors the 256-color scheme used elsewhere in similar TUIs.
const (
	cAccent = "39" // cyan-blue
	cInk    = "0"
	cMuted  = "244"
	cFaint  = "240"
	cText   = "255"
	cGreen  = "42"
	cYellow = "220"
	cRed    = "203"
)

var (
	appStyle         = lipgloss.NewStyle()
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cAccent)).Padding(1, 3)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted)).Padding(1, 3)

	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cFaint))

	headerRowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted)).Bold(true)
	selectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cAccent))
	normalRowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(cText))

	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted))
	faintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(cFaint))
	boldStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cText))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cAccent))

	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(cGreen))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cYellow))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(cRed))

	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(cFaint))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cYellow))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(cRed))

	enabledButtonStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(cInk)).
				Background(lipgloss.Color(cGreen)).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(cGreen)).
				BorderBackground(lipgloss.Color(cGreen)).
				Padding(0, 3)

	nativeButtonStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(cInk)).
				Background(lipgloss.Color(cYellow)).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(cYellow)).
				BorderBackground(lipgloss.Color(cYellow)).
				Padding(0, 3)

	disabledButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(cFaint)).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(cFaint)).
				Padding(0, 3)

	// dangerButtonStyle is deliberately an outline, not a filled block
	// like enabled/nativeButtonStyle — it's always present next to every
	// entry (safe or not), so it shouldn't compete for attention the way
	// the actual recommended action does.
	dangerButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(cRed)).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color(cRed)).
				Padding(0, 3)

	headerActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cAccent)).Bold(true)
	nativeRowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(cYellow))
	unknownRowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(cFaint))
)

const (
	colMarker = 2
	colType   = 7
	colSize   = 9
	colBar    = 18
	colMod    = 5
	colScore  = 5
	colNative = 4
	colGaps   = 7 // one space between each of the 8 columns
)

const (
	tabBarHeight      = 3 // tab boxes: 1 line of padding + 1 line text + 1 line padding
	breadcrumbHeight  = 1
	cleanButtonHeight = 3 // border(2) + one text line, no vertical padding — kept small on purpose
	detailScrollStep  = 3

	// tableHeaderY is the fixed screen row the sortable column headers
	// render on: past the tab bar, the breadcrumb line, and the table
	// box's top border. Unlike the button row this never depends on
	// terminal height, so it can be a plain constant.
	tableHeaderY = tabBarHeight + breadcrumbHeight + 1

	// wideBreakpoint is roughly 1000px of terminal width, assuming ~8px
	// per monospace cell — above this the detail panel splits into two
	// columns (meta+commands on the left, description on the right)
	// instead of stacking everything vertically.
	wideBreakpoint = 120
)

// contentLayout computes the table/detail panel heights for the current
// terminal size. It's the single source of truth for that split so
// rendering and mouse hit-testing (which needs to know where the button
// row starts) can never disagree. Every browsable tab shares the layout.
func (m Model) contentLayout() (tableH, detailH int) {
	contentH := m.height - tabBarHeight - breadcrumbHeight - cleanButtonHeight - 1
	if contentH < 6 {
		contentH = 6
	}
	tableH = contentH * 3 / 5
	if tableH < 8 {
		tableH = 8
	}
	detailH = contentH - tableH
	if detailH < 6 {
		detailH = 6
	}
	return tableH, detailH
}

// buttonRowY is the screen row the clean/native-clean buttons start on.
func (m Model) buttonRowY() int {
	tableH, detailH := m.contentLayout()
	return tabBarHeight + breadcrumbHeight + tableH + detailH
}

// tableNameWidth is the NAME column's width for the current terminal size
// — shared by rendering and header click hit-testing so they can never
// drift apart.
func (m Model) tableNameWidth() int {
	innerW := m.width - 2
	if innerW < 20 {
		innerW = 20
	}
	nameW := innerW - colMarker - colType - colSize - colBar - colMod - colScore - colNative - colGaps
	if nameW < 6 {
		nameW = 6
	}
	return nameW
}

func (m Model) render() string {
	if !m.ready || m.width == 0 {
		return "loading…"
	}

	var b strings.Builder
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	if !m.activeTab.browsable() {
		contentH := m.height - tabBarHeight - 1
		if contentH < 6 {
			contentH = 6
		}
		b.WriteString(m.renderPlaceholder(m.width, contentH))
		b.WriteString("\n")
		b.WriteString(m.renderHelp())
		return appStyle.Render(b.String())
	}

	// tabs + breadcrumb + button + help are fixed-height; the rest splits
	// between the table and the detail panel. Both hard-clip/scroll their
	// content to the height they're given so nothing can ever push the
	// button or help line off screen.
	tableH, detailH := m.contentLayout()

	b.WriteString(m.renderBreadcrumb(m.width))
	b.WriteString("\n")
	b.WriteString(m.renderTable(m.width, tableH))
	b.WriteString("\n")
	b.WriteString(m.renderDetail(m.width, detailH))
	b.WriteString("\n")
	b.WriteString(m.renderCleanButton(m.width))
	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	return appStyle.Render(b.String())
}

// tabLabel returns the plain (unstyled) label for t, including a size
// summary once at least some sizes are known ("+" while that tab's scan is
// still incomplete).
func (m Model) tabLabel(t tab) string {
	label := t.String()
	if !t.browsable() {
		return label
	}
	if total, complete := m.tabTotalSize(t); total > 0 {
		suffix := "+"
		if complete {
			suffix = ""
		}
		label = fmt.Sprintf("%s (%s%s)", label, formatSize(total), suffix)
	}
	return label
}

// tabRegionsFor computes mouse hit-test rectangles for the tab bar. It only
// depends on the same plain labels renderTabs uses, so hit-testing always
// matches what's on screen without needing to persist state from render
// into the model.
func (m Model) tabRegionsFor() []tabRegion {
	regions := make([]tabRegion, 0, int(tabCount))
	x := 0
	for t := tab(0); t < tabCount; t++ {
		w := displayWidth(m.tabLabel(t)) + 6 // Padding(1, 3) => 3 cols each side
		regions = append(regions, tabRegion{tab: t, x0: x, x1: x + w - 1})
		x += w + 1 // 1-column gap between boxes
	}
	return regions
}

func (m Model) renderTabs() string {
	boxes := make([]string, 0, int(tabCount)*2)
	spacer := lipgloss.NewStyle().Height(tabBarHeight).Render(" ")
	for t := tab(0); t < tabCount; t++ {
		style := tabInactiveStyle
		if t == m.activeTab {
			style = tabActiveStyle
		}
		if len(boxes) > 0 {
			boxes = append(boxes, spacer)
		}
		boxes = append(boxes, style.Render(m.tabLabel(t)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}

func (m Model) renderBreadcrumb(width int) string {
	text := m.breadcrumb()
	if len(m.navs[m.activeTab]) > 1 {
		text += "   (Esc/Backspace to go up)"
	}
	return dimStyle.Render(truncate(text, width))
}

func (m Model) renderPlaceholder(width, height int) string {
	box := boxStyle.Width(width - 2).Height(height - 2)
	msg := fmt.Sprintf("%s — coming soon", m.activeTab.String())
	return box.Render(lipgloss.Place(width-4, height-2, lipgloss.Center, lipgloss.Center, dimStyle.Render(msg)))
}

func (m Model) renderTable(width, height int) string {
	innerW := width - 2
	innerH := height - 2
	if innerW < 20 {
		innerW = 20
	}
	nameW := m.tableNameWidth()

	header := m.renderTableHeader(nameW)

	entries := m.currentEntries()
	f := m.currentFrame()

	var body string
	if f != nil && f.loadErr != "" {
		body = errorStyle.Render(f.loadErr)
	} else if f != nil && f.loading && len(entries) == 0 {
		body = dimStyle.Render("scanning…")
	} else if len(entries) == 0 {
		body = dimStyle.Render("empty directory")
	} else {
		visibleRows := innerH - 1
		if visibleRows < 1 {
			visibleRows = 1
		}
		idx := m.selectedIndex()
		offset := 0
		if idx >= 0 {
			offset = idx - visibleRows/2
		}
		maxOffset := len(entries) - visibleRows
		if maxOffset < 0 {
			maxOffset = 0
		}
		if offset > maxOffset {
			offset = maxOffset
		}
		if offset < 0 {
			offset = 0
		}

		max := maxSize(entries)
		end := offset + visibleRows
		if end > len(entries) {
			end = len(entries)
		}

		lines := make([]string, 0, visibleRows)
		for i := offset; i < end; i++ {
			lines = append(lines, m.renderRow(entries[i], i == idx, nameW, max, innerW))
		}
		body = strings.Join(lines, "\n")
	}

	content := header + "\n" + body
	return boxStyle.Width(innerW).Height(innerH).Render(content)
}

// sortArrow returns a 1-column indicator appended to a header label when
// its column is the active sort, chosen so it always fits within that
// column's existing fixed width (no header text needs to grow to make
// room for it).
func sortArrow(active, asc bool) string {
	if !active {
		return ""
	}
	if asc {
		return "▴"
	}
	return "▾"
}

// renderTableHeader builds the column header row with per-column styling,
// so the active sort column can be highlighted without disturbing the
// others — the same "pad plain text, then style each segment, then
// concatenate" approach renderRow uses, for the same ANSI-width reasons.
func (m Model) renderTableHeader(nameW int) string {
	style := func(col sortColumn) lipgloss.Style {
		if m.sortCol == col {
			return headerActiveStyle
		}
		return headerRowStyle
	}
	active := func(col sortColumn) bool { return m.sortCol == col }

	marker := headerRowStyle.Render(padRight(strings.Repeat(" ", colMarker), colMarker))
	typeSeg := headerRowStyle.Render(padRight("TYPE", colType))
	nameSeg := style(sortByName).Render(padRight("NAME"+sortArrow(active(sortByName), m.sortAsc), nameW))
	sizeSeg := style(sortBySize).Render(padLeft("SIZE"+sortArrow(active(sortBySize) || m.sortCol == sortDefault, m.sortAsc), colSize))
	barSeg := headerRowStyle.Render(padRight("RELATIVE SIZE", colBar))
	modSeg := style(sortByMod).Render(padRight("MOD"+sortArrow(active(sortByMod), m.sortAsc), colMod))
	safeSeg := style(sortBySafe).Render(padCenter("SAFE"+sortArrow(active(sortBySafe), m.sortAsc), colScore))
	natSeg := headerRowStyle.Render(padCenter("NAT", colNative))
	gap := headerRowStyle.Render(" ")

	return marker + gap + typeSeg + gap + nameSeg + gap + sizeSeg + gap + barSeg + gap + modSeg + gap + safeSeg + gap + natSeg
}

// headerRegion is a mouse hit-test rectangle for a clickable, sortable
// column header.
type headerRegion struct {
	col    sortColumn
	x0, x1 int
}

// headerRegions computes click regions for the header row using the exact
// same column-width arithmetic as renderTableHeader/renderRow, so a click
// always lands on the column it visually appears to.
func (m Model) headerRegions() []headerRegion {
	nameW := m.tableNameWidth()
	x := 1 // column 0 is the table box's left border
	x += colMarker + 1
	x += colType + 1
	nameX0 := x
	x += nameW + 1
	sizeX0 := x
	x += colSize + 1
	x += colBar + 1 // relative-size bar isn't a sortable column
	modX0 := x
	x += colMod + 1
	safeX0 := x
	x += colScore + 1

	return []headerRegion{
		{col: sortByName, x0: nameX0, x1: nameX0 + nameW - 1},
		{col: sortBySize, x0: sizeX0, x1: sizeX0 + colSize - 1},
		{col: sortByMod, x0: modX0, x1: modX0 + colMod - 1},
		{col: sortBySafe, x0: safeX0, x1: safeX0 + colScore - 1},
	}
}

// renderRow builds each column as its own fixed-width, already-styled
// segment and concatenates them directly. Widths are correct by
// construction (every segment is padded to its declared column width
// *before* it is colored), so nothing downstream ever needs to re-measure
// an ANSI-styled string — which is unreliable since escape codes throw off
// naive rune-width counting.
func (m Model) renderRow(e *scan.Entry, selected bool, nameW int, max int64, innerW int) string {
	k := m.knowledgeFor(e)

	rowStyle := normalRowStyle
	switch {
	case selected:
		rowStyle = selectedRowStyle
	case k.Native != nil:
		rowStyle = nativeRowStyle
	case k.Score == knowledge.Unknown:
		rowStyle = unknownRowStyle
	}
	gap := rowStyle.Render(" ")

	marker := "  "
	if selected {
		marker = "▸ "
	}
	markerSeg := rowStyle.Render(padRight(marker, colMarker))
	typeSeg := rowStyle.Render(padRight(truncate(e.Source, colType), colType))

	icon := "📄 "
	if e.IsDir {
		icon = "📁 "
	}
	name := icon + e.Name
	switch {
	case k.Container:
		name += "  (versions ›)"
	case e.IsDir:
		name += " ›"
	}
	if k.Orphan {
		name += "  ⚠ leftover"
	}
	nameSeg := rowStyle.Render(padRight(truncate(name, nameW), nameW))

	sizeStr := "…"
	if e.SizeReady {
		sizeStr = formatSize(e.Size)
	}
	sizeSeg := rowStyle.Render(padLeft(sizeStr, colSize))

	barSeg := renderBarSegment(e, max, colBar, selected)
	modSeg := rowStyle.Render(padRight(timeAgo(e.ModTime), colMod))
	starsSeg := renderStarsSegment(k.Score, colScore, selected)

	natMark := " "
	if k.Native != nil {
		natMark = "✓"
	}
	natSeg := rowStyle.Render(padCenter(natMark, colNative))

	return markerSeg + gap + typeSeg + gap + nameSeg + gap + sizeSeg + gap + barSeg + gap + modSeg + gap + starsSeg + gap + natSeg
}

func renderBarSegment(e *scan.Entry, max int64, width int, selected bool) string {
	filled := 0
	if e.SizeReady && max > 0 {
		filled = int(float64(width) * float64(e.Size) / float64(max))
		if filled > width {
			filled = width
		}
		if e.Size > 0 && filled == 0 {
			filled = 1
		}
	}
	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", width-filled)

	if selected {
		emptyOnAccent := lipgloss.NewStyle().Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cAccent))
		return selectedRowStyle.Render(filledStr) + emptyOnAccent.Render(emptyStr)
	}
	return accentStyle.Render(filledStr) + faintStyle.Render(emptyStr)
}

// starsGlyph returns the plain (unstyled) 3-rune glyph for a score plus the
// style that colors it.
func starsGlyph(score knowledge.Score) (string, lipgloss.Style) {
	switch score {
	case knowledge.Safe:
		return "★★★", greenStyle
	case knowledge.Caution:
		return "★★☆", yellowStyle
	case knowledge.Risky:
		return "★☆☆", redStyle
	default:
		return " ? ", faintStyle
	}
}

func renderStarsSegment(score knowledge.Score, width int, selected bool) string {
	glyph, style := starsGlyph(score)
	padded := padCenter(glyph, width)
	if selected {
		return selectedRowStyle.Render(padded)
	}
	return style.Render(padded)
}

// renderStars is the plain, non-tabular rendering used in the detail panel.
func renderStars(score knowledge.Score) string {
	glyph, style := starsGlyph(score)
	return style.Render(glyph)
}

func (m Model) renderDetail(width, height int) string {
	innerW := width - 4
	innerH := height - 2
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 1 {
		innerH = 1
	}

	if m.mode != modeNormal {
		return m.renderConfirm(width, height)
	}

	f := m.currentFrame()
	e := m.selectedEntry()
	if e == nil {
		msg := "no item selected"
		switch {
		case f != nil && f.loading:
			msg = "scanning…"
		case f != nil && len(f.entries) == 0:
			msg = "empty directory"
		}
		return boxStyle.Width(width - 2).Height(innerH).Render(dimStyle.Render(msg))
	}

	k := m.knowledgeFor(e)

	if width >= wideBreakpoint {
		return m.renderDetailWide(e, k, width, innerW, innerH)
	}
	return m.renderDetailNarrow(e, k, width, innerW, innerH)
}

func (m Model) renderDetailNarrow(e *scan.Entry, k knowledge.Entry, width, innerW, innerH int) string {
	lines := m.buildMetaLines(e, k, innerW)
	lines = append(lines, "")
	lines = append(lines, buildDescLines(k, innerW)...)

	window := scrollWindow(lines, m.detailScroll, innerH)
	content := strings.Join(window, "\n")
	return boxStyle.Width(width - 2).Height(innerH).Render(content)
}

func (m Model) renderDetailWide(e *scan.Entry, k knowledge.Entry, width, innerW, innerH int) string {
	const sep = " │ "
	leftW := innerW * 2 / 5
	if leftW < 20 {
		leftW = 20
	}
	rightW := innerW - leftW - displayWidth(sep)
	if rightW < 10 {
		rightW = 10
	}

	leftWin := scrollWindow(m.buildMetaLines(e, k, leftW), m.detailScroll, innerH)
	rightWin := scrollWindow(buildDescLines(k, rightW), m.detailScroll, innerH)

	var b strings.Builder
	for i := 0; i < innerH; i++ {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ansiPadRight(leftWin[i], leftW))
		b.WriteString(faintStyle.Render(sep))
		b.WriteString(rightWin[i])
	}
	return boxStyle.Width(width - 2).Height(innerH).Render(b.String())
}

// sizeSummaryLine renders the "Size: X" meta line, upgraded to
// "Size: X → Y (-Z%)" once we know (or can trivially derive) how much a
// clean action would free: instantly for a whole-folder clean (frees
// ~everything), or from the background-computed reclaimCache for a
// granular (CleanPaths) clean — showing a "calculating…" hint while that's
// still in flight rather than nothing at all.
func (m Model) sizeSummaryLine(e *scan.Entry, k knowledge.Entry, w int) string {
	sizeStr := "…"
	if e.SizeReady {
		sizeStr = formatSize(e.Size)
	}
	base := fmt.Sprintf("Size: %s", sizeStr)
	if !e.SizeReady || !k.CanClean() {
		return truncate(base, w)
	}

	if len(k.CleanPaths) == 0 {
		return truncate(fmt.Sprintf("Size: %s → ~0 B (-100%%)", sizeStr), w)
	}

	info, ok := m.reclaimCache[e.Path]
	if !ok || !info.ready {
		return truncate(base+"  (calculating potential savings…)", w)
	}
	freed := info.total
	if freed > e.Size {
		freed = e.Size
	}
	projected := e.Size - freed
	pct := 0
	if e.Size > 0 {
		pct = int(float64(freed) / float64(e.Size) * 100)
	}
	return truncate(fmt.Sprintf("Size: %s → %s (-%d%%)", sizeStr, formatSize(projected), pct), w)
}

// buildMetaLines renders identity/metadata/commands — the "what is this
// and what would run" half of the detail panel.
func (m Model) buildMetaLines(e *scan.Entry, k knowledge.Entry, w int) []string {
	var lines []string
	lines = append(lines, boldStyle.Render(truncate(e.Name, w)))
	lines = append(lines, dimStyle.Render(truncate(e.Path, w)))
	lines = append(lines, "")

	lines = append(lines, m.sizeSummaryLine(e, k, w))
	lines = append(lines, truncate(fmt.Sprintf("Modified: %s", timeAgo(e.ModTime)), w))
	// Safety mixes plain text with an already-styled stars glyph, so it
	// needs ANSI-aware truncation (plain truncate() would miscount the
	// embedded escape codes as visible characters).
	lines = append(lines, ansi.Truncate(fmt.Sprintf("Safety: %s %s", renderStars(k.Score), scoreLabel(k.Score)), w, "…"))
	if k.Orphan {
		lines = append(lines, yellowStyle.Render(truncate("⚠ No installed app owns this", w)))
	}
	switch {
	case k.Container:
		for _, l := range wrapText("Enter to open — holds several version caches, see inside to clean them", w) {
			lines = append(lines, dimStyle.Render(l))
		}
	case e.IsDir:
		lines = append(lines, dimStyle.Render("Enter to open"))
	}

	if len(k.Commands) > 0 {
		lines = append(lines, "")
		lines = append(lines, boldStyle.Render("Commands"))
		info, haveReclaim := m.reclaimCache[e.Path]
		// Commands and CleanPaths line up one-to-one only after comment
		// lines are dropped — entries commonly open with a "# Quit X
		// first" note — so track a separate cursor into CleanPaths that
		// advances on real commands alone. Zipping on raw index would
		// shift every size onto the wrong command as soon as a comment
		// appeared.
		zipped := haveReclaim && info.ready && countRealCommands(k.Commands) == len(k.CleanPaths)
		pathIdx := 0
		for _, cmd := range k.Commands {
			if strings.HasPrefix(cmd, "#") {
				lines = append(lines, faintStyle.Render(truncate(cmd, w)))
				continue
			}
			sizeSuffix := ""
			if zipped {
				if sz, ok := info.perPath[k.CleanPaths[pathIdx]]; ok {
					sizeSuffix = "  (" + formatSize(sz) + ")"
				}
			}
			pathIdx++
			cmdW := w - 2 - displayWidth(sizeSuffix)
			line := accentStyle.Render("$ ") + dimStyle.Render(truncate(cmd, cmdW))
			if sizeSuffix != "" {
				line += faintStyle.Render(sizeSuffix)
			}
			lines = append(lines, line)
		}
	}

	if k.Native != nil {
		lines = append(lines, "")
		lines = append(lines, yellowStyle.Render("Native clean available"))
		lines = append(lines, wrapText(k.Native.Description, w)...)
		lines = append(lines, accentStyle.Render("$ ")+dimStyle.Render(truncate(k.Native.Command, w-2)))
	}
	return lines
}

// countRealCommands counts the commands that actually run, ignoring the
// "#"-prefixed annotation lines the dictionary uses for prerequisites and
// equivalences.
func countRealCommands(cmds []string) int {
	n := 0
	for _, c := range cmds {
		if !strings.HasPrefix(c, "#") {
			n++
		}
	}
	return n
}

// buildDescLines renders the descriptive half — what the folder is and
// what happens if you clean it.
func buildDescLines(k knowledge.Entry, w int) []string {
	var lines []string
	desc := k.Description
	if desc == "" {
		desc = "No information yet — this folder hasn't been researched. It is skipped from cleaning until it's added to the knowledge base."
	}
	lines = append(lines, boldStyle.Render("Description"))
	lines = append(lines, wrapText(desc, w)...)

	if k.Effects != "" {
		lines = append(lines, "")
		lines = append(lines, boldStyle.Render("If you clean it"))
		lines = append(lines, wrapText(k.Effects, w)...)
	}
	return lines
}

// scrollWindow returns exactly `visible` lines starting at offset (clamped
// to a valid range), padded with blanks if the content is shorter. When
// there's more content above/below the window, the first/last line is
// replaced with a scroll hint so truncation never looks like the content
// just silently stops mid-sentence.
func scrollWindow(lines []string, offset, visible int) []string {
	if visible < 0 {
		visible = 0
	}
	total := len(lines)
	maxStart := total - visible
	if maxStart < 0 {
		maxStart = 0
	}
	start := offset
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > total {
		end = total
	}

	window := append([]string{}, lines[start:end]...)
	for len(window) < visible {
		window = append(window, "")
	}
	if start > 0 && len(window) > 0 {
		window[0] = faintStyle.Render("↑ more above (PgUp)")
	}
	if end < total && len(window) > 0 {
		window[len(window)-1] = faintStyle.Render("↓ more below (PgDn)")
	}
	return window
}

func scoreLabel(s knowledge.Score) string {
	switch s {
	case knowledge.Safe:
		return "safe to delete"
	case knowledge.Caution:
		return "caution — may lose cached state"
	case knowledge.Risky:
		return "risky — may lose app data, delete at your own risk"
	default:
		return "unknown"
	}
}

// buttonAction identifies what a rendered button does when clicked/pressed.
type buttonAction int

const (
	actionNone buttonAction = iota
	actionClean
	actionNativeClean
	actionManualDelete
)

type uiButton struct {
	action buttonAction
	label  string
	style  lipgloss.Style
}

// cleanButtons decides which buttons appear below the detail panel for the
// current selection: always one clean-related button (enabled or a
// disabled state explaining why not), a second native-clean button when
// the knowledge entry defines one, and always a manual-delete button last
// — the always-available, un-gated "delete this whole folder, entirely at
// your own risk" override, regardless of what (if anything) the knowledge
// base says about it.
func (m Model) cleanButtons() []uiButton {
	e := m.selectedEntry()
	if e == nil {
		return []uiButton{{label: "CLEAN  (no folder selected)", style: disabledButtonStyle}}
	}
	k := m.knowledgeFor(e)

	var btns []uiButton
	switch {
	case k.CanClean():
		btns = append(btns, uiButton{action: actionClean, label: "CLEAN  (d)", style: enabledButtonStyle})
	case k.Container:
		btns = append(btns, uiButton{label: "CLEAN UNAVAILABLE  (open this folder to clean what's inside)", style: disabledButtonStyle})
	case k.Score == knowledge.Unknown:
		btns = append(btns, uiButton{label: "CLEAN UNAVAILABLE  (folder not reviewed yet)", style: disabledButtonStyle})
	default:
		btns = append(btns, uiButton{label: "CLEAN UNAVAILABLE  (no clean instructions written yet)", style: disabledButtonStyle})
	}
	if k.Native != nil {
		btns = append(btns, uiButton{action: actionNativeClean, label: "NATIVE CLEAN  (n)", style: nativeButtonStyle})
	}
	btns = append(btns, uiButton{action: actionManualDelete, label: "MANUALLY DELETE  (D)", style: dangerButtonStyle})
	return btns
}

// buttonWidth is a button's total rendered width: label + horizontal
// padding (0, 3) + the border on each side.
func buttonWidth(label string) int {
	return displayWidth(label) + 6 + 2
}

const buttonGap = 2

// spinnerFrames animates the "cleaning…" indicator. Native-clean commands
// shell out to real tools (Homebrew, go, pnpm) that can take anywhere from
// under a second to tens of seconds, so this is the only feedback the user
// gets that something is actually happening rather than the app being
// frozen.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderCleanButton is the standalone action row below the detail panel.
// It always renders at exactly cleanButtonHeight; buttons are centered as
// a group using the same width arithmetic buttonRegions uses, so clicks
// always land where they visually appear.
func (m Model) renderCleanButton(width int) string {
	if m.cleaning {
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		msg := fmt.Sprintf("%s cleaning… this can take a while for native commands like brew cleanup", frame)
		return lipgloss.Place(width, cleanButtonHeight, lipgloss.Center, lipgloss.Center, statusStyle.Render(msg))
	}
	if m.mode != modeNormal {
		return lipgloss.NewStyle().Width(width).Height(cleanButtonHeight).Render("")
	}

	btns := m.cleanButtons()
	blocks := make([]string, 0, len(btns)*2-1)
	spacer := lipgloss.NewStyle().Height(cleanButtonHeight).Render(strings.Repeat(" ", buttonGap))
	for i, b := range btns {
		if i > 0 {
			blocks = append(blocks, spacer)
		}
		blocks = append(blocks, b.style.Render(b.label))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	return lipgloss.Place(width, cleanButtonHeight, lipgloss.Center, lipgloss.Center, row)
}

// buttonRegion is a mouse hit-test rectangle for a clean/native-clean
// button.
type buttonRegion struct {
	action buttonAction
	x0, x1 int
}

// buttonRegions computes click regions for the button row, replicating
// renderCleanButton's centering math independently (rather than trying to
// recover coordinates from lipgloss.Place's output) so it stays simple and
// exact.
func (m Model) buttonRegions(width int) []buttonRegion {
	btns := m.cleanButtons()
	widths := make([]int, len(btns))
	total := 0
	for i, b := range btns {
		widths[i] = buttonWidth(b.label)
		total += widths[i]
	}
	total += buttonGap * (len(btns) - 1)
	leftPad := (width - total) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	regions := make([]buttonRegion, 0, len(btns))
	x := leftPad
	for i, b := range btns {
		regions = append(regions, buttonRegion{action: b.action, x0: x, x1: x + widths[i] - 1})
		x += widths[i] + buttonGap
	}
	return regions
}

func (m Model) renderConfirm(width, height int) string {
	innerH := height - 2
	innerW := width - 4
	if innerW < 20 {
		innerW = 20
	}
	e := m.selectedEntry()
	name := ""
	if e != nil {
		name = e.Name
	}

	borderColor := cYellow

	var lines []string
	switch m.mode {
	case modeConfirmNative:
		lines = append(lines, fmt.Sprintf("Run native clean for %q?", name))
		lines = append(lines, "")
		lines = append(lines, accentStyle.Render("$ ")+dimStyle.Render(truncate(m.confirmNativeCmd, innerW-2)))
		lines = append(lines, "")
		lines = append(lines, statusStyle.Render("[y] Yes, run it")+"    "+dimStyle.Render("[n/esc] Cancel"))

	case modeConfirmManualDelete:
		borderColor = cRed
		lines = append(lines, redStyle.Render(fmt.Sprintf("Permanently delete the ENTIRE folder %q?", name)))
		lines = append(lines, "")
		for _, l := range wrapText(
			"This is the manual override — it bypasses the knowledge base entirely and removes the folder itself, "+
				"not just its contents. Not gated by any safety rating. This cannot be undone.", innerW) {
			lines = append(lines, l)
		}
		lines = append(lines, "")
		lines = append(lines, accentStyle.Render("$ ")+dimStyle.Render(truncate("rm -rf "+m.confirmPath, innerW-2)))
		lines = append(lines, "")
		lines = append(lines, redStyle.Render("[y] Yes, delete it permanently")+"    "+dimStyle.Render("[n/esc] Cancel"))

	default:
		lines = append(lines, fmt.Sprintf("Clean %q?", name))
		lines = append(lines, "")
		if len(m.confirmCleanPaths) == 0 {
			lines = append(lines, "This removes everything inside the folder (not the folder itself):")
			lines = append(lines, accentStyle.Render("$ ")+dimStyle.Render(truncate("rm -rf "+m.confirmPath+"/*", innerW-2)))
		} else {
			info, ready := m.reclaimCache[m.confirmPath]
			ready = ready && info.ready
			if ready {
				lines = append(lines, fmt.Sprintf(
					"This only removes the specific cache paths below (freeing %s) — the rest of the folder is left alone:",
					formatSize(info.total)))
			} else {
				lines = append(lines, "This only removes the specific cache paths below — the rest of the folder is left alone:")
			}
			for _, pat := range m.confirmCleanPaths {
				cmd := "rm -rf " + filepath.Join(m.confirmPath, pat)
				sizeSuffix := ""
				if ready {
					if sz, ok := info.perPath[pat]; ok {
						sizeSuffix = "  (" + formatSize(sz) + ")"
					}
				}
				line := accentStyle.Render("$ ") + dimStyle.Render(truncate(cmd, innerW-2-displayWidth(sizeSuffix)))
				if sizeSuffix != "" {
					line += faintStyle.Render(sizeSuffix)
				}
				lines = append(lines, line)
			}
		}
		lines = append(lines, "")
		lines = append(lines, statusStyle.Render("[y] Yes, clean it")+"    "+dimStyle.Render("[n/esc] Cancel"))
	}

	msg := strings.Join(lines, "\n")
	return boxStyle.Width(width - 2).Height(innerH).BorderForeground(lipgloss.Color(borderColor)).
		Render(lipgloss.Place(width-4, innerH, lipgloss.Center, lipgloss.Center, msg))
}

func (m Model) renderHelp() string {
	switch m.mode {
	case modeConfirmClean:
		return helpStyle.Render("confirm the cleanup above")
	case modeConfirmNative:
		return helpStyle.Render("confirm the native clean above")
	case modeConfirmManualDelete:
		return redStyle.Render("confirm the permanent delete above")
	}
	left := "↑/↓ select   Enter open   d clean   n native clean   D delete   Esc/⌫ back   PgUp/PgDn scroll   " +
		"click headers to sort   tab switch   r rescan   q quit"
	if !m.activeTab.browsable() {
		left = "tab switch tabs   q quit"
	}
	if m.statusMsg != "" {
		return helpStyle.Render(left) + "   " + statusStyle.Render(m.statusMsg)
	}
	return helpStyle.Render(left)
}

// --- string helpers -------------------------------------------------------

func displayWidth(s string) int { return runewidth.StringWidth(s) }

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if displayWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

func padRight(s string, w int) string {
	s = truncate(s, w)
	d := displayWidth(s)
	if d >= w {
		return s
	}
	return s + strings.Repeat(" ", w-d)
}

func padLeft(s string, w int) string {
	s = truncate(s, w)
	d := displayWidth(s)
	if d >= w {
		return s
	}
	return strings.Repeat(" ", w-d) + s
}

func padCenter(s string, w int) string {
	s = truncate(s, w)
	d := displayWidth(s)
	if d >= w {
		return s
	}
	left := (w - d) / 2
	right := w - d - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// ansiPadRight pads s with trailing spaces up to display width w, using
// ANSI-aware width measurement so it's safe to call on strings that already
// contain lipgloss/ANSI styling — unlike padRight/truncate above, which
// assume plain text and must only ever be used *before* styling is applied.
func ansiPadRight(s string, w int) string {
	d := ansi.StringWidth(s)
	if d >= w {
		return s
	}
	return s + strings.Repeat(" ", w-d)
}

func formatSize(n int64) string {
	if n < 0 {
		return "…"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func wrapText(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if displayWidth(line)+1+displayWidth(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	lines = append(lines, line)
	return lines
}
