// Package ollama reads Ollama's on-disk model store directly, so models
// can be listed and removed without the Ollama server running.
//
// This exists because `ollama rm` talks to a daemon. With the server
// stopped — the normal state on a Mac where you last used Ollama weeks
// ago — `ollama list` fails, so there is no way to name a model, and the
// only remaining option is deleting the entire blobs directory and losing
// every model at once. Meanwhile the store itself is plain files: a
// manifest per model listing the blob digests it needs. That is enough to
// do exactly what `ollama rm` does, offline and per model.
package ollama

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lmkdmvlvkn/internal/scan"
)

// Model is one installed model, as its manifest describes it.
type Model struct {
	// Name is the reference you would pass to `ollama rm`, e.g.
	// "llama3.1:8b" or "hf.co/user/repo:q4".
	Name string
	// Manifest is the absolute path of the manifest file.
	Manifest string
	// Blobs are the absolute paths of every blob this model references.
	Blobs []string
	// Exclusive are the blobs no other installed model references — the
	// ones that actually go away when this model is removed.
	Exclusive []string
	// Size is the on-disk size of Exclusive: what removing this model
	// really frees.
	Size int64
	// Shared is the on-disk size of blobs other models also use, which
	// removing this model does not free.
	Shared   int64
	Modified time.Time
}

// RemovePaths are the files to delete to remove this model: its manifest
// plus the blobs nothing else needs. This is what `ollama rm` does, and
// doing it in this order means an interrupted delete leaves orphaned
// blobs (recoverable, see Orphans) rather than a manifest pointing at
// weights that are gone (which makes `ollama run` fail confusingly).
func (m Model) RemovePaths() []string {
	return append(append([]string{}, m.Exclusive...), m.Manifest)
}

// Root is Ollama's model store, honouring OLLAMA_MODELS.
func Root() string {
	if custom := strings.TrimSpace(os.Getenv("OLLAMA_MODELS")); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ollama", "models")
}

// Available reports whether a model store exists to read.
func Available() bool {
	root := Root()
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, "manifests"))
	return err == nil && info.IsDir()
}

// manifest is the subset of Ollama's manifest format we need. It is the
// OCI image manifest shape: a config descriptor plus content layers, each
// carrying a "sha256:..." digest naming a file in blobs/.
type manifest struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
}

// List returns every installed model, largest first.
func List() ([]Model, error) {
	root := Root()
	if root == "" {
		return nil, nil
	}
	manifestRoot := filepath.Join(root, "manifests")
	blobRoot := filepath.Join(root, "blobs")

	// First pass: read every manifest and count how many models reference
	// each blob, so the second pass can tell exclusive from shared.
	type parsed struct {
		path     string
		name     string
		digests  []string
		modified time.Time
	}
	var found []parsed
	refs := make(map[string]int)

	err := filepath.WalkDir(manifestRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry skips, it doesn't abort the listing
		}
		digests, ok := readManifest(p)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(manifestRoot, p)
		if err != nil {
			return nil
		}
		found = append(found, parsed{path: p, name: modelName(rel), digests: digests, modified: info.ModTime()})
		for _, dg := range digests {
			refs[dg]++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	models := make([]Model, 0, len(found))
	for _, f := range found {
		m := Model{Name: f.name, Manifest: f.path, Modified: f.modified}
		for _, dg := range f.digests {
			blob := filepath.Join(blobRoot, dg)
			size := diskSize(blob)
			if size < 0 {
				continue // the manifest names a blob that isn't there
			}
			m.Blobs = append(m.Blobs, blob)
			if refs[dg] == 1 {
				m.Exclusive = append(m.Exclusive, blob)
				m.Size += size
				continue
			}
			m.Shared += size
		}
		models = append(models, m)
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].Size != models[j].Size {
			return models[i].Size > models[j].Size
		}
		return models[i].Name < models[j].Name
	})
	return models, nil
}

// Orphans returns blobs no manifest references, and their total size.
//
// These are the residue of an interrupted pull or a manifest removed
// without its blobs. `ollama rm` can never reach them — it only removes
// what a manifest names — so without something like this they sit there
// forever.
func Orphans() ([]string, int64, error) {
	root := Root()
	if root == "" {
		return nil, 0, nil
	}
	models, err := List()
	if err != nil {
		return nil, 0, err
	}
	referenced := make(map[string]bool)
	for _, m := range models {
		for _, b := range m.Blobs {
			referenced[b] = true
		}
	}

	blobRoot := filepath.Join(root, "blobs")
	items, err := os.ReadDir(blobRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	var orphans []string
	var total int64
	for _, it := range items {
		if it.IsDir() || !strings.HasPrefix(it.Name(), "sha256-") {
			continue
		}
		p := filepath.Join(blobRoot, it.Name())
		if referenced[p] {
			continue
		}
		if size := diskSize(p); size >= 0 {
			orphans = append(orphans, p)
			total += size
		}
	}
	sort.Strings(orphans)
	return orphans, total, nil
}

// readManifest extracts the blob filenames a manifest references,
// converting Ollama's "sha256:<hex>" digests into the "sha256-<hex>"
// filenames used in blobs/. Duplicates are collapsed: a manifest can name
// the same blob twice and it is still one file.
func readManifest(path string) ([]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var mf manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, false
	}

	seen := make(map[string]bool)
	var out []string
	add := func(digest string) {
		name := strings.Replace(strings.TrimSpace(digest), ":", "-", 1)
		if !strings.HasPrefix(name, "sha256-") || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(mf.Config.Digest)
	for _, l := range mf.Layers {
		add(l.Digest)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// modelName turns a manifest's path relative to manifests/ back into the
// reference you would type. Ollama lays these out as
// <registry>/<namespace>/<name>/<tag>, and hides the defaults: the
// official registry and its "library" namespace are both implicit, so
// registry.ollama.ai/library/llama3.1/8b is just llama3.1:8b, while a
// third-party model keeps enough of its path to stay unambiguous.
func modelName(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return rel
	}
	tag := parts[len(parts)-1]
	rest := parts[:len(parts)-1]

	if len(rest) > 0 && (rest[0] == "registry.ollama.ai" || rest[0] == "ollama.ai") {
		rest = rest[1:]
	}
	if len(rest) > 1 && rest[0] == "library" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return tag
	}
	return strings.Join(rest, "/") + ":" + tag
}

// diskSize is the space a blob actually occupies, or -1 if it is missing.
// It goes through scan.PathSize so these figures are allocated size, the
// same measure every other total in the app uses — a model's size here
// must add up with what the Home tab reports for ~/.ollama.
func diskSize(path string) int64 {
	size, _, err := scan.PathSize(path)
	if err != nil {
		return -1
	}
	return size
}
