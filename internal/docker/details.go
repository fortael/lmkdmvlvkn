package docker

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// This file defines the detail panel: everything the Docker tab shows about
// one object once it is selected, and the shape the UI renders it in.
//
// The design constraint is that `docker inspect` already exists. A tab that
// re-prints inspect's JSON in nicer colours is worth nothing. What a
// developer actually wants to know about a 64-hex-named volume or a 1.3 GB
// image is where it came from, which project it belonged to, what is inside
// it, and therefore whether throwing it away costs anything. So the
// ordering of the sections below is the whole design: provenance first,
// contents second, bookkeeping last.

// DetailRow is one label/value fact about an object.
type DetailRow struct {
	Label string
	Value string
}

// DetailSection is a titled block in the Docker detail panel. Rows render
// as an aligned label/value table; Lines render verbatim (use for layer
// commands, mount paths, env — anything that is already formatted).
type DetailSection struct {
	Title string
	Rows  []DetailRow
	Lines []string
}

// Layer is one step of an image's build history, as `docker history
// --no-trunc` reports it.
//
// Command is cleaned up for reading rather than quoted verbatim: Docker
// records a RUN as "/bin/sh -c #(nop) ..." or "RUN /bin/sh -c ... #
// buildkit" depending on which builder produced the image, and neither
// spelling is what anybody wrote in the Dockerfile.
type Layer struct {
	// Command is the build step that created the layer, normalised back
	// towards the Dockerfile line it came from.
	Command string
	// Instruction is the Dockerfile verb the step came from ("RUN",
	// "COPY", "ENV", …), or "" when the step is not one — the bottom of a
	// distro image is a comment describing how the rootfs was assembled.
	Instruction string
	// Size is how many bytes the step added to the image.
	Size int64
	// SizeKnown distinguishes a step that genuinely added nothing from one
	// whose size Docker did not report.
	SizeKnown bool
	// Created is when the step ran, which for a pulled image is when the
	// publisher built it, not when it arrived here.
	Created time.Time
	// Empty means the step added no filesystem content — ENV, CMD, LABEL
	// and friends. Most of an image's history is these, and they are noise
	// in a size breakdown while being exactly what identifies the image in
	// a provenance one.
	Empty bool
}

// ImageInfo is everything worth knowing about an image beyond its size,
// gathered from `docker image inspect` and `docker history`. It is nil when
// the image could not be inspected, which degrades the detail panel rather
// than the listing.
type ImageInfo struct {
	// Digest is the image's own content digest.
	Digest string
	// RepoDigests are the registry references this image was pulled by,
	// which is the only proof of where it was downloaded from.
	RepoDigests []string
	// Architecture and OS are the platform the image was built for —
	// worth showing on an Apple Silicon machine, where an amd64 image runs
	// under emulation and is often a surprise.
	Architecture string
	OS           string
	// BaseImage is the reference of the image this one was built on, when
	// that image is also on this machine. It is established by layer
	// identity, not by parsing a Dockerfile nobody has.
	BaseImage string
	// BaseSystem names the distribution at the bottom of the image
	// ("Alpine Linux 3.21.2", "Debian trixie"), read off the layer that
	// imported the root filesystem.
	BaseSystem string
	// PackageManager is apt/apk/dnf as inferred from the build steps, for
	// images whose bottom layer gives nothing away.
	PackageManager string
	// Derived lists images on this machine that were built on top of this
	// one. A non-empty list is the strongest possible argument for keeping
	// an image.
	Derived []string
	// Layers is the build history in build order — oldest first, so the
	// list reads like the Dockerfile did.
	Layers []Layer
	// FilesystemLayers is how many layers actually hold content, taken
	// from the image's own RootFS rather than counted from the history. It
	// is smaller than len(Layers) because most build steps are metadata.
	FilesystemLayers int
	// MetadataSteps is how many of the build steps added nothing to the
	// filesystem.
	MetadataSteps int
	// Labels is the image's full label set. The OCI ones
	// (org.opencontainers.image.source and friends) are the jackpot: they
	// name the repository the image was built from.
	Labels map[string]string
	// Env is the subset of the image's environment that identifies the
	// stack — PHP_VERSION, GOLANG_VERSION, GOPATH — rather than the whole
	// environment, which is mostly PATH and noise.
	Env []DetailRow
	// WorkingDir, Entrypoint and Cmd are how the image runs.
	WorkingDir string
	Entrypoint []string
	Cmd        []string
	User       string
	// ExposedPorts and DeclaredVolumes are what the image asks the daemon
	// for. DeclaredVolumes is the reason anonymous volumes exist at all.
	ExposedPorts    []string
	DeclaredVolumes []string
	// BuiltAt is when the publisher built the image; PulledAt is when it
	// landed on this machine. They can be months apart, and the second is
	// the one that answers "do I still use this".
	BuiltAt  time.Time
	PulledAt time.Time
	// ApparentSize is the size `docker images` prints: the whole image
	// including the layers it shares with other images, as against
	// Item.Size, which is only the part removing it would free.
	ApparentSize int64
	// Users are the containers built from this image.
	Users []ContainerRef
}

