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
