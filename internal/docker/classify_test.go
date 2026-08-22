package docker

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Fixed clock for every test in this file, so "three weeks ago" means the
// same thing on every machine and in every timezone.
var testNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) string { return testNow.Add(-d).UTC().Format(time.RFC3339Nano) }

// jsonLines renders records the way the docker CLI does with
// `--format '{{json .}}'`: one JSON object per line.
func jsonLines[T any](t *testing.T, records ...T) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshalling fixture: %v", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func dfJSON(t *testing.T, df systemDF) []byte {
	t.Helper()
	b, err := json.Marshal(df)
	if err != nil {
		t.Fatalf("marshalling df fixture: %v", err)
	}
	return b
}

// find returns the classified item of the given kind and name, failing the
// test when it is missing.
func find(t *testing.T, items []Item, kind Kind, name string) Item {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("no %s named %q in %s", kind, name, itemNames(items))
	return Item{}
}

func itemNames(items []Item) string {
	var names []string
	for _, it := range items {
		names = append(names, it.Kind.String()+" "+it.Name)
	}
	return "[" + strings.Join(names, ", ") + "]"
}

func mustClassify(t *testing.T, snap snapshot) []Item {
	t.Helper()
	items, err := classify(snap, testNow)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	return items
}

// A full 64-character image ID as `docker system df -v` and `docker
// inspect` report it, from a 12-character prefix.
func fullImageID(prefix string) string {
	return "sha256:" + prefix + strings.Repeat("0", 64-len(prefix))
}

// The case the whole package exists for. Several compose projects
// commonly share one base image — postgres:16, redis:7 — and it is exactly
// the image a size-sorted cleanup tool recommends deleting first, forcing
// a re-pull on every project that shares it.
//
// The two containers deliberately join to the image by different routes:
// one through the resolved image ID that docker inspect reports, the other
// through the reference alone, because docker ps reports whichever of the
// two its container was created with.
func TestSharedImageIsKeptAndSaysWhoElseNeedsIt(t *testing.T) {
	snap := snapshot{
		ps: jsonLines(t,
			psLine{ID: "c1", Names: "api-db-1", Image: "postgres:16", State: "exited",
				Status: "Exited (0) 3 weeks ago", Labels: composeProjectLabel + "=api"},
			psLine{ID: "c2", Names: "billing-db-1", Image: "postgres:16", State: "exited",
				Status: "Exited (0) 5 days ago", Labels: composeProjectLabel + "=billing"},
		),
		// Only the first container is inspected; the second has to be
		// joined to the image by its reference.
		inspect: jsonLines(t, containerInspect{
			ID:    "c1",
			Name:  "/api-db-1",
			Image: fullImageID("aaaaaaaaaaaa"),
			State: struct {
				Status     string
				Running    bool
				StartedAt  string
				FinishedAt string
			}{Status: "exited", StartedAt: ago(30 * 24 * time.Hour), FinishedAt: ago(21 * 24 * time.Hour)},
			Config: struct {
				Image  string
				Labels map[string]string
			}{Image: "postgres:16", Labels: map[string]string{composeProjectLabel: "api"}},
		}),
		images:  jsonLines(t, imageLine{ID: "aaaaaaaaaaaa", Repository: "postgres", Tag: "16"}),
		volumes: nil,
		df: dfJSON(t, systemDF{Images: []imageLine{
			{ID: fullImageID("aaaaaaaaaaaa"), Size: "1.2GB", UniqueSize: "400MB", SharedSize: "800MB"},
		}}),
	}

	img := find(t, mustClassify(t, snap), KindImage, "postgres:16")

	if img.RefCount != 2 {
		t.Errorf("RefCount = %d, want 2 (one container joined by image ID, one by reference)", img.RefCount)
	}
	if !img.Shared {
		t.Error("Shared = false; an image two containers are built on is shared infrastructure")
	}
	if img.Verdict != VerdictKeep {
		t.Errorf("Verdict = %v, want Keep", img.Verdict)
	}
	if img.RemoveCmd != "" {
		t.Errorf("RemoveCmd = %q, want none: docker refuses to delete an image containers reference", img.RemoveCmd)
	}
	if img.CanRemove() {
		t.Error("CanRemove() = true for a shared image")
	}
	// The user's actual complaint is re-pulling these over and over, so
	// the consequence has to be spelled out rather than implied.
	for _, want := range []string{"every one of the projects", "registry"} {
		if !strings.Contains(img.Effects, want) {
			t.Errorf("Effects does not mention %q; it must say removing this forces a re-pull for every project.\nGot: %s", want, img.Effects)
		}
	}
	for _, want := range []string{"api", "billing"} {
		if !strings.Contains(img.Description, want) {
			t.Errorf("Description does not name the %q project, which is the fact that stops the deletion.\nGot: %s", want, img.Description)
		}
	}
	if !strings.Contains(img.Status, "2 compose projects") {
		t.Errorf("Status = %q, want it to report 2 compose projects", img.Status)
	}
	// Two projects means no single project owns it, and the prose names
	// them instead.
	if img.Project != "" {
		t.Errorf("Project = %q, want empty for an image shared by several projects", img.Project)
	}
	// Reclaimable, not apparent: the 800MB of shared base layers survive
	// this image's deletion because other images still need them.
	if img.Size != 400_000_000 {
		t.Errorf("Size = %d, want 400000000 (UniqueSize); the apparent 1.2GB counts layers that would survive", img.Size)
	}
	// The newest thing either container did.
	if want := testNow.Add(-21 * 24 * time.Hour); !img.LastUsed.Equal(want) {
		t.Errorf("LastUsed = %v, want %v borrowed from the most recently used container", img.LastUsed, want)
	}
}