// ContainerRef names a container from another object's point of view.
type ContainerRef struct {
	ID      string
	Name    string
	State   string
	Project string
	Service string
	// LastUsed is the last moment that container did anything.
	LastUsed time.Time
	// Destination is where the referring volume is mounted inside this
	// container. It is empty when the reference is not about a volume.
	Destination string
	// ReadOnly reports a read-only volume mount.
	ReadOnly bool
}

// VolumeInfo is everything worth knowing about a volume beyond its size.
//
// This is the part of the package that earns its keep. A volume named
// 9c270dd8d451… and sized 275.6 MB tells a developer nothing at all; that
// the same volume is a MySQL data directory holding the databases
// `clip-plus-service` and `clip-plus-service-test` tells them everything,
// including whether they can afford to delete it.
type VolumeInfo struct {
	Driver string
	// Mountpoint is the path inside the Docker VM. On macOS it is not a
	// path this process can open, which is why HostPath exists.
	Mountpoint string
	// HostPath is where this machine can actually read the volume, when
	// anywhere can. OrbStack re-exports the VM's volume directory under
	// ~/OrbStack; a Linux daemon's Mountpoint is readable as it stands;
	// Docker Desktop hides it entirely, and then this is empty.
	HostPath string
	Options  map[string]string
	Labels   map[string]string
	// ComposeProject, ComposeService and ComposeVolume come from the
	// labels docker compose stamps on the volumes it creates, and name the
	// project, the service and the volume's key in the compose file.
	ComposeProject string
	ComposeService string
	ComposeVolume  string
	// Destination is the path the volume was mounted at inside the
	// container, which is usually the single most identifying fact about
	// it: /var/lib/mysql is a database, /root/.composer/cache is a package
	// cache.
	Destination string
	// DestinationSource says how Destination was established — from a
	// container that still exists, or inferred from what is inside the
	// volume. Inference is a guess and is labelled as one.
	DestinationSource string
	// DeclaringImages are images on this machine that declare a VOLUME at
	// exactly this volume's destination. That is evidence, not a guess.
	// It is usually empty: the image that created an anonymous volume is
	// very often gone by the time anyone looks.
	DeclaringImages []string
	// RelatedImages are images whose repository name belongs to the same
	// software family as whatever wrote this volume's contents. That is a
	// guess and nothing more — "there is a mysql image here and this looks
	// like a MySQL directory" — and the panel labels it as one.
	RelatedImages []string
	// Users are the containers that still reference the volume.
	Users []ContainerRef
	// Contents is what a shallow, read-only look inside the volume found.
	Contents VolumeContents
}

// VolumeContents is the result of listing a volume's top-level entries.
//
// The read is deliberately shallow — one directory listing, plus a second
// one for a handful of sub-directories whose names carry provenance — and
// it never opens anything but a couple of tiny marker files. It is the
// difference between "275.6 MB of something" and "a MySQL data directory
// for two named databases".
type VolumeContents struct {
	// Read reports whether the listing happened at all.
	Read bool
	// Path is the directory that was read.
	Path string
	// Unavailable explains why nothing was read, for the panel to show
	// instead of silently pretending the volume is empty.
	Unavailable string
	// Empty means the volume exists but holds nothing.
	Empty bool
	// Entries are the top-level names, sorted, capped.
	Entries []string
	// Truncated reports that there were more entries than Entries holds.
	Truncated bool
	// Software names what the layout says is in there ("MySQL 8 data
	// directory"), or "" when nothing recognisable matched.
	Software string
	// Inferred means Software was worked out from the mount path and the
	// image name rather than from the contents, because the contents could
	// not be read. It is a weaker claim and the panel says so.
	Inferred bool
	// Summary is a sentence a developer can act on.
	Summary string
	// Facts are the specifics recovered from the contents: database names,
	// cached repositories, the node name a RabbitMQ container left behind.
	Facts []DetailRow
	// ConventionalPath is where the software that wrote this normally
	// mounts it, for the common case where the container is gone and
	// nothing else can say.
	ConventionalPath string
	// ImageHints are the repository-name fragments of the image family
	// that writes this layout, used to point at a matching local image.
	ImageHints []string
}

