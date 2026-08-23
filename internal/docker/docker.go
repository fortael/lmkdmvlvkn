// Package docker reports what Docker is storing on this machine —
// containers, images, volumes and the build cache — and rates each object
// by how much removing it would actually cost.
//
// The rating is the point of the package. Disk-usage tools tend to sort by
// size and stop there, which on a developer machine recommends deleting
// exactly the images that several projects share and leaves the genuine
// litter alone. So the classification here is built around two questions
// that size cannot answer: how many containers reference this image, and
// did anybody ever name this thing. An image two compose projects both
// build on is infrastructure, however large; an untagged image left over
// by a superseded build and a 64-hex-character volume from a one-off
// `docker run` are litter, however small.
//
// Everything is derived from read-only docker subcommands — ps, images,
// volume ls, inspect, system df. The package never removes anything. Each
// Item carries the literal command that would remove it, as a string, for
// the UI to show and to run only after the user has agreed to it.
//
// Reported sizes are the *reclaimable* figure rather than the apparent
// one. For images that means the layers unique to the image, since layers
// shared with another image survive its deletion; summing this package's
// image sizes reproduces the "RECLAIMABLE" total `docker system df`
// prints. Where Docker declines to compute a size, the size is 0 and the
// description says so, rather than a guess.
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Kind is what sort of Docker object an Item describes.
type Kind int

const (
	KindContainer Kind = iota
	KindImage
	KindVolume
	KindBuildCache
)

// String returns the singular noun for the kind, as the UI's TYPE column
// shows it.
func (k Kind) String() string {
	switch k {
	case KindContainer:
		return "Container"
	case KindImage:
		return "Image"
	case KindVolume:
		return "Volume"
	case KindBuildCache:
		return "Build cache"
	default:
		return "Unknown"
	}
}

// Verdict is how worth keeping an object is.
//
// The scale deliberately mirrors knowledge.Score's 0-3 shape, so the UI
// can render a Docker object with the same star widget it renders a cache
// folder with, and so the zero value means "we could not tell" in both
// packages rather than accidentally meaning "safe".
type Verdict int

const (
	// VerdictUnknown means the object could not be classified — Docker
	// reported a state this package does not recognise, or the data
	// needed to judge it was unavailable. Never offer removal for one.
	VerdictUnknown Verdict = 0
	// VerdictKeep means the object is in use or shared, and removing it
	// costs you something real: a container that is running, an image
	// several projects are built on, a volume holding a project's data.
	VerdictKeep Verdict = 1
	// VerdictReview means the object is probably disposable but not
	// provably so — a tagged image no container currently references
	// looks unused whether it is genuinely forgotten or belongs to a
	// compose project that simply is not up right now.
	VerdictReview Verdict = 2
	// VerdictDisposable means the object is residue from a one-off run:
	// nothing references it, and nothing named it.
	VerdictDisposable Verdict = 3
)

// String returns the verdict's label for the UI.
func (v Verdict) String() string {
	switch v {
	case VerdictKeep:
		return "Keep"
	case VerdictReview:
		return "Review"
	case VerdictDisposable:
		return "Disposable"
	default:
		return "Unknown"
	}
}

// Item is one Docker object, already classified.
type Item struct {
	Kind Kind
	// ID is the short identifier as Docker prints it in its own tables.
	// It is empty for the aggregated build-cache row, which stands for
	// many records rather than one.
	ID string
	// Name is what to show: the image reference (all of its tags when it
	// has several), the container name, the volume name.
	Name string
	// Size is how many bytes removing this object would actually
	// reclaim, or 0 when Docker would not tell us — in which case the
	// Description says so instead of guessing.
	Size int64
	// Created is when the object came into existence.
	Created time.Time
	// LastUsed is the most recent moment the object did anything. It is
	// the zero time when Docker does not report one, which the UI renders
	// as "-"; images and volumes borrow it from the newest container that
	// references them, because Docker tracks no such time for them.
	LastUsed time.Time
	// InUse means something live depends on this: a running container, an
	// image a running container was built from, a volume a container
	// still references. An InUse object is never rated disposable.
	InUse bool
	// Anonymous means nobody named this — a dangling image, an anonymous
	// volume, or a container carrying a name the daemon invented.
	Anonymous bool
	// RefCount is, for images, how many containers reference the image.
	// It is 0 for every other kind.
	RefCount int
	// Shared is RefCount > 1: more than one container, and in practice
	// often more than one compose project, is built on this image.
	Shared bool
	// Project is the docker compose project the object belongs to, from
	// the com.docker.compose.project label, or "" when there is none. An
	// image shared by several projects leaves this empty and names them
	// in the prose instead, since the image itself carries no such label.
	Project string
	// Status is a short status line, e.g. "Exited (0) 3 weeks ago" for a
	// container or "Used by 3 containers in 2 compose projects" for an
	// image.
	Status  string
	Verdict Verdict
	// Description explains what the object is and where it came from.
	Description string
	// Effects explains what actually happens if it is removed: what is
	// lost, what survives, and what has to be re-downloaded.
	Effects string
	// RemoveCmd is the literal docker command that removes just this
	// object, shown to the user before anything happens. It is empty when
	// this package will not offer removal at all — because the object is
	// in use, because Docker would refuse, or because we could not
	// classify it.
	RemoveCmd string

	// The four fields below carry everything the detail panel shows, and
	// exactly one of them is set per item — whichever matches Kind. They
	// are pointers so that "Docker would not tell us" is distinguishable
	// from "Docker said nothing was there": a nil Image means the inspect
	// failed, and the panel says so instead of drawing an empty table.
	//
	// Read them through Details rather than directly unless you want to
	// lay the facts out yourself; Details is where the ordering and the
	// wording live.
	Image      *ImageInfo
	Volume     *VolumeInfo
	Container  *ContainerInfo
	BuildCache *BuildCacheInfo
}