// How many containers reference an image is what decides its fate, so the
// whole range gets a table.
func TestImageVerdictByReferenceCount(t *testing.T) {
	tests := []struct {
		name        string
		containers  []psLine
		repository  string
		tag         string
		wantRefs    int
		wantShared  bool
		wantVerdict Verdict
		wantRemove  bool
		wantAnon    bool
	}{
		{
			name: "nothing references it", repository: "redis", tag: "7",
			wantVerdict: VerdictReview, wantRemove: true,
		},
		{
			name:       "one stopped container references it",
			containers: []psLine{{ID: "c1", Names: "solo", Image: "redis:7", State: "exited", Status: "Exited (0) 2 days ago"}},
			repository: "redis", tag: "7",
			wantRefs: 1, wantVerdict: VerdictKeep,
		},
		{
			name: "two containers reference it",
			containers: []psLine{
				{ID: "c1", Names: "a", Image: "redis:7", State: "exited", Status: "Exited (0) 2 days ago"},
				{ID: "c2", Names: "b", Image: "redis:7", State: "exited", Status: "Exited (0) 2 days ago"},
			},
			repository: "redis", tag: "7",
			wantRefs: 2, wantShared: true, wantVerdict: VerdictKeep,
		},
		{
			name: "three containers reference it",
			containers: []psLine{
				{ID: "c1", Names: "a", Image: "redis:7", State: "exited", Status: "Exited (0) 2 days ago"},
				{ID: "c2", Names: "b", Image: "redis:7", State: "exited", Status: "Exited (0) 2 days ago"},
				{ID: "c3", Names: "c", Image: "redis:7", State: "running", Status: "Up 2 hours"},
			},
			repository: "redis", tag: "7",
			wantRefs: 3, wantShared: true, wantVerdict: VerdictKeep,
		},
		{
			name: "dangling and unreferenced", repository: noneRef, tag: noneRef,
			wantVerdict: VerdictDisposable, wantRemove: true, wantAnon: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshot{
				ps:     jsonLines(t, tt.containers...),
				images: jsonLines(t, imageLine{ID: "bbbbbbbbbbbb", Repository: tt.repository, Tag: tt.tag}),
				df: dfJSON(t, systemDF{Images: []imageLine{
					{ID: fullImageID("bbbbbbbbbbbb"), UniqueSize: "100MB"},
				}}),
			}
			items := mustClassify(t, snap)

			var img Item
			for _, it := range items {
				if it.Kind == KindImage {
					img = it
				}
			}
			if img.ID != "bbbbbbbbbbbb" {
				t.Fatalf("no image in %s", itemNames(items))
			}
			if img.RefCount != tt.wantRefs {
				t.Errorf("RefCount = %d, want %d", img.RefCount, tt.wantRefs)
			}
			if img.Shared != tt.wantShared {
				t.Errorf("Shared = %v, want %v", img.Shared, tt.wantShared)
			}
			if img.Verdict != tt.wantVerdict {
				t.Errorf("Verdict = %v, want %v", img.Verdict, tt.wantVerdict)
			}
			if got := img.RemoveCmd != ""; got != tt.wantRemove {
				t.Errorf("offers removal = %v (%q), want %v", got, img.RemoveCmd, tt.wantRemove)
			}
			if img.Anonymous != tt.wantAnon {
				t.Errorf("Anonymous = %v, want %v", img.Anonymous, tt.wantAnon)
			}
		})
	}
}

// A dangling image is the residue of a build whose tag a later build took
// over. Nothing can name it, so nothing can miss it.
func TestDanglingImageIsAnonymousAndDisposable(t *testing.T) {
	snap := snapshot{
		images: jsonLines(t,
			imageLine{ID: "deadbeefcafe", Repository: noneRef, Tag: noneRef, Digest: noneRef},
			// A digest-pinned image also shows an empty tag, but it still
			// has a repository and must not be mistaken for dangling.
			imageLine{ID: "feedfacefeed", Repository: "alpine", Tag: noneRef, Digest: "sha256:abc123"},
		),
		df: dfJSON(t, systemDF{Images: []imageLine{
			{ID: fullImageID("deadbeefcafe"), UniqueSize: "300MB"},
			{ID: fullImageID("feedfacefeed"), UniqueSize: "14MB"},
		}}),
	}
	items := mustClassify(t, snap)

	dangling := find(t, items, KindImage, noneRef+":"+noneRef)
	if !dangling.Anonymous {
		t.Error("Anonymous = false for a dangling image")
	}
	if dangling.Verdict != VerdictDisposable {
		t.Errorf("Verdict = %v, want Disposable", dangling.Verdict)
	}
	if dangling.RemoveCmd != "docker rmi deadbeefcafe" {
		t.Errorf("RemoveCmd = %q, want removal by ID — a dangling image has no other handle", dangling.RemoveCmd)
	}
	if dangling.Status != "Dangling, unused" {
		t.Errorf("Status = %q, want %q", dangling.Status, "Dangling, unused")
	}

	pinned := find(t, items, KindImage, "alpine@sha256:abc123")
	if pinned.Anonymous {
		t.Error("Anonymous = true for a digest-pinned image; it has a repository, so it is not dangling")
	}
	if pinned.Verdict != VerdictReview {
		t.Errorf("Verdict = %v, want Review for a named but unreferenced image", pinned.Verdict)
	}
}