// ContainerInfo is everything worth knowing about a container beyond its
// status line.
type ContainerInfo struct {
	Image   string
	ImageID string
	// Command is what actually runs as PID 1.
	Command string
	// WorkingDir, Ports and Networks are the rest of how it was run.
	WorkingDir string
	Ports      string
	Networks   string
	ExitCode   int
	// Project, Service, ProjectDir and ConfigFiles come from compose's
	// labels. ProjectDir is the directory on this machine the compose file
	// was run from — the answer to "where did this come from" that nothing
	// else provides.
	Project     string
	Service     string
	ProjectDir  string
	ConfigFiles string
	// SourceDir is a host directory recorded by whatever created the
	// container when it was not compose — VS Code dev containers stamp the
	// workspace folder on, for instance.
	SourceDir string
	Labels    map[string]string
	// Mounts is every volume and bind the container has.
	Mounts []ContainerMount
	// Env is the identifying subset of the container's environment.
	Env []DetailRow
}

// ContainerMount is one entry of a container's Mounts array.
type ContainerMount struct {
	// Type is "volume", "bind" or "tmpfs".
	Type string
	// Name is the volume name, empty for a bind mount.
	Name string
	// Source is the host path for a bind, or the volume's mountpoint.
	Source string
	// Destination is the path inside the container.
	Destination string
	ReadOnly    bool
	// Anonymous marks a volume nobody named.
	Anonymous bool
}

// BuildCacheInfo breaks the aggregated build-cache row down into the things
// that identify the builds it came from.
//
// BuildKit records what it pulled and what it executed, so the cache is the
// one object here that can name projects nothing else on the machine
// mentions any more: a registry reference in this list is a project that
// was built here even if its image, containers and volumes have all been
// removed since.
type BuildCacheInfo struct {
	Records int
	InUse   int
	// Total is every record; Reclaimable is the records nothing is using.
	Total       int64
	Reclaimable int64
	// Pulled are the distinct image references BuildKit downloaded while
	// building, newest-largest first.
	Pulled []string
	// Steps are the distinct build commands whose results are cached.
	Steps []string
	// LocalContexts is how many records are copies of a local build
	// context or Dockerfile, i.e. builds run from a directory here.
	LocalContexts int
	// Biggest are the largest individual records, described as BuildKit
	// describes them.
	Biggest []BuildCacheRecord
}

// BuildCacheRecord is one BuildKit cache record.
type BuildCacheRecord struct {
	Type        string
	Description string
	Size        int64
	InUse       bool
	Shared      bool
	UsageCount  int
	LastUsed    time.Time
}

// Details returns everything worth showing about this object, ordered
// most-useful-first. It is a pure function of the Item: the scan has
// already fetched everything, so opening the panel never touches Docker.
func (i Item) Details() []DetailSection {
	return i.details(time.Now())
}

// details is Details with the clock injected, so the tests can pin it.
func (i Item) details(now time.Time) []DetailSection {
	switch i.Kind {
	case KindImage:
		return nonEmpty(i.imageDetails(now))
	case KindVolume:
		return nonEmpty(i.volumeDetails(now))
	case KindContainer:
		return nonEmpty(i.containerDetails(now))
	case KindBuildCache:
		return nonEmpty(i.buildCacheDetails(now))
	default:
		return nil
	}
}

