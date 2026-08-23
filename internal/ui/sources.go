package ui

// Adapters turning the Docker and Vendors scanners into ordinary table
// rows, so both tabs get the existing browser, sorting, selection and
// batch machinery for free instead of growing their own.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/docker"
	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/ollama"
	"lmkdmvlvkn/internal/scan"
	"lmkdmvlvkn/internal/vendors"
)

// scanTimeout bounds the external scanners. Docker shells out per object
// and the vendor walk covers a whole home directory; neither should be
// able to hang the tab forever if something goes wrong underneath.
const scanTimeout = 90 * time.Second

// --- Docker ---------------------------------------------------------------

type dockerLoadedMsg struct {
	items     []docker.Item
	available bool
	reason    string
	err       error
}

func loadDockerCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		defer cancel()

		if ok, reason := docker.AvailableContext(ctx); !ok {
			return dockerLoadedMsg{available: false, reason: reason}
		}
		items, err := docker.Scan(ctx)
		return dockerLoadedMsg{items: items, available: true, err: err}
	}
}

// dockerPath is the synthetic identity of a Docker object.
//
// These rows have no file to point at — the bytes live inside the VM's
// disk image — but the table keys selection and sizing by path, so each
// object needs a stable unique one. The scheme prefix also marks the row
// as virtual: nothing may ever try to stat or rm -rf it.
func dockerPath(it docker.Item) string {
	return fmt.Sprintf("docker://%s/%s", it.Kind, it.ID)
}

// dockerScore maps Docker's verdict onto the safety rating the table
// already knows how to draw. The two scales were designed to line up.
func dockerScore(v docker.Verdict) knowledge.Score {
	switch v {
	case docker.VerdictDisposable:
		return knowledge.Safe
	case docker.VerdictReview:
		return knowledge.Caution
	case docker.VerdictKeep:
		return knowledge.Risky
	default:
		return knowledge.Unknown
	}
}

// dockerEntries converts a scan into table rows plus their dictionary
// entries. Sizes come from Docker itself and are marked ready
// immediately, so the filesystem scanner is never asked to measure a path
// that doesn't exist.
func (m *Model) dockerEntries(items []docker.Item) []*scan.Entry {
	entries := make([]*scan.Entry, 0, len(items))
	for _, it := range items {
		path := dockerPath(it)

		name := it.Name
		switch {
		case it.Shared:
			name += fmt.Sprintf("  ★ shared by %d containers", it.RefCount)
		case it.Anonymous:
			name += "  ⚠ anonymous"
		}

		// Prefer the last-used time, falling back to creation: the whole
		// point of the tab is spotting things nothing has touched in
		// months.
		stamp := it.LastUsed
		if stamp.IsZero() {
			stamp = it.Created
		}

		k := knowledge.Entry{
			Score:       dockerScore(it.Verdict),
			Description: it.Description,
			Effects:     it.Effects,
		}
		if it.RemoveCmd != "" {
			// Removal always goes through the docker CLI rather than us
			// touching the VM's disk image, which is exactly what the
			// Native mechanism is for.
			k.Native = &knowledge.NativeClean{
				Description: "Removes this object through the Docker CLI, so Docker updates its own bookkeeping " +
					"and reclaims the space inside the VM image itself.",
				Command: it.RemoveCmd,
			}
		}
		m.pathDB[path] = k
		m.dockerItems[path] = it

		entries = append(entries, &scan.Entry{
			Name:      name,
			Path:      path,
			Source:    it.Kind.String(),
			Root:      string(knowledge.RootDocker),
			ModTime:   stamp,
			IsDir:     false,
			Size:      it.Size,
			SizeReady: true,
		})
	}
	return entries
}

// --- Vendors --------------------------------------------------------------

type vendorsLoadedMsg struct {
	items []vendors.Item
	err   error
}

func loadVendorsCmd() tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return vendorsLoadedMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		defer cancel()

		items, err := vendors.Scan(ctx, home)
		return vendorsLoadedMsg{items: items, err: err}
	}
}