// Docker lists an image once per tag. Grouping the rows back together is
// what stops a two-tag image being offered twice and, more importantly,
// having its reclaimable size counted twice in the total.
func TestMultiTagImageIsOneRowCountedOnce(t *testing.T) {
	snap := snapshot{
		images: jsonLines(t,
			imageLine{ID: "abcabcabcabc", Repository: "myapp", Tag: "latest"},
			imageLine{ID: "abcabcabcabc", Repository: "myapp", Tag: "v2"},
		),
		df: dfJSON(t, systemDF{Images: []imageLine{
			{ID: fullImageID("abcabcabcabc"), UniqueSize: "500MB"},
			{ID: fullImageID("abcabcabcabc"), UniqueSize: "500MB"},
		}}),
	}
	items := mustClassify(t, snap)

	var images []Item
	var total int64
	for _, it := range items {
		if it.Kind == KindImage {
			images = append(images, it)
			total += it.Size
		}
	}
	if len(images) != 1 {
		t.Fatalf("got %d image rows, want 1: a two-tag image is one image", len(images))
	}
	if total != 500_000_000 {
		t.Errorf("total image bytes = %d, want 500000000 counted once", total)
	}
	if images[0].Name != "myapp:latest, myapp:v2" {
		t.Errorf("Name = %q, want both tags listed", images[0].Name)
	}
	// Removing one tag of a two-tag image only untags it and frees
	// nothing, so the command has to name both.
	if images[0].RemoveCmd != "docker rmi myapp:latest myapp:v2" {
		t.Errorf("RemoveCmd = %q, want both tags named", images[0].RemoveCmd)
	}
	if !strings.Contains(images[0].Description, "2 tags") {
		t.Errorf("Description does not warn that the image carries 2 tags.\nGot: %s", images[0].Description)
	}
}

// An anonymous volume is the residue of a run of an image that declares a
// VOLUME. They accumulate one per run, and the 64-hex name is what marks
// them — but a name a human chose must never be caught by it.
func TestAnonymousVolumeDetection(t *testing.T) {
	const hex64 = "0b6dd16c827320fab207c126a1104c3ee27cbe003c749d59cc76a47835729353"
	tests := []struct {
		name     string
		volume   volumeLine
		wantAnon bool
	}{
		{
			name:     "64 hex characters",
			volume:   volumeLine{Name: hex64},
			wantAnon: true,
		},
		{
			name:     "marked by the daemon's label",
			volume:   volumeLine{Name: hex64, Labels: anonymousVolumeLabel + "="},
			wantAnon: true,
		},
		{
			name:     "compose project volume",
			volume:   volumeLine{Name: "api_db_data", Labels: composeProjectLabel + "=api"},
			wantAnon: false,
		},
		{
			name:     "hand-named volume",
			volume:   volumeLine{Name: "pgdata"},
			wantAnon: false,
		},
		{
			name:     "one character short of an ID",
			volume:   volumeLine{Name: hex64[:63]},
			wantAnon: false,
		},
		{
			name:     "hex-looking but uppercase",
			volume:   volumeLine{Name: strings.ToUpper(hex64)},
			wantAnon: false,
		},
		{
			name:     "long but not hex",
			volume:   volumeLine{Name: strings.Repeat("z", 64)},
			wantAnon: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshot{
				volumes: jsonLines(t, tt.volume),
				df: dfJSON(t, systemDF{Volumes: []volumeLine{
					{Name: tt.volume.Name, Size: "26.07MB", Links: "0"},
				}}),
			}
			v := find(t, mustClassify(t, snap), KindVolume, tt.volume.Name)

			if v.Anonymous != tt.wantAnon {
				t.Errorf("Anonymous = %v, want %v", v.Anonymous, tt.wantAnon)
			}
			// An unreferenced anonymous volume is junk; an unreferenced
			// named one is a project between runs and only ever Review,
			// because no registry can hand its data back.
			want := VerdictReview
			if tt.wantAnon {
				want = VerdictDisposable
			}
			if v.Verdict != want {
				t.Errorf("Verdict = %v, want %v", v.Verdict, want)
			}
			if v.Size != 26_070_000 {
				t.Errorf("Size = %d, want the figure docker system df reports", v.Size)
			}
		})
	}
}

