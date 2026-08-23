package docker

import (
	"context"
	"os"
	"testing"
	"time"
)

// The tests in this file are the only ones that talk to a real daemon, and
// they are skipped unless MACCLEANER_DOCKER_LIVE_TEST is set. Everything
// that decides what the user sees is a pure function tested against
// recorded output; these exist to catch the other half of the problem —
// Docker changing the shape of what it prints. Run them by hand after a
// Docker upgrade:
//
//	MACCLEANER_DOCKER_LIVE_TEST=1 go test ./internal/docker/ -run Live -v
func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("MACCLEANER_DOCKER_LIVE_TEST") == "" {
		t.Skip("set MACCLEANER_DOCKER_LIVE_TEST=1 to run against the local Docker daemon")
	}
	if ok, reason := Available(); !ok {
		t.Skipf("docker is not available: %s", reason)
	}
}

// TestLiveScan checks that a real daemon's output still parses into
// something coherent: sizes that add up, verdicts that never contradict
// InUse, and no removal offered for anything in use.
func TestLiveScan(t *testing.T) {
	skipUnlessLive(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	items, err := Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(items) == 0 {
		t.Skip("nothing on this machine to classify")
	}

	byKind := map[Kind]int64{}
	counts := map[Kind]int{}
	for _, it := range items {
		byKind[it.Kind] += it.Size
		counts[it.Kind]++

		if it.InUse && it.Verdict == VerdictDisposable {
			t.Errorf("%s %q is in use but rated Disposable", it.Kind, it.Name)
		}
		if it.InUse && it.RemoveCmd != "" {
			t.Errorf("%s %q is in use but offers %q", it.Kind, it.Name, it.RemoveCmd)
		}
		if it.Shared && it.Verdict != VerdictKeep {
			t.Errorf("shared image %q rated %v, want Keep", it.Name, it.Verdict)
		}
		if it.Shared != (it.RefCount > 1) {
			t.Errorf("%s %q: Shared = %v but RefCount = %d", it.Kind, it.Name, it.Shared, it.RefCount)
		}
		if it.Kind != KindImage && it.RefCount != 0 {
			t.Errorf("%s %q carries RefCount %d; that field is for images", it.Kind, it.Name, it.RefCount)
		}
		if it.Description == "" || it.Effects == "" {
			t.Errorf("%s %q cannot explain itself", it.Kind, it.Name)
		}
		if !it.LastUsed.IsZero() && it.LastUsed.After(time.Now().Add(time.Hour)) {
			t.Errorf("%s %q claims to have been used in the future: %v", it.Kind, it.Name, it.LastUsed)
		}
	}

	// Compare these against `docker system df` by hand: the image total
	// must equal its images RECLAIMABLE figure, the volume total its
	// volumes SIZE, and the build cache total its build cache SIZE.
	for _, k := range []Kind{KindContainer, KindImage, KindVolume, KindBuildCache} {
		t.Logf("%-12s %3d item(s) %14s", k, counts[k], humanSize(byKind[k]))
	}
	for _, it := range items {
		last := "-"
		if !it.LastUsed.IsZero() {
			last = it.LastUsed.Format(time.DateOnly)
		}
		t.Logf("%-11s %-10s refs=%d %-10s %10s  %-40s %s",
			it.Kind, it.Verdict, it.RefCount, last, humanSize(it.Size), truncate(it.Name, 40), it.Status)
	}
}

// TestLiveDetails prints the detail panel for every object on the machine
// and times the scan.
//
// It is a reading exercise as much as a test: the assertions only catch a
// panel that cannot say anything at all, because whether a detail is
// *useful* is a judgement no assertion makes. Run it after changing
// anything in details.go, content.go or history.go and read the output.
func TestLiveDetails(t *testing.T) {
	skipUnlessLive(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	items, err := Scan(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("scanned %d object(s) in %s", len(items), elapsed.Round(time.Millisecond))
	// The UI allows 90 seconds; a scan anywhere near that on a normal
	// machine means the enrichment has stopped being affordable.
	if elapsed > 30*time.Second {
		t.Errorf("scan took %s, which is too close to the UI's 90s budget", elapsed)
	}

	for _, it := range items {
		sections := it.Details()
		if len(sections) == 0 {
			t.Errorf("%s %q has no details at all", it.Kind, it.Name)
			continue
		}
		t.Logf("\n=== %s %s (%s, %s)", it.Kind, it.Name, it.Verdict, humanSize(it.Size))
		for _, s := range sections {
			if s.Title == "" {
				t.Errorf("%s %q has an untitled section", it.Kind, it.Name)
			}
			if len(s.Rows) == 0 && len(s.Lines) == 0 {
				t.Errorf("%s %q section %q is empty", it.Kind, it.Name, s.Title)
			}
			t.Logf("  %s", s.Title)
			for _, r := range s.Rows {
				t.Logf("    %-24s %s", r.Label, r.Value)
			}
			for _, l := range s.Lines {
				t.Logf("    %s", l)
			}
		}
	}
}

// TestLiveAvailable checks the daemon probe against whatever is installed.
func TestLiveAvailable(t *testing.T) {
	skipUnlessLive(t)

	ok, reason := Available()
	if !ok {
		t.Fatalf("Available() = false: %s", reason)
	}
	if reason != "" {
		t.Errorf("Available() returned a reason %q alongside success", reason)
	}
}
