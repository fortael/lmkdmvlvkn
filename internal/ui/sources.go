package ui

// Adapters turning the Docker and Vendors scanners into ordinary table
// rows, so both tabs get the existing browser, sorting, selection and
// batch machinery for free instead of growing their own.

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/docker"
	"lmkdmvlvkn/internal/knowledge"
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