// A volume a container still references cannot be removed at all — docker
// refuses, and forcing it would leave that container pointing at data that
// is gone. A stopped container counts, because starting it again has to
// find its data where it left it.
func TestReferencedVolumeIsNeverOffered(t *testing.T) {
	tests := []struct {
		name    string
		mounted bool
		links   string
	}{
		{name: "mounted by a container we inspected", mounted: true, links: "0"},
		{name: "only df knows it is referenced", mounted: false, links: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshot{
				ps: jsonLines(t, psLine{ID: "c1", Names: "api-db-1", Image: "postgres:16",
					State: "exited", Status: "Exited (0) 2 days ago", Labels: composeProjectLabel + "=api"}),
				volumes: jsonLines(t, volumeLine{Name: "api_db_data", Labels: composeProjectLabel + "=api"}),
				df: dfJSON(t, systemDF{Volumes: []volumeLine{
					{Name: "api_db_data", Size: "200.7MB", Links: tt.links},
				}}),
			}
			if tt.mounted {
				snap.inspect = jsonLines(t, containerInspect{
					ID: "c1", Name: "/api-db-1", Image: fullImageID("aaaaaaaaaaaa"),
					Mounts: []struct {
						Type        string
						Name        string
						Source      string
						Destination string
					}{{Type: "volume", Name: "api_db_data", Destination: "/var/lib/postgresql/data"}},
				})
			}

			v := find(t, mustClassify(t, snap), KindVolume, "api_db_data")
			if !v.InUse {
				t.Error("InUse = false for a volume a container references")
			}
			if v.Verdict != VerdictKeep {
				t.Errorf("Verdict = %v, want Keep", v.Verdict)
			}
			if v.RemoveCmd != "" {
				t.Errorf("RemoveCmd = %q, want none while a container references it", v.RemoveCmd)
			}
			if v.Project != "api" {
				t.Errorf("Project = %q, want %q", v.Project, "api")
			}
		})
	}
}

// The hardest rule to get wrong safely: whatever else is true of it, a
// container that is running is not junk.
func TestRunningContainerIsNeverDisposable(t *testing.T) {
	// A daemon-generated name that last did something long ago — every
	// signal this package has for "throwaway" — so only the state can be
	// keeping it out of Disposable.
	tests := []struct {
		state       string
		status      string
		wantInUse   bool
		wantVerdict Verdict
	}{
		{state: "running", status: "Up 3 weeks", wantInUse: true, wantVerdict: VerdictKeep},
		{state: "restarting", status: "Restarting (1) 5 seconds ago", wantInUse: true, wantVerdict: VerdictKeep},
		{state: "paused", status: "Up 3 weeks (Paused)", wantInUse: true, wantVerdict: VerdictKeep},
		{state: "removing", status: "Removal In Progress", wantInUse: true, wantVerdict: VerdictKeep},
		{state: "exited", status: "Exited (0) 3 weeks ago", wantInUse: false, wantVerdict: VerdictDisposable},
		{state: "dead", status: "Dead", wantInUse: false, wantVerdict: VerdictDisposable},
		// A state no version of Docker this package knows about reports:
		// say nothing rather than guess.
		{state: "hibernating", status: "Hibernating", wantInUse: false, wantVerdict: VerdictUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			snap := snapshot{
				ps: jsonLines(t, psLine{ID: "c1", Names: "nostalgic_ptolemy", Image: "busybox:latest",
					State: tt.state, Status: tt.status}),
				inspect: jsonLines(t, containerInspect{
					ID: "c1", Name: "/nostalgic_ptolemy", Image: fullImageID("cccccccccccc"),
					Created: ago(90 * 24 * time.Hour),
					State: struct {
						Status     string
						Running    bool
						StartedAt  string
						FinishedAt string
					}{Status: tt.state, Running: tt.state == "running",
						StartedAt: ago(90 * 24 * time.Hour), FinishedAt: ago(60 * 24 * time.Hour)},
				}),
			}
			c := find(t, mustClassify(t, snap), KindContainer, "nostalgic_ptolemy")

			if c.InUse != tt.wantInUse {
				t.Errorf("InUse = %v, want %v", c.InUse, tt.wantInUse)
			}
			if c.Verdict != tt.wantVerdict {
				t.Errorf("Verdict = %v, want %v", c.Verdict, tt.wantVerdict)
			}
			if c.InUse && c.Verdict == VerdictDisposable {
				t.Error("a live container was rated Disposable")
			}
			if c.InUse && c.RemoveCmd != "" {
				t.Errorf("RemoveCmd = %q for a live container; stopping it is the user's decision", c.RemoveCmd)
			}
			if c.Verdict == VerdictUnknown && c.RemoveCmd != "" {
				t.Errorf("RemoveCmd = %q for an unclassifiable container", c.RemoveCmd)
			}
		})
	}
}

// An image whose only container is running is in use too, and the same
// rule applies one level up.
func TestImageBackingARunningContainerIsInUse(t *testing.T) {
	snap := snapshot{
		ps: jsonLines(t, psLine{ID: "c1", Names: "web", Image: "nginx:1.27", State: "running", Status: "Up 2 hours"}),
		inspect: jsonLines(t, containerInspect{
			ID: "c1", Name: "/web", Image: fullImageID("dddddddddddd"),
			State: struct {
				Status     string
				Running    bool
				StartedAt  string
				FinishedAt string
			}{Status: "running", Running: true, StartedAt: ago(2 * time.Hour)},
		}),
		images: jsonLines(t, imageLine{ID: "dddddddddddd", Repository: "nginx", Tag: "1.27"}),
		df:     dfJSON(t, systemDF{Images: []imageLine{{ID: fullImageID("dddddddddddd"), UniqueSize: "60MB"}}}),
	}
	img := find(t, mustClassify(t, snap), KindImage, "nginx:1.27")

	if !img.InUse {
		t.Error("InUse = false for an image a running container was built from")
	}
	if img.Verdict != VerdictKeep {
		t.Errorf("Verdict = %v, want Keep", img.Verdict)
	}
	if img.RemoveCmd != "" {
		t.Errorf("RemoveCmd = %q, want none", img.RemoveCmd)
	}
}