// CanRemove reports whether the UI should offer a remove action for the
// item, mirroring knowledge.Entry.CanClean.
func (i Item) CanRemove() bool {
	return i.RemoveCmd != "" && i.Verdict >= VerdictKeep && !i.InUse
}

// dockerCandidates are the paths checked when "docker" is not on PATH.
//
// A macOS app launched from Finder inherits launchd's minimal PATH —
// /usr/bin:/bin:/usr/sbin:/sbin — not the one the user's shell profile
// builds. Every common Docker install puts its CLI somewhere that PATH
// does not contain, so a plain LookPath would tell a user with a perfectly
// healthy Docker that it is not installed.
var dockerCandidates = []string{
	"/usr/local/bin/docker",
	"/opt/homebrew/bin/docker",
	"/Applications/Docker.app/Contents/Resources/bin/docker",
}

// dockerPath resolves the docker CLI, falling back to the known install
// locations and finally to OrbStack's per-user bin directory.
func dockerPath() (string, error) {
	if p, err := exec.LookPath("docker"); err == nil {
		return p, nil
	}
	candidates := dockerCandidates
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(slices.Clone(candidates), filepath.Join(home, ".orbstack", "bin", "docker"))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("docker CLI not found")
}

// run executes one docker subcommand and returns its stdout.
//
// ctx bounds the subprocess itself, not just the wait: `docker system df
// -v` walks every volume on the machine and can take seconds, and a
// cancelled scan has to actually stop it rather than leave it running
// behind a returned error.
func run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			return stdout.Bytes(), fmt.Errorf("docker %s: %s", args[0], truncate(msg, 200))
		}
		return stdout.Bytes(), fmt.Errorf("docker %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

// availableTimeout bounds the daemon liveness check. It is short on
// purpose: the UI asks this question in order to decide whether to draw a
// "Docker is not running" placeholder, and a user whose daemon is down
// should get that answer immediately rather than watch a spinner.
const availableTimeout = 5 * time.Second

// Available reports whether the docker CLI exists and its daemon answers,
// with a short human-readable reason when it does not.
func Available() (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), availableTimeout)
	defer cancel()
	return AvailableContext(ctx)
}

// AvailableContext is Available bounded by a caller-supplied context, for
// a UI that already has one.
func AvailableContext(ctx context.Context) (bool, string) {
	bin, err := dockerPath()
	if err != nil {
		return false, "The docker command is not installed, or is not on this app's PATH."
	}
	// `docker version` is the cheapest command that has to talk to the
	// daemon: with the daemon down it still prints the client half, but
	// it exits non-zero, which is the signal being read here.
	if _, err := run(ctx, bin, "version", "--format", "{{json .Server}}"); err != nil {
		return false, daemonReason(err.Error())
	}
	return true, ""
}

// daemonReason turns a failed `docker version` into a sentence explaining
// what the user has to do about it.
func daemonReason(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "cannot connect to the docker daemon"),
		strings.Contains(lower, "is the docker daemon running"),
		strings.Contains(lower, "no such file or directory"),
		strings.Contains(lower, "connection refused"):
		return "Docker is installed, but its daemon is not answering. Start Docker Desktop or OrbStack and try again."
	case strings.Contains(lower, "permission denied"):
		return "Docker is installed, but this user is not allowed to use its socket."
	case strings.Contains(lower, "context deadline exceeded"), strings.Contains(lower, "signal: killed"):
		return "Docker did not answer in time. It may still be starting up."
	default:
		return "Docker did not answer: " + truncate(firstLine(msg), 120)
	}
}

