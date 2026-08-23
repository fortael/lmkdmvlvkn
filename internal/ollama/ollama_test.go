package ollama

import (
	"os"
	"path/filepath"
	"testing"
)

// store builds a synthetic Ollama model store and points OLLAMA_MODELS at
// it, so tests never touch a real one.
type store struct{ root string }

func newStore(t *testing.T) *store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "models")
	for _, d := range []string{"blobs", "manifests"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OLLAMA_MODELS", root)
	return &store{root: root}
}

// blob writes a blob of the given size and returns its digest.
func (s *store) blob(t *testing.T, name string, size int) string {
	t.Helper()
	digest := "sha256-" + name
	if err := os.WriteFile(filepath.Join(s.root, "blobs", digest), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return "sha256:" + name
}

// model writes a manifest at the given path relative to manifests/.
func (s *store) model(t *testing.T, rel string, config string, layers ...string) {
	t.Helper()
	p := filepath.Join(s.root, "manifests", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":2,"config":{"digest":"` + config + `"},"layers":[`
	for i, l := range layers {
		if i > 0 {
			body += ","
		}
		body += `{"digest":"` + l + `"}`
	}
	body += `]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(models []Model, name string) (Model, bool) {
	for _, m := range models {
		if m.Name == name {
			return m, true
		}
	}
	return Model{}, false
}

// The whole point of the package: name models without the daemon.
func TestListNamesModelsAndSizesThem(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "aaa", 1024)
	weights := s.blob(t, "bbb", 64*1024)
	s.model(t, "registry.ollama.ai/library/llama3.1/8b", cfg, weights)

	models, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	m := models[0]
	if m.Name != "llama3.1:8b" {
		t.Errorf("Name = %q, want llama3.1:8b", m.Name)
	}
	if len(m.Blobs) != 2 || len(m.Exclusive) != 2 {
		t.Errorf("blobs=%d exclusive=%d, want 2 and 2", len(m.Blobs), len(m.Exclusive))
	}
	if m.Size < 64*1024 {
		t.Errorf("Size = %d, want at least the 64KB of weights", m.Size)
	}
	if m.Shared != 0 {
		t.Errorf("Shared = %d, want 0 with only one model installed", m.Shared)
	}
}

// A blob two models both reference must not be counted as freed by
// removing either of them, or the app would promise space it can't
// deliver.
func TestSharedBlobsAreNotCountedAsFreed(t *testing.T) {
	s := newStore(t)
	common := s.blob(t, "common", 100*1024)
	cfgA := s.blob(t, "cfga", 512)
	cfgB := s.blob(t, "cfgb", 512)
	ownA := s.blob(t, "owna", 20*1024)

	s.model(t, "registry.ollama.ai/library/alpha/latest", cfgA, common, ownA)
	s.model(t, "registry.ollama.ai/library/beta/latest", cfgB, common)

	models, err := List()
	if err != nil {
		t.Fatal(err)
	}
	alpha, ok := find(models, "alpha:latest")
	if !ok {
		t.Fatal("alpha:latest missing")
	}
	for _, b := range alpha.Exclusive {
		if filepath.Base(b) == "sha256-common" {
			t.Error("the shared blob was listed as exclusive to alpha")
		}
	}
	if alpha.Shared < 100*1024 {
		t.Errorf("alpha.Shared = %d, want at least the 100KB shared blob", alpha.Shared)
	}
	if alpha.Size >= 100*1024 {
		t.Errorf("alpha.Size = %d, must exclude the shared blob", alpha.Size)
	}
	// Removing alpha must not delete the blob beta still needs.
	for _, p := range alpha.RemovePaths() {
		if filepath.Base(p) == "sha256-common" {
			t.Fatal("removing alpha would delete a blob beta still references")
		}
	}
}

// RemovePaths is what the clean action runs, so it has to cover the
// manifest as well — leaving it behind makes `ollama run` fail on a model
// whose weights are gone.
func TestRemovePathsIncludesTheManifest(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "cfg", 512)
	w := s.blob(t, "w", 4096)
	s.model(t, "registry.ollama.ai/library/solo/latest", cfg, w)

	models, err := List()
	if err != nil {
		t.Fatal(err)
	}
	paths := models[0].RemovePaths()
	var sawManifest bool
	for _, p := range paths {
		if p == models[0].Manifest {
			sawManifest = true
		}
	}
	if !sawManifest {
		t.Error("RemovePaths omits the manifest")
	}
	if len(paths) != len(models[0].Exclusive)+1 {
		t.Errorf("RemovePaths has %d entries, want exclusive blobs + manifest", len(paths))
	}
}

// Orphans are the residue `ollama rm` can never reach, so they are the
// one thing the app can offer that Ollama itself cannot.
func TestOrphansFindsUnreferencedBlobs(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "cfg", 512)
	w := s.blob(t, "w", 4096)
	s.model(t, "registry.ollama.ai/library/solo/latest", cfg, w)
	s.blob(t, "leftover", 32*1024) // written by nothing that survived

	orphans, total, err := Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 {
		t.Fatalf("got %d orphans, want 1: %v", len(orphans), orphans)
	}
	if filepath.Base(orphans[0]) != "sha256-leftover" {
		t.Errorf("orphan = %s, want sha256-leftover", filepath.Base(orphans[0]))
	}
	if total < 32*1024 {
		t.Errorf("total = %d, want at least 32KB", total)
	}
}

func TestOrphansEmptyWhenEverythingIsReferenced(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "cfg", 512)
	w := s.blob(t, "w", 4096)
	s.model(t, "registry.ollama.ai/library/solo/latest", cfg, w)

	orphans, total, err := Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 || total != 0 {
		t.Errorf("got %d orphans (%d bytes), want none", len(orphans), total)
	}
}

func TestModelName(t *testing.T) {
	tests := []struct{ rel, want string }{
		{"registry.ollama.ai/library/llama3.1/8b", "llama3.1:8b"},
		{"registry.ollama.ai/library/all-minilm/latest", "all-minilm:latest"},
		// A third-party namespace stays visible, or two different models
		// could display under one name.
		{"registry.ollama.ai/someuser/custom/v2", "someuser/custom:v2"},
		{"hf.co/user/repo/q4", "hf.co/user/repo:q4"},
	}
	for _, tt := range tests {
		if got := modelName(tt.rel); got != tt.want {
			t.Errorf("modelName(%q) = %q, want %q", tt.rel, got, tt.want)
		}
	}
}

// A half-written manifest is what an interrupted pull leaves behind. It
// must be skipped, not reported as a model with no blobs — which would
// offer a clean that frees nothing.
func TestMalformedManifestsAreSkipped(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "cfg", 512)
	s.model(t, "registry.ollama.ai/library/good/latest", cfg)

	bad := filepath.Join(s.root, "manifests", "registry.ollama.ai", "library", "broken", "latest")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(`{"schemaVersion":2,"conf`), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "good:latest" {
		t.Errorf("got %+v, want only good:latest", models)
	}
}

// A manifest naming a blob that isn't on disk must not produce a
// remove-path pointing at nothing.
func TestMissingBlobsAreIgnored(t *testing.T) {
	s := newStore(t)
	cfg := s.blob(t, "cfg", 512)
	s.model(t, "registry.ollama.ai/library/partial/latest", cfg, "sha256:neverdownloaded")

	models, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	for _, b := range models[0].Blobs {
		if _, err := os.Stat(b); err != nil {
			t.Errorf("Blobs includes a path that does not exist: %s", b)
		}
	}
}

func TestAvailableFalseWithoutAStore(t *testing.T) {
	t.Setenv("OLLAMA_MODELS", filepath.Join(t.TempDir(), "nope"))
	if Available() {
		t.Error("Available() = true with no store on disk")
	}
}