// docker ps reports prose ("Exited (0) 3 weeks ago"), never a timestamp,
// so the real times come from inspect: the most recent of finished,
// started and created. A missing one must not win, and Docker's year-1
// "never happened" placeholder must not either.
func TestLastUsedPicksTheNewestTimestamp(t *testing.T) {
	const never = "0001-01-01T00:00:00Z"
	tests := []struct {
		name       string
		created    string
		started    string
		finished   string
		wantOffset time.Duration // from testNow; -1 means the zero time
	}{
		{
			name:    "finished is newest",
			created: ago(30 * 24 * time.Hour), started: ago(20 * 24 * time.Hour), finished: ago(10 * 24 * time.Hour),
			wantOffset: 10 * 24 * time.Hour,
		},
		{
			name:    "still running, so started is newest",
			created: ago(30 * 24 * time.Hour), started: ago(2 * time.Hour), finished: never,
			wantOffset: 2 * time.Hour,
		},
		{
			name:    "created but never started",
			created: ago(5 * 24 * time.Hour), started: never, finished: never,
			wantOffset: 5 * 24 * time.Hour,
		},
		{
			name:    "restarted after its last exit",
			created: ago(40 * 24 * time.Hour), started: ago(1 * time.Hour), finished: ago(3 * 24 * time.Hour),
			wantOffset: 1 * time.Hour,
		},
		{
			name:    "nothing usable at all",
			created: "", started: never, finished: "N/A",
			wantOffset: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshot{
				ps: jsonLines(t, psLine{ID: "c1", Names: "sample", Image: "busybox:latest",
					State: "exited", Status: "Exited (0) 3 weeks ago"}),
				inspect: jsonLines(t, containerInspect{
					ID: "c1", Name: "/sample", Created: tt.created,
					State: struct {
						Status     string
						Running    bool
						StartedAt  string
						FinishedAt string
					}{Status: "exited", StartedAt: tt.started, FinishedAt: tt.finished},
				}),
			}
			c := find(t, mustClassify(t, snap), KindContainer, "sample")

			if tt.wantOffset < 0 {
				if !c.LastUsed.IsZero() {
					t.Errorf("LastUsed = %v, want the zero time: no value may be invented", c.LastUsed)
				}
				return
			}
			if want := testNow.Add(-tt.wantOffset); !c.LastUsed.Equal(want) {
				t.Errorf("LastUsed = %v, want %v", c.LastUsed, want)
			}
		})
	}
}

// A daemon-invented name is the signal for a one-off run, but a container
// that stopped an hour ago may be one somebody is in the middle of, so age
// decides between residue and something to look at.
func TestGeneratedNameContainerNeedsAgeToBeDisposable(t *testing.T) {
	tests := []struct {
		name        string
		container   string
		labels      string
		idle        time.Duration
		haveInspect bool
		wantAnon    bool
		wantVerdict Verdict
	}{
		{name: "generated name, long idle", container: "nostalgic_ptolemy", idle: 60 * 24 * time.Hour,
			haveInspect: true, wantAnon: true, wantVerdict: VerdictDisposable},
		{name: "generated name, stopped an hour ago", container: "vibrant_curie", idle: time.Hour,
			haveInspect: true, wantAnon: true, wantVerdict: VerdictReview},
		{name: "generated name but no timestamps to judge by", container: "vibrant_curie",
			haveInspect: false, wantAnon: true, wantVerdict: VerdictReview},
		{name: "hand-named and long idle", container: "my_db", idle: 60 * 24 * time.Hour,
			haveInspect: true, wantAnon: false, wantVerdict: VerdictReview},
		// A generated name inside a compose project is compose's doing,
		// not a throwaway run, so the project label wins.
		{name: "generated name inside a compose project", container: "nostalgic_ptolemy",
			labels: composeProjectLabel + "=api", idle: 60 * 24 * time.Hour,
			haveInspect: true, wantAnon: false, wantVerdict: VerdictReview},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshot{
				ps: jsonLines(t, psLine{ID: "c1", Names: tt.container, Image: "busybox:latest",
					State: "exited", Status: "Exited (0) long ago", Labels: tt.labels}),
			}
			if tt.haveInspect {
				snap.inspect = jsonLines(t, containerInspect{
					ID: "c1", Name: "/" + tt.container, Created: ago(tt.idle + time.Hour),
					State: struct {
						Status     string
						Running    bool
						StartedAt  string
						FinishedAt string
					}{Status: "exited", StartedAt: ago(tt.idle + time.Hour), FinishedAt: ago(tt.idle)},
					Config: struct {
						Image  string
						Labels map[string]string
					}{Image: "busybox:latest", Labels: parseLabels(tt.labels)},
				})
			}
			c := find(t, mustClassify(t, snap), KindContainer, tt.container)

			if c.Anonymous != tt.wantAnon {
				t.Errorf("Anonymous = %v, want %v", c.Anonymous, tt.wantAnon)
			}
			if c.Verdict != tt.wantVerdict {
				t.Errorf("Verdict = %v, want %v", c.Verdict, tt.wantVerdict)
			}
			if c.RemoveCmd != "docker rm c1" {
				t.Errorf("RemoveCmd = %q, want %q for a stopped container", c.RemoveCmd, "docker rm c1")
			}
		})
	}
}