// nonEmpty drops any section that ended up with nothing in it, so a scan
// that could not establish a whole category of fact leaves no titled void
// on the screen.
func nonEmpty(sections []DetailSection) []DetailSection {
	out := sections[:0]
	for _, s := range sections {
		if len(s.Rows) > 0 || len(s.Lines) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// maxDetailLines caps any verbatim block, so one image with two hundred
// build steps cannot push everything else off the panel.
const maxDetailLines = 12

// maxCommandLen caps one rendered build step. Docker's own output for a
// distro image's install step runs to several thousand characters; the
// first few hundred are what identify it.
const maxCommandLen = 300

// imageDetails answers, in order: where did this image come from, what do
// its first layers build, why is it this big, how does it run, and who
// here uses it.
func (i Item) imageDetails(now time.Time) []DetailSection {
	info := i.Image
	if info == nil {
		return []DetailSection{{
			Title: "Details",
			Lines: []string{"Docker did not answer `docker image inspect` for this image, so only the listing is available."},
		}}
	}
	var out []DetailSection

	// Provenance. The OCI labels come first because a repository URL ends
	// the question of what an image is faster than anything else can.
	var prov []DetailRow
	prov = addRow(prov, "Source repository", info.Labels["org.opencontainers.image.source"])
	prov = addRow(prov, "Revision", info.Labels["org.opencontainers.image.revision"])
	prov = addRow(prov, "Upstream version", info.Labels["org.opencontainers.image.version"])
	prov = addRow(prov, "Title", info.Labels["org.opencontainers.image.title"])
	prov = addRow(prov, "Description", truncate(info.Labels["org.opencontainers.image.description"], 160))
	prov = addRow(prov, "Documentation", info.Labels["org.opencontainers.image.documentation"])
	prov = addRow(prov, "Vendor", info.Labels["org.opencontainers.image.vendor"])
	prov = addRow(prov, "Compose project", info.Labels[composeProjectLabel])
	prov = addRow(prov, "Compose service", info.Labels[composeServiceLabel])
	if info.BaseImage != "" {
		prov = addRow(prov, "Built on", info.BaseImage+" — that image is on this machine too, and these "+
			"layers are literally the same layers")
	}
	prov = addRow(prov, "Base system", info.BaseSystem)
	if info.BaseSystem == "" {
		prov = addRow(prov, "Package manager", info.PackageManager)
	}
	if len(info.Derived) > 0 {
		prov = addRow(prov, "Base for", strings.Join(info.Derived, ", ")+" — "+
			countPhrase(len(info.Derived), "image")+" on this machine is built on it")
	}
	prov = addRow(prov, "Built", stampPhrase(info.BuiltAt, now))
	prov = addRow(prov, "Pulled or tagged here", stampPhrase(info.PulledAt, now))
	for _, ref := range info.RepoDigests {
		prov = addRow(prov, "Pulled as", ref)
	}
	if len(prov) == 0 {
		prov = append(prov, DetailRow{
			Label: "Provenance",
			Value: "This image carries no source label, no digest and no local base image, so nothing " +
				"records where it was built. That is normal for an image built locally without labels.",
		})
	}
	out = append(out, DetailSection{Title: "Where it came from", Rows: prov})

	// The first build steps, in build order. This is the section the whole
	// exercise is for: the top of an image's history is what identifies
	// the build, and Docker's own output shows it last and truncated.
	if steps := buildSteps(info.Layers, maxDetailLines); len(steps) > 0 {
		out = append(out, DetailSection{
			Title: "What it builds, first steps first",
			Lines: steps,
		})
	}

	// The size breakdown, which is the answer to "why is this 1.3 GB".
	if big := biggestLayers(info.Layers, 6); len(big) > 0 {
		out = append(out, DetailSection{
			Title: "Biggest layers",
			Lines: big,
		})
	}

	var run []DetailRow
	run = addRow(run, "Working directory", info.WorkingDir)
	run = addRow(run, "Entrypoint", strings.Join(info.Entrypoint, " "))
	run = addRow(run, "Command", strings.Join(info.Cmd, " "))
	run = addRow(run, "Runs as", info.User)
	run = addRow(run, "Exposed ports", strings.Join(info.ExposedPorts, ", "))
	if len(info.DeclaredVolumes) > 0 {
		run = addRow(run, "Declares VOLUME", strings.Join(info.DeclaredVolumes, ", "))
		run = addRow(run, "", "Every `docker run` of this image that does not map those paths to a name "+
			"leaves an anonymous volume behind. That is where they come from.")
	}
	if len(run) > 0 {
		out = append(out, DetailSection{Title: "How it runs", Rows: run})
	}

	if len(info.Env) > 0 {
		out = append(out, DetailSection{Title: "Stack it carries", Rows: info.Env})
	}

	out = append(out, DetailSection{Title: "Used by", Rows: usersRows(info.Users, i.RefCount, now,
		"No container on this machine references this image. Nothing here proves it is unwanted — a "+
			"compose project that is currently down still expects its images to be present.")})

	var store []DetailRow
	store = addRow(store, "Reclaimable", humanSize(i.Size))
	// Only worth a row when the two genuinely differ. The figures come
	// from different Docker commands and disagree by rounding even when
	// nothing is shared, and a row saying "740.0 MB, of which 740.2 MB is
	// shared" would read as a bug.
	if shared := info.ApparentSize - i.Size; shared > 10_000_000 && shared*20 > info.ApparentSize {
		store = addRow(store, "Apparent size", humanSize(info.ApparentSize)+", of which "+humanSize(shared)+
			" is layers it shares with other images and removing it would not free")
	}
	store = addRow(store, "Layers", layerCountPhrase(info))
	store = addRow(store, "Image ID", i.ID)
	store = addRow(store, "Digest", info.Digest)
	store = addRow(store, "Platform", platformPhrase(info))
	out = append(out, DetailSection{Title: "Storage", Rows: store})

	return out
}

// volumeDetails answers the question the user actually has about a
// 64-hex-named volume, in the order they have it: what is in there, where
// did it come from, is anything still using it.
func (i Item) volumeDetails(now time.Time) []DetailSection {
	info := i.Volume
	if info == nil {
		return []DetailSection{{
			Title: "Details",
			Lines: []string{"Docker did not answer `docker volume inspect` for this volume, so only the listing is available."},
		}}
	}
	var out []DetailSection

	// Contents first. For an anonymous volume this is the only section
	// that turns a hash into a decision.
	inside := []DetailRow{}
	c := info.Contents
	switch {
	case c.Software != "" && c.Inferred:
		inside = addRow(inside, "Probably", c.Software+" — judged from where it was mounted and by what, "+
			"since the contents themselves could not be read")
	case c.Software != "":
		inside = addRow(inside, "Looks like", c.Software)
	case c.Empty:
		inside = addRow(inside, "Looks like", "an empty volume")
	case c.Read:
		inside = addRow(inside, "Looks like", "a layout this app does not recognise")
	}
	inside = append(inside, c.Facts...)
	inside = addRow(inside, "Size", humanSize(i.Size))
	if c.Summary != "" {
		inside = addRow(inside, "", c.Summary)
	}
	if !c.Read && c.Unavailable != "" {
		inside = addRow(inside, "", c.Unavailable)
	}
	contents := DetailSection{Title: "What is inside it", Rows: inside}
	if len(c.Entries) > 0 {
		// When the layout was recognised the entry names are only
		// corroboration, and a dozen lines of a database's internal files
		// bury the rows above that carry the actual answer. When it was
		// not, the names are all the user has, so they get the room.
		limit := maxDetailLines
		if c.Software != "" {
			limit = 6
		}
		shown := capLines(c.Entries, limit)
		contents.Lines = append([]string{"Top-level entries:"}, indentAll(shown)...)
		if c.Truncated && len(shown) == len(c.Entries) {
			contents.Lines = append(contents.Lines, "  … and more")
		}
	}
	out = append(out, contents)

	// Provenance.
	var prov []DetailRow
	prov = addRow(prov, "Created", stampPhrase(i.Created, now))
	switch {
	case len(info.Users) > 0:
		u := info.Users[0]
		prov = addRow(prov, "Created by", "the container `"+u.Name+"`, which still exists")
	case i.Anonymous:
		prov = addRow(prov, "Created by", "a container that no longer exists. Docker records no link from a "+
			"volume back to the container that made it, so once that container is removed the only thing "+
			"left to go on is what is written inside the volume.")
	}
	switch {
	case info.Destination != "":
		prov = addRow(prov, "Mounted at", info.Destination+destinationSourceClause(info.DestinationSource))
	case i.Anonymous:
		prov = addRow(prov, "Mounted at", "unknown — nothing on this machine records the path this volume "+
			"was mounted at inside its container")
	}
	prov = addRow(prov, "Compose project", info.ComposeProject)
	prov = addRow(prov, "Compose service", info.ComposeService)
	if info.ComposeVolume != "" {
		prov = addRow(prov, "Compose volume key", info.ComposeVolume+" — this is the name it has in that "+
			"project's compose file, under `volumes:`")
	}
	switch {
	case len(info.DeclaringImages) > 0:
		prov = addRow(prov, "Declared by", strings.Join(info.DeclaringImages, ", ")+" — "+
			plural(len(info.DeclaringImages), "that image is", "those images are")+
			" still on this machine and "+plural(len(info.DeclaringImages), "declares", "declare")+
			" a VOLUME at exactly this path")
	case i.Anonymous && c.Software != "":
		prov = addRow(prov, "Declared by", "an image that is no longer on this machine. Nothing here records "+
			"which one, so the only evidence is the contents, and they say "+c.Software+".")
	}
	if len(info.RelatedImages) > 0 {
		prov = addRow(prov, "Possibly related", strings.Join(info.RelatedImages, ", ")+" — the same software "+
			"family as the contents, matched on the image name alone. Nothing records that this volume came "+
			"from it.")
	}
	if len(prov) == 0 {
		prov = append(prov, DetailRow{Label: "Nothing", Value: "Docker records nothing about where this volume " +
			"came from, and its contents gave nothing away either."})
	}
	out = append(out, DetailSection{Title: "Where it came from", Rows: prov})

	out = append(out, DetailSection{Title: "Used by", Rows: usersRows(info.Users, 0, now,
		"No container on this machine references this volume, so nothing will notice it disappearing — "+
			"and nothing will bring its contents back either.")})

	var store []DetailRow
	store = addRow(store, "Volume name", i.Name)
	store = addRow(store, "Driver", info.Driver)
	store = addRow(store, "Mountpoint", info.Mountpoint)
	if info.HostPath != "" && info.HostPath != info.Mountpoint {
		store = addRow(store, "Readable here", info.HostPath)
	}
	for k, v := range sortedPairs(info.Options) {
		store = addRow(store, "Option "+k, v)
	}
	for k, v := range sortedPairs(info.Labels) {
		store = addRow(store, "Label "+k, v)
	}
	out = append(out, DetailSection{Title: "Storage", Rows: store})

	return out
}

// containerDetails leads with the compose labels, because the working
// directory a container was started from is the "where did this come from"
// answer for a container in exactly the way contents are for a volume.
func (i Item) containerDetails(now time.Time) []DetailSection {
	info := i.Container
	if info == nil {
		return []DetailSection{{
			Title: "Details",
			Lines: []string{"Docker did not answer `docker inspect` for this container, so only the listing is available."},
		}}
	}
	var out []DetailSection

	var prov []DetailRow
	prov = addRow(prov, "Compose project", info.Project)
	prov = addRow(prov, "Compose service", info.Service)
	prov = addRow(prov, "Started from", info.ProjectDir)
	prov = addRow(prov, "Compose file", info.ConfigFiles)
	prov = addRow(prov, "Source directory", info.SourceDir)
	prov = addRow(prov, "Image", info.Image)
	prov = addRow(prov, "Image ID", info.ImageID)
	prov = addRow(prov, "Created", stampPhrase(i.Created, now))
	prov = addRow(prov, "Last active", stampPhrase(i.LastUsed, now))
	if len(prov) == 0 {
		prov = append(prov, DetailRow{Label: "Provenance", Value: "Nothing on this container records where it " +
			"was started from, which is what a bare `docker run` looks like."})
	}
	out = append(out, DetailSection{Title: "Where it came from", Rows: prov})

	var run []DetailRow
	run = addRow(run, "Status", i.Status)
	run = addRow(run, "Command", info.Command)
	run = addRow(run, "Working directory", info.WorkingDir)
	run = addRow(run, "Ports", info.Ports)
	run = addRow(run, "Networks", info.Networks)
	if len(run) > 0 {
		out = append(out, DetailSection{Title: "What it runs", Rows: run})
	}

	if len(info.Env) > 0 {
		out = append(out, DetailSection{Title: "Stack it carries", Rows: info.Env})
	}

	if len(info.Mounts) > 0 {
		lines := make([]string, 0, len(info.Mounts))
		for _, m := range info.Mounts {
			lines = append(lines, mountLine(m))
		}
		out = append(out, DetailSection{
			Title: "Storage it mounts",
			Lines: lines,
		})
	}

	var store []DetailRow
	store = addRow(store, "Writable layer", humanSize(i.Size))
	store = addRow(store, "Container ID", i.ID)
	out = append(out, DetailSection{Title: "Storage", Rows: store})

	return out
}

// buildCacheDetails turns the one aggregated row into the list of projects
// that produced it. BuildKit records the references it pulled and the
// commands it ran, which frequently names projects whose images and
// containers are long gone.
func (i Item) buildCacheDetails(now time.Time) []DetailSection {
	info := i.BuildCache
	if info == nil {
		return []DetailSection{{
			Title: "Details",
			Lines: []string{"`docker system df -v` did not report the individual cache records, so only the total is available."},
		}}
	}
	var out []DetailSection

	if len(info.Pulled) > 0 {
		lines := indentAll(capLines(info.Pulled, maxDetailLines))
		out = append(out, DetailSection{
			Title: "Images these builds pulled",
			Rows: []DetailRow{{
				Label: "",
				Value: "Every base image a build here downloaded, including private registries. A reference " +
					"in this list is a project that was built on this machine, whether or not anything of it " +
					"survives.",
			}},
			Lines: lines,
		})
	}

	if len(info.Steps) > 0 {
		out = append(out, DetailSection{
			Title: "Build steps it is holding results for",
			Lines: indentAll(capLines(info.Steps, maxDetailLines)),
		})
	}

	if len(info.Biggest) > 0 {
		lines := make([]string, 0, len(info.Biggest))
		for _, r := range info.Biggest {
			desc := r.Description
			if desc == "" {
				desc = "an unnamed intermediate result"
			}
			lines = append(lines, "  "+padLeft(humanSize(r.Size), 10)+"  "+truncate(desc, maxCommandLen))
		}
		out = append(out, DetailSection{Title: "Biggest records", Lines: lines})
	}

	var store []DetailRow
	store = addRow(store, "Records", countPhrase(info.Records, "record"))
	if info.InUse > 0 {
		store = addRow(store, "In use", countPhrase(info.InUse, "record")+" a build running right now depends on")
	}
	store = addRow(store, "Total", humanSize(info.Total))
	store = addRow(store, "Reclaimable", humanSize(info.Reclaimable))
	if info.LocalContexts > 0 {
		store = addRow(store, "Local build contexts", countPhrase(info.LocalContexts, "record")+
			" hold a copy of a Dockerfile or build context taken from a directory on this machine")
	}
	store = addRow(store, "Last used", stampPhrase(i.LastUsed, now))
	out = append(out, DetailSection{Title: "Totals", Rows: store})

	return out
}

// usersRows renders the containers referencing an object, or the sentence
// that explains an empty list. An empty section would leave the user
// wondering whether we looked.
func usersRows(users []ContainerRef, refCount int, now time.Time, none string) []DetailRow {
	if len(users) == 0 {
		if refCount > 0 {
			// df counted references but inspect could not name them.
			return []DetailRow{{
				Label: "Referenced by",
				Value: countPhrase(refCount, "container") + " Docker would not name for this scan",
			}}
		}
		return []DetailRow{{Label: "Nothing", Value: none}}
	}
	rows := make([]DetailRow, 0, len(users))
	for _, u := range users {
		value := u.State
		if u.Project != "" {
			value += ", in the `" + u.Project + "` compose project"
			if u.Service != "" {
				value += " as `" + u.Service + "`"
			}
		}
		if u.Destination != "" {
			value += ", mounted at " + u.Destination
			if u.ReadOnly {
				value += " read-only"
			}
		}
		if !u.LastUsed.IsZero() {
			value += ", last active " + agoPhrase(u.LastUsed, now)
		}
		rows = append(rows, DetailRow{Label: u.Name, Value: strings.TrimPrefix(value, ", ")})
	}
	return rows
}

// buildSteps renders an image's history in build order, keeping the steps
// that put something in the image and dropping the metadata ones, which
// outnumber them roughly three to one and say nothing about what was built.
func buildSteps(layers []Layer, limit int) []string {
	out := make([]string, 0, limit)
	for _, l := range layers {
		if l.Command == "" || !meaningfulStep(l) {
			continue
		}
		out = append(out, "  "+truncate(l.Command, maxCommandLen))
		if len(out) == limit {
			break
		}
	}
	return out
}

// meaningfulStep reports whether a history entry describes work rather than
// metadata. An unrecognised instruction counts: the bottom entry of a
// distro image is a comment ("# debian.sh --arch 'arm64' out/ 'trixie'")
// and it is the single most identifying line in the whole history.
func meaningfulStep(l Layer) bool {
	switch l.Instruction {
	case "RUN", "COPY", "ADD", "WORKDIR", "FROM", "VOLUME":
		return true
	case "":
		return true
	default:
		return l.Size > 0
	}
}

// biggestLayers renders the size breakdown, largest first, skipping the
// zero-byte metadata steps that would otherwise fill it.
func biggestLayers(layers []Layer, limit int) []string {
	sized := make([]Layer, 0, len(layers))
	for _, l := range layers {
		if l.Size > 0 && l.Command != "" {
			sized = append(sized, l)
		}
	}
	slices.SortStableFunc(sized, func(a, b Layer) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		default:
			return 0
		}
	})
	if len(sized) > limit {
		sized = sized[:limit]
	}
	out := make([]string, 0, len(sized))
	for _, l := range sized {
		out = append(out, "  "+padLeft(humanSize(l.Size), 10)+"  "+truncate(l.Command, maxCommandLen))
	}
	return out
}