// vendorEntries converts discovered dependency directories into rows.
//
// These get generated prose rather than curated prose, deliberately: the
// user's call is per-project, not per-folder — "am I still working on
// this?" — and the facts that answer it are the project name, the age and
// the one command that brings it back. Writing a paragraph about every
// node_modules on the disk would add nothing.
func (m *Model) vendorEntries(items []vendors.Item) []*scan.Entry {
	entries := make([]*scan.Entry, 0, len(items))
	for _, it := range items {
		name := it.Project + "/" + it.Kind.Dir

		m.pathDB[it.Path] = knowledge.Entry{
			Score: knowledge.Caution,
			Description: fmt.Sprintf(
				"%s belonging to the project %q at %s. Everything in here was installed from that project's "+
					"manifest and can be reinstalled from it, so the only question worth asking is whether you "+
					"are still working on this project.",
				it.Kind.Dir, it.Project, it.ProjectPath),
			Effects: fmt.Sprintf(
				"Deletes the whole directory. Your source code and the manifest that describes these dependencies "+
					"are untouched — run `%s` in %s to get them back, which needs network access and takes as long "+
					"as a fresh install. The MOD column is the last time the project's own sources changed, not "+
					"this directory, so it tells you how long since you actually worked here.",
				it.Kind.Restore, it.ProjectPath),
			Commands: []string{
				"rm -rf " + it.Path,
				"# restore with: cd " + it.ProjectPath + " && " + it.Kind.Restore,
			},
		}

		entries = append(entries, &scan.Entry{
			Name:    name,
			Path:    it.Path,
			Source:  it.Kind.Name,
			Root:    string(knowledge.RootVendors),
			ModTime: it.ModTime,
			IsDir:   true,
			Size:    -1,
		})
	}
	return entries
}

// --- Docker detail rendering ---------------------------------------------

// dockerRowLabelW is the label column in the Docker detail panel. Wide
// enough for "Pulled or tagged here", the longest label the package emits.
const dockerRowLabelW = 22

// maxWrappedLines caps how far one long value is allowed to wrap. Build
// commands run to several hundred characters; showing every one in full
// would bury the layer list it belongs to, and the point of that list is
// that it can be skimmed.
const maxWrappedLines = 3

// dockerDetailLines renders an Item's sections into display lines.
//
// The Docker tab deliberately drops the two-column description/effects
// layout the folder tabs use. Its content is a sequence of titled
// label/value blocks and long verbatim build commands, neither of which
// survives being squeezed into half the width — and a hash with a size
// next to it was exactly the black box this replaces.
func (m Model) dockerDetailLines(it docker.Item, w int) []string {
	var lines []string
	for _, sec := range it.Details() {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, boldStyle.Render(truncate(sec.Title, w)))

		for _, r := range sec.Rows {
			label := dimStyle.Render(padRight(truncate(r.Label, dockerRowLabelW), dockerRowLabelW))
			wrapped := wrapText(r.Value, w-dockerRowLabelW-1)
			if len(wrapped) == 0 {
				lines = append(lines, label)
				continue
			}
			lines = append(lines, label+" "+wrapped[0])
			for _, cont := range clampLines(wrapped[1:], maxWrappedLines-1) {
				lines = append(lines, strings.Repeat(" ", dockerRowLabelW+1)+cont)
			}
		}

		for _, l := range sec.Lines {
			wrapped := clampLines(wrapText(l, w-4), maxWrappedLines)
			for i, cont := range wrapped {
				prefix := "  "
				if i > 0 {
					prefix = "    "
				}
				lines = append(lines, faintStyle.Render(prefix+cont))
			}
		}
	}
	return lines
}

// clampLines truncates a wrapped value to at most n lines, marking the cut
// so a shortened build command never reads as a complete one.
func clampLines(lines []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(lines) <= n {
		return lines
	}
	out := append([]string{}, lines[:n]...)
	if n > 0 {
		out[n-1] += " …"
	}
	return out
}

// renderDockerDetail draws the Docker panel: full width, scrollable, with
// the same toolbar the other tabs use.
func (m Model) renderDockerDetail(e *scan.Entry, k knowledge.Entry, width, innerW, innerH int) string {
	it, ok := m.dockerItems[e.Path]
	if !ok {
		// No Docker object behind this row — fall back rather than showing
		// an empty panel.
		return m.renderDetailNarrow(e, k, width, innerW, innerH)
	}

	lines := m.dockerDetailLines(it, innerW)
	if k.Native != nil {
		lines = append(lines, "")
		lines = append(lines, yellowStyle.Render("Removal command"))
		lines = append(lines, accentStyle.Render("$ ")+dimStyle.Render(truncate(k.Native.Command, innerW-2)))
	}

	bodyH := innerH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	window := scrollWindow(lines, m.detailScroll, bodyH)
	toolbar := m.renderDetailToolbar(innerW, m.detailScroll > 0, len(lines) > bodyH+m.detailScroll)
	return boxStyle.Width(width - 2).Height(innerH).Render(toolbar + "\n" + strings.Join(window, "\n"))
}

// --- Ollama models --------------------------------------------------------

type ollamaLoadedMsg struct {
	frameID int
	models  []ollama.Model
	orphans []string
	orphanB int64
	err     error
}