// snapshot is everything one scan fetched, kept as raw bytes so that
// parsing and classification stay pure functions the tests can drive with
// recorded output.
//
// The three listings are required; everything else is enrichment. A scan
// that loses df still lists every object, just without trustworthy sizes,
// which is far more useful than refusing to show anything — and the flags
// below let the prose admit which numbers are missing instead of
// presenting a fabricated 0 as fact.
type snapshot struct {
	ps      []byte
	images  []byte
	volumes []byte

	inspect    []byte
	volInspect []byte
	imgInspect []byte
	df         []byte

	// history is `docker history --no-trunc` per image, keyed by short ID.
	// It cannot be batched: the subcommand takes exactly one image.
	history map[string][]byte

	// probes is a shallow read of each volume's contents, keyed by volume
	// name. It is the only part of a scan that touches the filesystem
	// rather than the daemon.
	probes map[string]volumeProbe

	sizesFailed   bool
	detailsFailed bool
}

// inspectBatch is how many IDs are passed to one `docker inspect`.
// Unbounded argument lists eventually hit the operating system's limit; a
// machine with thousands of stale containers is exactly the machine this
// app is for, so the call is chunked rather than assumed to fit.
const inspectBatch = 100

// fetchWorkers bounds how many docker subprocesses run at once.
//
// The detail this package now shows costs one `docker history` per image,
// and a machine with a hundred images would spend most of a scan waiting
// for processes to start if they ran one at a time. Six is enough to hide
// the per-process latency without turning a scan into a fork bomb on the
// four-core laptops this app is aimed at.
const fetchWorkers = 6

// historyLimit caps how many images get their build history fetched.
//
// Images are sorted biggest-first before this is applied, so the cap only
// ever costs the detail panel of images too small to be worth cleaning. A
// machine with several hundred images would otherwise spend the whole scan
// budget on histories nobody will open.
const historyLimit = 60

// Scan queries Docker and returns every object worth showing, already
// classified. It runs only read-only subcommands, and ctx bounds all of
// them.
func Scan(ctx context.Context) ([]Item, error) {
	snap, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	return classify(snap, time.Now())
}

// fetch runs the read-only docker commands one scan needs.
//
// It is deliberately two phases rather than one straight line. The three
// listings and `system df` depend on nothing, so they run together; the
// inspects and histories depend on the IDs those listings produce, so they
// run together afterwards. On a machine with a dozen images that turns
// twenty sequential subprocesses into two rounds of parallel ones.
func fetch(ctx context.Context) (snapshot, error) {
	bin, err := dockerPath()
	if err != nil {
		return snapshot{}, errors.New("the docker command is not installed, or is not on this app's PATH")
	}

	// Each task below writes only its own field and its own error, which
	// is what makes running them together safe without a lock.
	var (
		snap                        snapshot
		psErr, imagesErr, volumeErr error
	)
	runParallel(fetchWorkers, []func(){
		func() { snap.ps, psErr = run(ctx, bin, "ps", "-a", "--format", "{{json .}}") },
		func() { snap.images, imagesErr = run(ctx, bin, "images", "--format", "{{json .}}") },
		func() { snap.volumes, volumeErr = run(ctx, bin, "volume", "ls", "--format", "{{json .}}") },
		func() {
			out, dfErr := run(ctx, bin, "system", "df", "-v", "--format", "{{json .}}")
			if dfErr != nil {
				snap.df, snap.sizesFailed = nil, true
				return
			}
			snap.df = out
		},
	})
	// The listings are the scan; without one of them there is nothing
	// truthful to show, so their failure is fatal where df's is not.
	for _, err := range []error{psErr, imagesErr, volumeErr} {
		if err != nil {
			return snapshot{}, err
		}
	}

	// From here on a failure degrades the result instead of failing the
	// scan. Sizes, timestamps and provenance are worth a lot, but not
	// worth showing the user nothing at all when the listings succeeded.
	var (
		containerIDs = idsFrom(snap.ps)
		volumeNames  = namesFrom(snap.volumes)
		imageIDs     = imageIDsFrom(snap.images)
		mountpoints  = mountpointsFrom(snap.volumes)
		tasks        []func()
		historyMu    sync.Mutex
		probeMu      sync.Mutex
	)
	snap.history = make(map[string][]byte, len(imageIDs))
	snap.probes = make(map[string]volumeProbe, len(volumeNames))

	if len(containerIDs) > 0 {
		tasks = append(tasks, func() {
			out, inspectErr := runBatched(ctx, bin,
				[]string{"inspect", "--type", "container", "--format", "{{json .}}"}, containerIDs)
			snap.inspect, snap.detailsFailed = out, inspectErr != nil
		})
	}
	if len(volumeNames) > 0 {
		tasks = append(tasks, func() {
			// A missing volume inspect costs only creation timestamps,
			// which nothing is classified on, so its failure is not even
			// recorded.
			snap.volInspect, _ = runBatched(ctx, bin, []string{"volume", "inspect", "--format", "{{json .}}"}, volumeNames)
		})
	}
	if len(imageIDs) > 0 {
		tasks = append(tasks, func() {
			snap.imgInspect, _ = runBatched(ctx, bin, []string{"image", "inspect", "--format", "{{json .}}"}, imageIDs)
		})
		for _, id := range imageIDs[:min(len(imageIDs), historyLimit)] {
			tasks = append(tasks, func() {
				out, historyErr := run(ctx, bin, "history", "--no-trunc", "--format", "{{json .}}", id)
				if historyErr != nil {
					return
				}
				historyMu.Lock()
				snap.history[id] = out
				historyMu.Unlock()
			})
		}
	}
	for _, name := range volumeNames {
		tasks = append(tasks, func() {
			probe := probeVolume(ctx, mountpoints[name], name)
			probeMu.Lock()
			snap.probes[name] = probe
			probeMu.Unlock()
		})
	}
	runParallel(fetchWorkers, tasks)
	return snap, nil
}