// layerCountPhrase explains the gap between the number of build steps and
// the number of layers that hold anything, which is otherwise confusing:
// `docker history` prints twenty lines for an image made of nine layers.
func layerCountPhrase(info *ImageInfo) string {
	if info.FilesystemLayers == 0 {
		return ""
	}
	phrase := countPhrase(info.FilesystemLayers, "layer") + " holding content"
	if len(info.Layers) > 0 {
		phrase += ", from " + countPhrase(len(info.Layers), "build step")
		if info.MetadataSteps > 0 {
			phrase += " — of which " + countPhrase(info.MetadataSteps, "step") + " only set metadata"
		}
	}
	return phrase
}

// platformPhrase names the image's platform, and says so plainly when it is
// not this machine's.
func platformPhrase(info *ImageInfo) string {
	if info.OS == "" && info.Architecture == "" {
		return ""
	}
	return strings.TrimPrefix(info.OS+"/"+info.Architecture, "/")
}

// mountLine renders one of a container's mounts as a single verbatim line.
func mountLine(m ContainerMount) string {
	source := m.Source
	switch {
	case m.Type == "volume" && m.Anonymous:
		source = "anonymous volume " + shortID(m.Name)
	case m.Type == "volume" && m.Name != "":
		source = "volume " + m.Name
	case m.Type == "bind":
		source = "bind " + m.Source
	case m.Type == "tmpfs":
		source = "tmpfs"
	}
	line := "  " + source + " → " + m.Destination
	if m.ReadOnly {
		line += " (read-only)"
	}
	return line
}