// Hundreds of build cache records with opaque IDs and no way to remove one
// individually would be noise carrying a command that cannot be written
// honestly, so they collapse into a single row.
func TestBuildCacheIsOneAggregatedRow(t *testing.T) {
	snap := snapshot{
		df: dfJSON(t, systemDF{BuildCache: []buildCacheLine{
			{ID: "a1", CacheType: "regular", Size: "1GB", InUse: "false",
				CreatedAt: "2026-06-17 07:15:48.431748818 +0000 UTC", LastUsedAt: "2026-06-18 07:15:48 +0000 UTC"},
			{ID: "a2", CacheType: "source.local", Size: "2GB", InUse: "false",
				CreatedAt: "2026-05-01 07:15:48 +0000 UTC", LastUsedAt: "2026-07-01 07:15:48 +0000 UTC"},
			{ID: "a3", CacheType: "exec.cachemount", Size: "500MB", InUse: "true",
				CreatedAt: "2026-07-01 07:15:48 +0000 UTC", LastUsedAt: "2026-08-01 07:15:48 +0000 UTC"},
		}}),
	}
	items := mustClassify(t, snap)

	var cache []Item
	for _, it := range items {
		if it.Kind == KindBuildCache {
			cache = append(cache, it)
		}
	}
	if len(cache) != 1 {
		t.Fatalf("got %d build cache rows, want exactly 1 in %s", len(cache), itemNames(items))
	}
	bc := cache[0]
	// Only what `docker builder prune` would actually free: the in-use
	// record is left alone by that command, so counting it would promise
	// space the user will not get.
	if bc.Size != 3_000_000_000 {
		t.Errorf("Size = %d, want 3000000000 (the two unused records only)", bc.Size)
	}
	if !bc.InUse {
		t.Error("InUse = false although one record is in use")
	}
	if bc.Verdict != VerdictReview {
		t.Errorf("Verdict = %v, want Review while a build is using part of it", bc.Verdict)
	}
	if bc.RemoveCmd != "docker builder prune -f" {
		t.Errorf("RemoveCmd = %q, want the only command that exists for this", bc.RemoveCmd)
	}
	if !strings.Contains(bc.Status, "3 records") {
		t.Errorf("Status = %q, want the record count", bc.Status)
	}
	// The newest use among the removable records.
	if want := time.Date(2026, 7, 1, 7, 15, 48, 0, time.UTC); !bc.LastUsed.Equal(want) {
		t.Errorf("LastUsed = %v, want %v", bc.LastUsed, want)
	}

	t.Run("nothing in use is disposable", func(t *testing.T) {
		snap := snapshot{df: dfJSON(t, systemDF{BuildCache: []buildCacheLine{
			{ID: "a1", Size: "4.693GB", InUse: "false"},
		}})}
		bc := find(t, mustClassify(t, snap), KindBuildCache, "Build cache")
		if bc.Verdict != VerdictDisposable {
			t.Errorf("Verdict = %v, want Disposable when no build is using it", bc.Verdict)
		}
		if bc.Size != 4_693_000_000 {
			t.Errorf("Size = %d, want 4693000000", bc.Size)
		}
	})

	t.Run("no records means no row", func(t *testing.T) {
		for _, it := range mustClassify(t, snapshot{df: dfJSON(t, systemDF{})}) {
			if it.Kind == KindBuildCache {
				t.Error("a build cache row was produced for a machine with no build cache")
			}
		}
	})
}

// Docker not being installed, not running, or answering with something
// unexpected are ordinary conditions on a developer machine. None of them
// may take the app down.
func TestMalformedInputDoesNotPanic(t *testing.T) {
	garbage := [][]byte{
		nil,
		[]byte(""),
		[]byte("   \n\n  "),
		[]byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock."),
		[]byte("{"),
		[]byte("null"),
		[]byte("[]"),
		[]byte(`{"ID":`),
		[]byte("\x00\x01\x02 binary nonsense"),
		[]byte(`{"ID":12345,"Names":true}`),
		[]byte(strings.Repeat(`{"ID":"a"}`+"\n", 100)),
	}

	for _, ps := range garbage {
		for _, other := range garbage {
			snap := snapshot{ps: ps, images: other, volumes: other, inspect: other, volInspect: other, df: other}
			items, err := classify(snap, testNow)
			if err != nil {
				continue // reporting garbage as an error is the correct outcome
			}
			// Whatever survived has to be internally consistent, because
			// the UI acts on these fields.
			for _, it := range items {
				if it.InUse && it.Verdict == VerdictDisposable {
					t.Errorf("in-use item %q rated Disposable from input %q", it.Name, ps)
				}
				if it.InUse && it.RemoveCmd != "" {
					t.Errorf("in-use item %q offered %q from input %q", it.Name, it.RemoveCmd, ps)
				}
			}
		}
	}
}