// loadOllamaCmd reads Ollama's model store off disk.
func loadOllamaCmd(frameID int) tea.Cmd {
	return func() tea.Msg {
		models, err := ollama.List()
		if err != nil {
			return ollamaLoadedMsg{frameID: frameID, err: err}
		}
		orphans, orphanB, _ := ollama.Orphans()
		return ollamaLoadedMsg{frameID: frameID, models: models, orphans: orphans, orphanB: orphanB}
	}
}

// ollamaEntries turns installed models into rows.
//
// Opening ~/.ollama used to lead to models/blobs and a wall of
// sha256-<64 hex> files with no way to tell which model any of them
// belonged to — the same black box as an anonymous Docker volume. Listing
// models instead means the choice is "do I still want llama3.1:8b",
// which is the only question the user can actually answer.
func (m *Model) ollamaEntries(models []ollama.Model, orphans []string, orphanBytes int64) []*scan.Entry {
	entries := make([]*scan.Entry, 0, len(models)+1)

	for _, mod := range models {
		shared := ""
		if mod.Shared > 0 {
			shared = fmt.Sprintf(" It also uses %s of blobs shared with other models, which stay put.",
				formatSize(mod.Shared))
		}

		commands := make([]string, 0, len(mod.RemovePaths())+1)
		commands = append(commands, "# Works with the Ollama server stopped")
		for _, p := range mod.RemovePaths() {
			commands = append(commands, "rm -f "+p)
		}

		m.pathDB[mod.Manifest] = knowledge.Entry{
			Score: knowledge.Caution,
			Description: fmt.Sprintf(
				"The installed Ollama model %q: one manifest plus the %d blobs it references, of which %d are used "+
					"by no other model. Removing it frees %s.%s",
				mod.Name, len(mod.Blobs), len(mod.Exclusive), formatSize(mod.Size), shared),
			Effects: fmt.Sprintf(
				"Deletes this model's manifest and the blobs nothing else needs — exactly what `ollama rm %s` does, "+
					"but by editing the store directly, so it works with the Ollama server stopped. Every other "+
					"model stays installed and usable. Getting this one back means `ollama pull %s`, a fresh "+
					"%s download.",
				mod.Name, mod.Name, formatSize(mod.Size)),
			Commands: commands,
			// Absolute, because a model's files live under two different
			// directories — see resolveCleanPath.
			CleanPaths: mod.RemovePaths(),
			Native: &knowledge.NativeClean{
				Description: "Asks Ollama to remove the model, so it updates its own bookkeeping. This one needs " +
					"the Ollama server running; the plain clean above does the same thing without it.",
				Command: "ollama rm " + mod.Name,
			},
		}

		entries = append(entries, &scan.Entry{
			Name:      mod.Name,
			Path:      mod.Manifest,
			Source:    "model",
			Root:      string(knowledge.RootHome),
			ModTime:   mod.Modified,
			IsDir:     false,
			Size:      mod.Size,
			SizeReady: true,
		})
	}

	// Blobs no manifest names. `ollama rm` can never reach these, so
	// without a row for them they are invisible and permanent.
	if len(orphans) > 0 {
		root := ollama.Root()
		commands := make([]string, 0, len(orphans)+1)
		commands = append(commands, "# Not referenced by any installed model")
		for _, p := range orphans {
			commands = append(commands, "rm -f "+p)
		}
		path := filepath.Join(root, "blobs", "#orphaned")
		m.pathDB[path] = knowledge.Entry{
			Score: knowledge.Safe,
			Description: fmt.Sprintf(
				"%d blob files in Ollama's store that no installed model's manifest references — the residue of an "+
					"interrupted pull, or of a model whose manifest was removed without its weights. Ollama itself "+
					"can never clean these up: `ollama rm` only removes what a manifest names.",
				len(orphans)),
			Effects: fmt.Sprintf(
				"Deletes %s of unreferenced files. No installed model refers to them, so nothing you can currently "+
					"run is affected, and `ollama list` looks exactly the same afterwards.", formatSize(orphanBytes)),
			Commands:   commands,
			CleanPaths: orphans,
		}
		entries = append(entries, &scan.Entry{
			Name:      fmt.Sprintf("orphaned blobs (%d)", len(orphans)),
			Path:      path,
			Source:    "orphan",
			Root:      string(knowledge.RootHome),
			IsDir:     false,
			Size:      orphanBytes,
			SizeReady: true,
		})
	}

	return entries
}

// ollamaRootParent is the ~/.ollama directory the Home tab lists, i.e.
// the parent of Ollama's model store. Empty when there is no store to
// read, so the drill-in never fires on a machine without Ollama.
func ollamaRootParent() string {
	if !ollama.Available() {
		return ""
	}
	root := ollama.Root()
	if root == "" {
		return ""
	}
	return filepath.Dir(root)
}