// destinationSourceClause admits when a mount path is inferred rather than
// recorded. An invented certainty here would be worse than saying nothing,
// because the path is the fact the user will act on.
func destinationSourceClause(source string) string {
	switch source {
	case "container":
		return " — recorded by a container that still references it"
	case "image":
		return " — the VOLUME declared by the image this matches"
	case "contents":
		return " — inferred from what is inside it, not recorded anywhere"
	default:
		return ""
	}
}

// addRow appends a row unless the value is empty, so a fact Docker did not
// report leaves no blank line behind.
func addRow(rows []DetailRow, label, value string) []DetailRow {
	if strings.TrimSpace(value) == "" {
		return rows
	}
	return append(rows, DetailRow{Label: label, Value: value})
}

// sortedPairs iterates a label map in a stable order, so the panel does not
// reshuffle itself between scans.
func sortedPairs(m map[string]string) func(func(string, string) bool) {
	return func(yield func(string, string) bool) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

// indentAll indents a verbatim block so it reads as one under its title.
func indentAll(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, "  "+truncate(l, maxCommandLen))
	}
	return out
}

// capLines truncates a verbatim block and says how much was dropped.
func capLines(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	out := slices.Clone(lines[:limit])
	return append(out, fmt.Sprintf("… and %d more", len(lines)-limit))
}

// plural picks between a singular and a plural phrase.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// padLeft right-aligns a size so a column of them lines up.
func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// stampPhrase renders an absolute date with the relative one beside it. The
// absolute date is what a user recognises ("that was the sprint we did X");
// the relative one is what decides whether it is stale.
func stampPhrase(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.DateOnly) + " (" + agoPhrase(t, now) + ")"
}