// Nothing this package produces may be a command that removes more than
// the row it belongs to. A stray `docker system prune -a` in a
// confirmation dialog is the worst bug this app could have.
func TestRemoveCommandsAreNarrowAndReadOnlyToProduce(t *testing.T) {
	snap := richSnapshot(t)
	items := mustClassify(t, snap)

	allowed := []string{"docker rm ", "docker rmi ", "docker volume rm ", "docker builder prune -f"}
	forbidden := []string{"system prune", "-a", "--all", "--volumes", "rm -rf", "&&", ";", "|"}

	var offered int
	for _, it := range items {
		if it.RemoveCmd == "" {
			continue
		}
		offered++
		var ok bool
		for _, prefix := range allowed {
			if strings.HasPrefix(it.RemoveCmd, prefix) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s %q: RemoveCmd = %q, which is not one of the narrow removal commands", it.Kind, it.Name, it.RemoveCmd)
		}
		for _, bad := range forbidden {
			if strings.Contains(it.RemoveCmd, bad) {
				t.Errorf("%s %q: RemoveCmd = %q contains %q", it.Kind, it.Name, it.RemoveCmd, bad)
			}
		}
		if it.InUse {
			t.Errorf("%s %q is in use but offers %q", it.Kind, it.Name, it.RemoveCmd)
		}
		if it.Verdict == VerdictUnknown {
			t.Errorf("%s %q is unclassified but offers %q", it.Kind, it.Name, it.RemoveCmd)
		}
	}
	if offered == 0 {
		t.Fatal("test setup: the fixture offered no removals at all, so nothing was checked")
	}
}

// Every row has to be able to explain itself, since a verdict the user
// cannot check is a verdict they should not act on.
func TestEveryItemExplainsItself(t *testing.T) {
	for _, it := range mustClassify(t, richSnapshot(t)) {
		if it.Description == "" {
			t.Errorf("%s %q has no description", it.Kind, it.Name)
		}
		if it.Effects == "" {
			t.Errorf("%s %q does not say what removing it would do", it.Kind, it.Name)
		}
		if it.Name == "" {
			t.Errorf("%s has no name", it.Kind)
		}
		if it.Status == "" {
			t.Errorf("%s %q has no status line", it.Kind, it.Name)
		}
		if it.Kind != KindImage && (it.RefCount != 0 || it.Shared) {
			t.Errorf("%s %q carries RefCount %d/Shared %v; those are for images only",
				it.Kind, it.Name, it.RefCount, it.Shared)
		}
		if it.Size < 0 {
			t.Errorf("%s %q has a negative size %d", it.Kind, it.Name, it.Size)
		}
	}
}

// Sizes are the reclaimable figure, not the apparent one, and when Docker
// declines to compute them the prose has to say so rather than let a row
// read as empty.
func TestMissingSizesAreAdmittedRatherThanGuessed(t *testing.T) {
	snap := snapshot{
		// The plain listings report N/A for anything that would cost a
		// directory walk, and this is a scan where df did not answer.
		images:      jsonLines(t, imageLine{ID: "eeeeeeeeeeee", Repository: "redis", Tag: "7", Size: "466MB", UniqueSize: "N/A"}),
		volumes:     jsonLines(t, volumeLine{Name: "pgdata", Size: "N/A", Links: "N/A"}),
		sizesFailed: true,
	}
	items := mustClassify(t, snap)

	for _, it := range items {
		if it.Size != 0 {
			t.Errorf("%s %q reported size %d although docker system df did not answer", it.Kind, it.Name, it.Size)
		}
		if !strings.Contains(it.Description, "Sizes are unavailable") {
			t.Errorf("%s %q does not admit that its size is unknown.\nGot: %s", it.Kind, it.Name, it.Description)
		}
	}
}

// When docker inspect does not answer there are no real timestamps, so
// nothing can be judged abandoned. The scan gets more cautious rather than
// merely less informative, and says which it is.
func TestMissingInspectMakesTheScanCautious(t *testing.T) {
	snap := snapshot{
		ps: jsonLines(t, psLine{ID: "c1", Names: "nostalgic_ptolemy", Image: "busybox:latest",
			State: "exited", Status: "Exited (0) 8 months ago"}),
		detailsFailed: true,
	}
	c := find(t, mustClassify(t, snap), KindContainer, "nostalgic_ptolemy")

	if !c.LastUsed.IsZero() {
		t.Errorf("LastUsed = %v, want the zero time when inspect did not answer", c.LastUsed)
	}
	if c.Verdict == VerdictDisposable {
		t.Error("Verdict = Disposable although nothing was known about the container's age")
	}
	if !strings.Contains(c.Description, "Timestamps are unavailable") {
		t.Errorf("Description does not admit the missing timestamps.\nGot: %s", c.Description)
	}
}

// A machine with Docker installed and nothing in it is a normal machine,
// not an error.
func TestEmptyMachineIsNotAnError(t *testing.T) {
	items, err := classify(snapshot{
		ps: []byte(""), images: []byte(""), volumes: []byte(""),
		df: []byte(`{"Images":[],"Containers":[],"Volumes":[],"BuildCache":[]}`),
	}, testNow)
	if err != nil {
		t.Fatalf("classify on an empty machine: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %s, want nothing", itemNames(items))
	}
}

// Items come back grouped by kind and biggest-first inside each group.
func TestItemsAreGroupedByKindThenSize(t *testing.T) {
	items := mustClassify(t, richSnapshot(t))
	if len(items) < 4 {
		t.Fatalf("test setup: only %d items in the fixture", len(items))
	}
	for i := 1; i < len(items); i++ {
		prev, cur := items[i-1], items[i]
		if cur.Kind < prev.Kind {
			t.Fatalf("item %d (%s) comes after %s: kinds must be grouped", i, cur.Kind, prev.Kind)
		}
		if cur.Kind == prev.Kind && cur.Size > prev.Size {
			t.Errorf("%s %q (%d bytes) comes after %q (%d bytes): biggest first within a kind",
				cur.Kind, cur.Name, cur.Size, prev.Name, prev.Size)
		}
	}
}

