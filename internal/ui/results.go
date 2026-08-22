package ui

// The Results tab: what this app has actually removed, over its whole
// lifetime, read back from the persisted history log.
//
// Every other tab is a projection of what *could* be freed. This is the
// only one that reports what was, backed by a record written at the
// moment of deletion rather than by anything recomputed afterwards.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"lmkdmvlvkn/internal/history"
)

// renderResults draws the all-time summary, the freed-vs-disk bar, and
// the reverse-chronological list of removals.
func (m Model) renderResults(width, height int) string {
	if m.mode == modeConfirmClearHistory {
		return m.renderConfirm(width, height)
	}

	innerW := width - 4
	innerH := height - 2
	if innerW < 20 {
		innerW = 20
	}
	if innerH < 1 {
		innerH = 1
	}

	var lines []string

	lines = append(lines, boldStyle.Render("Freed by this app, all time"))
	lines = append(lines, "")
	lines = append(lines, accentStyle.Render(bigSize(m.historyTotal))+dimStyle.Render(
		fmt.Sprintf("   across %d cleanups", len(m.history))))
	lines = append(lines, "")
	lines = append(lines, m.freedBarLines(innerW)...)
	lines = append(lines, "")

	if m.historyErr != "" {
		lines = append(lines, errorStyle.Render(truncate("history: "+m.historyErr, innerW)))
		lines = append(lines, "")
	}

	if len(m.history) == 0 {
		for _, l := range wrapText("Nothing cleaned yet. Anything you remove from the other tabs is recorded here — "+
			"what it was, where it lived, and how much it actually freed.", innerW) {
			lines = append(lines, dimStyle.Render(l))
		}
		return boxStyle.Width(width - 2).Height(innerH).Render(strings.Join(
			scrollWindow(lines, m.detailScroll, innerH), "\n"))
	}

	lines = append(lines, boldStyle.Render("History"))
	lines = append(lines, m.historyHeader(innerW))
	for _, r := range m.history {
		lines = append(lines, m.historyRow(r, innerW))
	}

	window := scrollWindow(lines, m.detailScroll, innerH)
	return boxStyle.Width(width - 2).Height(innerH).Render(strings.Join(window, "\n"))
}

// freedBarLines renders the progress bar the user asked for: how much has
// been freed relative to the size of the disk it was freed from.
//
// The denominator is the volume's total capacity rather than, say, the
// sum of everything currently cleanable — that figure moves every time
// anything is deleted, so a bar drawn against it would slide backwards as
// the user made progress, which is precisely the wrong feedback.
func (m Model) freedBarLines(w int) []string {
	if m.disk.Total <= 0 {
		return nil
	}
	barW := w - 2
	if barW < 10 {
		barW = 10
	}

	frac := float64(m.historyTotal) / float64(m.disk.Total)
	if frac > 1 {
		frac = 1
	}
	filled := int(float64(barW) * frac)
	// Anything freed at all should show, even when it rounds to nothing
	// against a large disk — a bar reading empty after a real cleanup
	// looks broken.
	if m.historyTotal > 0 && filled == 0 {
		filled = 1
	}

	bar := greenStyle.Render(strings.Repeat("█", filled)) +
		faintStyle.Render(strings.Repeat("░", barW-filled))

	pct := frac * 100
	legend := fmt.Sprintf("%s freed of %s total disk (%.2f%%)  ·  %s free right now",
		formatSize(m.historyTotal), formatSize(m.disk.Total), pct, formatSize(m.disk.Free))

	return []string{bar, dimStyle.Render(truncate(legend, w))}
}

const (
	histColWhen   = 12
	histColAction = 8
	histColSize   = 10
	histColSource = 12
)

func (m Model) historyHeader(w int) string {
	nameW := w - histColWhen - histColAction - histColSize - histColSource - 4
	if nameW < 10 {
		nameW = 10
	}
	return headerRowStyle.Render(
		padRight("WHEN", histColWhen) + " " +
			padRight("ACTION", histColAction) + " " +
			padRight("FROM", histColSource) + " " +
			padRight("NAME", nameW) + " " +
			padLeft("FREED", histColSize))
}

func (m Model) historyRow(r history.Record, w int) string {
	nameW := w - histColWhen - histColAction - histColSize - histColSource - 4
	if nameW < 10 {
		nameW = 10
	}

	style := normalRowStyle
	freed := formatSize(r.Freed)
	if r.Err != "" {
		style = unknownRowStyle
		freed = "failed"
	}

	return style.Render(padRight(timeAgo(r.Time), histColWhen)) + " " +
		style.Render(padRight(string(r.Action), histColAction)) + " " +
		style.Render(padRight(truncate(r.Source, histColSource), histColSource)) + " " +
		style.Render(padRight(truncate(r.Name, nameW), nameW)) + " " +
		style.Render(padLeft(freed, histColSize))
}

// renderResultsButton is the Results tab's action row.
func (m Model) renderResultsButton(width int) string {
	if m.mode != modeNormal {
		return lipgloss.NewStyle().Width(width).Height(cleanButtonHeight).Render("")
	}
	var btn uiButton
	if len(m.history) == 0 {
		btn = uiButton{label: "CLEAR HISTORY  (nothing recorded yet)", style: disabledButtonStyle}
	} else {
		btn = uiButton{action: actionClearHistory, label: "CLEAR HISTORY  (c)", style: dangerButtonStyle}
	}
	return lipgloss.Place(width, cleanButtonHeight, lipgloss.Center, lipgloss.Center, btn.style.Render(btn.label))
}

// bigSize renders the headline total a little larger than the table
// figures by spacing it out; terminals give us no font size to work with.
func bigSize(n int64) string {
	s := formatSize(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return boldStyle.Render(b.String())
}