// runParallel runs every task with at most limit of them in flight, and
// returns once they have all finished. Each task writes only its own part
// of the snapshot, so there is nothing to synchronise beyond the wait —
// except the two maps, which their tasks lock for themselves.
func runParallel(limit int, tasks []func()) {
	if len(tasks) == 0 {
		return
	}
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			task()
		}()
	}
	wg.Wait()
}

// runBatched runs one command over args in chunks, concatenating stdout.
// A chunk that fails is skipped: `docker inspect` errors out entirely if a
// single ID vanished between listing and inspecting (a container that
// exited and was auto-removed mid-scan), and one such race should not cost
// the details of every other container.
func runBatched(ctx context.Context, bin string, base, args []string) ([]byte, error) {
	var out bytes.Buffer
	var firstErr error
	for start := 0; start < len(args); start += inspectBatch {
		end := min(start+inspectBatch, len(args))
		chunk, err := run(ctx, bin, append(slices.Clone(base), args[start:end]...)...)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		out.Write(chunk)
	}
	if out.Len() == 0 {
		return nil, firstErr
	}
	return out.Bytes(), nil
}

// idsFrom pulls the container IDs out of already-fetched `docker ps`
// output, so the inspect call can be built without parsing twice at the
// call site.
func idsFrom(psOut []byte) []string {
	lines, err := decodeLines[psLine](psOut)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		if id := strings.TrimSpace(l.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// namesFrom pulls the volume names out of already-fetched `docker volume
// ls` output.
func namesFrom(volOut []byte) []string {
	lines, err := decodeLines[volumeLine](volOut)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		if n := strings.TrimSpace(l.Name); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// mountpointsFrom maps each volume name to the mountpoint the listing
// reports, so the contents probe does not have to wait for `docker volume
// inspect` to finish before it can start.
func mountpointsFrom(volOut []byte) map[string]string {
	lines, err := decodeLines[volumeLine](volOut)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(lines))
	for _, l := range lines {
		if n := strings.TrimSpace(l.Name); n != "" {
			out[n] = strings.TrimSpace(l.Mountpoint)
		}
	}
	return out
}

// imageIDsFrom pulls the distinct image IDs out of already-fetched `docker
// images` output, biggest first.
//
// The order matters because historyLimit truncates this list: an image is
// worth a subprocess in proportion to how much space it takes, so if
// anything has to be left without a build history it should be the 6 MB
// one rather than the 1.3 GB one. Distinctness matters because Docker
// prints an image once per tag it carries, and fetching the same history
// three times would be three subprocesses wasted.
func imageIDsFrom(imagesOut []byte) []string {
	lines, err := decodeLines[imageLine](imagesOut)
	if err != nil {
		return nil
	}
	type sized struct {
		id   string
		size int64
	}
	var (
		out  []sized
		seen = make(map[string]bool, len(lines))
	)
	for _, l := range lines {
		id := shortID(l.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		size, _ := parseSize(l.Size)
		out = append(out, sized{id: id, size: size})
	}
	slices.SortStableFunc(out, func(a, b sized) int {
		switch {
		case a.size > b.size:
			return -1
		case a.size < b.size:
			return 1
		default:
			return 0
		}
	})
	ids := make([]string, 0, len(out))
	for _, s := range out {
		ids = append(ids, s.id)
	}
	return ids
}