// richSnapshot is a machine with one of everything this package
// distinguishes: a running compose container, an abandoned one-off, a
// shared base image, a dangling image, an anonymous volume, a named
// project volume, and a pile of build cache.
func richSnapshot(t *testing.T) snapshot {
	t.Helper()
	const anonVol = "0b6dd16c827320fab207c126a1104c3ee27cbe003c749d59cc76a47835729353"

	mounts := func(name string) []struct {
		Type        string
		Name        string
		Source      string
		Destination string
	} {
		return []struct {
			Type        string
			Name        string
			Source      string
			Destination string
		}{{Type: "volume", Name: name, Destination: "/data"}}
	}
	state := func(status string, running bool, started, finished string) struct {
		Status     string
		Running    bool
		StartedAt  string
		FinishedAt string
	} {
		return struct {
			Status     string
			Running    bool
			StartedAt  string
			FinishedAt string
		}{Status: status, Running: running, StartedAt: started, FinishedAt: finished}
	}
	config := func(image string, labels map[string]string) struct {
		Image  string
		Labels map[string]string
	} {
		return struct {
			Image  string
			Labels map[string]string
		}{Image: image, Labels: labels}
	}

	return snapshot{
		ps: jsonLines(t,
			psLine{ID: "c1", Names: "api-db-1", Image: "postgres:16", State: "running",
				Status: "Up 2 hours", Labels: composeProjectLabel + "=api", Size: "12.4MB (virtual 400MB)"},
			psLine{ID: "c2", Names: "billing-db-1", Image: "postgres:16", State: "exited",
				Status: "Exited (0) 3 weeks ago", Labels: composeProjectLabel + "=billing"},
			psLine{ID: "c3", Names: "nostalgic_ptolemy", Image: "busybox:latest", State: "exited",
				Status: "Exited (0) 8 months ago"},
		),
		inspect: jsonLines(t,
			containerInspect{ID: "c1", Name: "/api-db-1", Created: ago(90 * 24 * time.Hour),
				Image:  fullImageID("aaaaaaaaaaaa"),
				State:  state("running", true, ago(2*time.Hour), "0001-01-01T00:00:00Z"),
				Config: config("postgres:16", map[string]string{composeProjectLabel: "api"}),
				Mounts: mounts("api_db_data")},
			containerInspect{ID: "c2", Name: "/billing-db-1", Created: ago(120 * 24 * time.Hour),
				Image:  fullImageID("aaaaaaaaaaaa"),
				State:  state("exited", false, ago(40*24*time.Hour), ago(21*24*time.Hour)),
				Config: config("postgres:16", map[string]string{composeProjectLabel: "billing"})},
			containerInspect{ID: "c3", Name: "/nostalgic_ptolemy", Created: ago(250 * 24 * time.Hour),
				Image:  fullImageID("cccccccccccc"),
				State:  state("exited", false, ago(250*24*time.Hour), ago(240*24*time.Hour)),
				Config: config("busybox:latest", nil)},
		),
		images: jsonLines(t,
			imageLine{ID: "aaaaaaaaaaaa", Repository: "postgres", Tag: "16"},
			imageLine{ID: "cccccccccccc", Repository: "busybox", Tag: "latest"},
			imageLine{ID: "deadbeefcafe", Repository: noneRef, Tag: noneRef, Digest: noneRef},
			imageLine{ID: "ffffffffffff", Repository: "golangci/golangci-lint", Tag: "v2.10"},
		),
		volumes: jsonLines(t,
			volumeLine{Name: "api_db_data", Labels: composeProjectLabel + "=api"},
			volumeLine{Name: anonVol, Labels: anonymousVolumeLabel + "="},
			volumeLine{Name: "orphan_cache"},
		),
		volInspect: jsonLines(t,
			volumeInspect{Name: "api_db_data", CreatedAt: ago(90 * 24 * time.Hour)},
			volumeInspect{Name: anonVol, CreatedAt: ago(200 * 24 * time.Hour)},
		),
		df: dfJSON(t, systemDF{
			Images: []imageLine{
				{ID: fullImageID("aaaaaaaaaaaa"), Size: "1.2GB", UniqueSize: "400MB"},
				{ID: fullImageID("cccccccccccc"), Size: "6.14MB", UniqueSize: "1.919MB"},
				{ID: fullImageID("deadbeefcafe"), Size: "300MB", UniqueSize: "300MB"},
				{ID: fullImageID("ffffffffffff"), Size: "1.34GB", UniqueSize: "1.344GB"},
			},
			Containers: []psLine{{ID: "c1", Size: "12.4MB (virtual 400MB)"}, {ID: "c3", Size: "2MB (virtual 8MB)"}},
			Volumes: []volumeLine{
				{Name: "api_db_data", Size: "200.7MB", Links: "1"},
				{Name: anonVol, Size: "26.07MB", Links: "0"},
				{Name: "orphan_cache", Size: "1.5GB", Links: "0"},
			},
			BuildCache: []buildCacheLine{
				{ID: "b1", Size: "4.693GB", InUse: "false", CreatedAt: ago(60 * 24 * time.Hour),
					LastUsedAt: ago(30 * 24 * time.Hour)},
			},
		}),
	}
}
