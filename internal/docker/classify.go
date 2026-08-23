package docker

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// composeProjectLabel is the label docker compose stamps on every
// container, volume and network it creates. It is the only reliable way to
// tell that two objects belong to the same project — names are not, since
// compose lets a project override them.
const composeProjectLabel = "com.docker.compose.project"

// The rest of compose's labels answer "where did this come from" more
// directly than anything else on a container. workingDirLabel in
// particular is the directory on this machine that `docker compose up` was
// run in, which is the fact a developer recognises instantly and which no
// other Docker surface records.
const (
	composeServiceLabel     = "com.docker.compose.service"
	composeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	composeConfigFilesLabel = "com.docker.compose.project.config_files"
	composeVolumeLabel      = "com.docker.compose.volume"
)

// sourceDirLabels are labels other tools use to record the host directory a
// container belongs to, for the containers compose did not create. VS
// Code's dev containers and Docker Desktop's dev environments both stamp
// the workspace folder on, and it is the same fact by another name.
var sourceDirLabels = []string{
	"devcontainer.local_folder",
	"com.docker.devenvironments.code",
	"com.docker.compose.project.working_dir",
}

// anonymousVolumeLabel is set by the daemon on a volume created because an
// image declared a VOLUME the run did not map to a name.
const anonymousVolumeLabel = "com.docker.volume.anonymous"

// staleAfter is how long a stopped container has to have sat idle before
// this package will call it residue instead of work in progress.
//
// A week is a compromise, not a discovery. A container from a `docker run`
// that exited an hour ago is very probably junk, but it is also exactly
// the container someone mid-debugging is about to `docker start` again,
// and being wrong in that direction is the expensive one. Recent
// containers are still listed and still explained — they are rated Review
// rather than Disposable, which is a difference in emphasis, not in
// visibility.
const staleAfter = 7 * 24 * time.Hour

// caveats records which optional sources a scan actually got, so the prose
// can admit a missing figure rather than present a zero as a finding. Both
// fields mean "we have this".
type caveats struct {
	sizes   bool
	details bool
}

// note returns the sentences to append to every item's description for
// whatever this scan could not establish.
func (c caveats) note() string {
	var out string
	if !c.sizes {
		out += sizesUnavailableNote
	}
	if !c.details {
		out += detailsUnavailableNote
	}
	return out
}

// containerFacts is one container as assembled from every source that
// knows something about it: the listing for its status line, inspect for
// the image it resolved to, its real timestamps, its labels as a proper
// map, and the volumes it mounts.
type containerFacts struct {
	id       string
	name     string
	imageRef string
	imageID  string
	state    string
	status   string
	live     bool
	created  time.Time
	lastUsed time.Time
	project  string
	service  string
	labels   map[string]string
	volumes  []string
	mounts   []ContainerMount
	info     *ContainerInfo
	size     int64
	sizeOK   bool
}

// ref renders this container as another object refers to it.
func (c *containerFacts) ref() ContainerRef {
	state := c.state
	if state == "" {
		state = "state unknown"
	}
	return ContainerRef{
		ID:       c.id,
		Name:     c.name,
		State:    state,
		Project:  c.project,
		Service:  c.service,
		LastUsed: c.lastUsed,
	}
}

// imageFacts is one image, keyed by ID rather than by tag. Docker lists an
// image once per tag it carries; grouping the rows back together is what
// keeps a two-tag image from being offered twice and, more importantly,
// from having its reclaimable size counted twice.
type imageFacts struct {
	id       string
	refs     []string
	dangling bool
	created  time.Time
	size     int64
	sizeOK   bool
	users    []*containerFacts
	info     *ImageInfo
	// apparent is the size including shared layers, as `docker images`
	// prints it, as against size, which is only what removing it frees.
	apparent int64
	// layers is the image's RootFS layer list, kept for the base-image
	// match below rather than for display.
	layers []string
}

// volumeFacts is one volume plus everything that references it.
type volumeFacts struct {
	name      string
	labels    map[string]string
	anonymous bool
	project   string
	links     int
	linksOK   bool
	created   time.Time
	size      int64
	sizeOK    bool
	users     []*containerFacts
	info      *VolumeInfo
}

// classify turns a fetched snapshot into the classified item list. It is a
// pure function of its inputs — now included, so tests can pin the clock
// and reason about staleness — and never touches the daemon.
func classify(snap snapshot, now time.Time) ([]Item, error) {
	psLines, err := decodeLines[psLine](snap.ps)
	if err != nil {
		return nil, fmt.Errorf("reading container list: %w", err)
	}
	imageLines, err := decodeLines[imageLine](snap.images)
	if err != nil {
		return nil, fmt.Errorf("reading image list: %w", err)
	}
	volumeLines, err := decodeLines[volumeLine](snap.volumes)
	if err != nil {
		return nil, fmt.Errorf("reading volume list: %w", err)
	}

	// Enrichment is best-effort by design: a machine whose `docker
	// inspect` or `docker system df` failed still gets a full listing,
	// with the prose admitting which figures are missing.
	inspects, inspectErr := decodeLines[containerInspect](snap.inspect)
	volInspects, _ := decodeLines[volumeInspect](snap.volInspect)
	imgInspects, _ := decodeLines[imageInspect](snap.imgInspect)
	df, dfErr := parseDF(snap.df)
	cav := caveats{
		sizes:   dfErr == nil && !snap.sizesFailed,
		details: inspectErr == nil && !snap.detailsFailed,
	}

	containers := buildContainers(psLines, inspects, df)
	images := buildImages(imageLines, df, containers, imgInspects, snap.history)
	linkBaseImages(images)
	volumes := buildVolumes(volumeLines, volInspects, df, containers, images, snap.probes)

	items := make([]Item, 0, len(containers)+len(images)+len(volumes)+1)
	for _, c := range containers {
		items = append(items, containerItem(c, now, cav))
	}
	for _, img := range images {
		items = append(items, imageItem(img, cav))
	}
	for _, v := range volumes {
		items = append(items, volumeItem(v, cav))
	}
	if bc, ok := buildCacheItem(df.BuildCache); ok {
		items = append(items, bc)
	}

	// Grouped by kind, biggest first inside each group: the kinds mean
	// different things and are not meaningfully comparable by size, while
	// within a kind size is the first thing anyone wants to sort by.
	slices.SortStableFunc(items, func(a, b Item) int {
		if a.Kind != b.Kind {
			return int(a.Kind) - int(b.Kind)
		}
		if a.Size != b.Size {
			if a.Size > b.Size {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return items, nil
}

// buildContainers merges the three container sources into one record per
// container, keyed by the short ID that all of them agree on.
func buildContainers(lines []psLine, inspects []containerInspect, df systemDF) []*containerFacts {
	// df's container sizes are the reclaimable writable-layer figure, and
	// the plain listing does not compute them at all unless asked.
	dfSize := make(map[string]string, len(df.Containers))
	for _, c := range df.Containers {
		dfSize[shortID(c.ID)] = c.Size
	}
	detail := make(map[string]containerInspect, len(inspects))
	for _, in := range inspects {
		detail[shortID(in.ID)] = in
	}

	out := make([]*containerFacts, 0, len(lines))
	for _, l := range lines {
		id := shortID(l.ID)
		if id == "" {
			continue
		}
		c := &containerFacts{
			id:       id,
			name:     firstName(l.Names),
			imageRef: strings.TrimSpace(l.Image),
			status:   strings.TrimSpace(l.Status),
			labels:   parseLabels(l.Labels),
			created:  parseTime(l.CreatedAt),
		}

		in, haveDetail := detail[id]
		if haveDetail {
			// inspect wins wherever the two overlap: it reports the
			// resolved image ID rather than whatever reference the
			// container happened to be created with, real timestamps
			// rather than prose, and labels as a map rather than a
			// comma-joined string that a value containing a comma would
			// have corrupted.
			if n := strings.TrimPrefix(in.Name, "/"); n != "" {
				c.name = n
			}
			c.imageID = shortID(in.Image)
			if len(in.Config.Labels) > 0 {
				c.labels = in.Config.Labels
			}
			if in.Config.Image != "" && c.imageRef == "" {
				c.imageRef = in.Config.Image
			}
			c.created = newest(c.created, parseTime(in.Created))
			c.lastUsed = newest(parseTime(in.State.FinishedAt), parseTime(in.State.StartedAt), c.created)
		}
		c.state = deriveState(l.State, in.State.Status, l.Status)
		c.live = liveState(c.state) || (haveDetail && in.State.Running)
		c.project = c.labels[composeProjectLabel]
		c.service = c.labels[composeServiceLabel]
		for _, m := range in.Mounts {
			mount := ContainerMount{
				Type:        strings.ToLower(strings.TrimSpace(m.Type)),
				Name:        m.Name,
				Source:      m.Source,
				Destination: m.Destination,
				ReadOnly:    !m.RW,
				Anonymous:   anonymousVolumeRe.MatchString(m.Name),
			}
			c.mounts = append(c.mounts, mount)
			if mount.Type == "volume" && m.Name != "" {
				c.volumes = append(c.volumes, m.Name)
			}
		}
		if size, ok := parseSize(dfSize[id]); ok {
			c.size, c.sizeOK = size, true
		} else if size, ok := parseSize(l.Size); ok {
			c.size, c.sizeOK = size, true
		}
		if haveDetail {
			c.info = containerInfo(c, l, in)
		}
		out = append(out, c)
	}
	return out
}

// containerInfo assembles the container's detail panel from the listing and
// its inspect record.
func containerInfo(c *containerFacts, l psLine, in containerInspect) *ContainerInfo {
	info := &ContainerInfo{
		Image:       c.imageRef,
		ImageID:     c.imageID,
		Command:     containerCommand(l, in),
		WorkingDir:  in.Config.WorkingDir,
		Ports:       strings.TrimSpace(l.Ports),
		Networks:    strings.TrimSpace(l.Networks),
		ExitCode:    in.State.ExitCode,
		Project:     c.project,
		Service:     c.service,
		ProjectDir:  c.labels[composeWorkingDirLabel],
		ConfigFiles: c.labels[composeConfigFilesLabel],
		Labels:      c.labels,
		Mounts:      c.mounts,
		Env:         identifyingEnv(in.Config.Env),
	}
	for _, label := range sourceDirLabels {
		if dir := c.labels[label]; dir != "" && dir != info.ProjectDir {
			info.SourceDir = dir
			break
		}
	}
	return info
}

// containerCommand renders what actually runs as PID 1.
//
// inspect's Path/Args is the resolved truth — the entrypoint and command
// already merged — while `docker ps` only prints a truncated rendering of
// it. The listing is the fallback for when inspect gave nothing.
func containerCommand(l psLine, in containerInspect) string {
	if in.Path != "" {
		return strings.TrimSpace(in.Path + " " + strings.Join(in.Args, " "))
	}
	return strings.Trim(strings.TrimSpace(l.Command), `"`)
}

// deriveState settles on a container state from the sources available.
//
// The State column only exists in reasonably recent CLIs, and inspect may
// not have run at all, so the prose status line is the last resort: it
// always begins with a word that gives the state away ("Up 3 hours",
// "Exited (0) 2 weeks ago", "Created").
func deriveState(psState, inspectState, statusText string) string {
	if s := strings.ToLower(strings.TrimSpace(psState)); s != "" {
		return s
	}
	if s := strings.ToLower(strings.TrimSpace(inspectState)); s != "" {
		return s
	}
	fields := strings.Fields(statusText)
	if len(fields) == 0 {
		return ""
	}
	switch strings.ToLower(fields[0]) {
	case "up":
		return "running"
	case "exited":
		return "exited"
	case "created":
		return "created"
	case "restarting":
		return "restarting"
	case "paused":
		return "paused"
	case "dead":
		return "dead"
	case "removal", "removing":
		return "removing"
	}
	return ""
}

// liveState reports whether a container in this state is doing something.
// "removing" counts: the daemon is mid-delete, and offering to delete it
// again would only produce an error.
func liveState(state string) bool {
	switch state {
	case "running", "restarting", "paused", "removing":
		return true
	}
	return false
}

// settledState reports whether a container is stopped in a way we
// understand well enough to rate it.
func settledState(state string) bool {
	switch state {
	case "created", "exited", "dead":
		return true
	}
	return false
}

// buildImages groups the image listing by ID, attaches df's reclaimable
// size, and joins containers to the image each was built from.
func buildImages(
	lines []imageLine,
	df systemDF,
	containers []*containerFacts,
	inspects []imageInspect,
	history map[string][]byte,
) []*imageFacts {
	// UniqueSize is the layers that exist only in this image, which is
	// precisely what deleting it frees; Size counts shared layers too and
	// so over-reports every image that has a common base. Summing
	// UniqueSize across every image reproduces the RECLAIMABLE total that
	// `docker system df` prints, which is the check that this is the
	// right column.
	dfSize := make(map[string]string, len(df.Images))
	for _, i := range df.Images {
		dfSize[shortID(i.ID)] = i.UniqueSize
	}

	var (
		out   []*imageFacts
		byID  = make(map[string]*imageFacts, len(lines))
		byRef = make(map[string]*imageFacts, len(lines))
	)
	for _, l := range lines {
		id := shortID(l.ID)
		if id == "" {
			continue
		}
		img := byID[id]
		if img == nil {
			img = &imageFacts{id: id, created: parseTime(l.CreatedAt), dangling: true}
			if size, ok := parseSize(dfSize[id]); ok {
				img.size, img.sizeOK = size, true
			}
			// The listing's own Size column is the apparent size — the
			// whole image, shared layers included — which is the figure
			// `docker images` prints and the one a user recognises.
			img.apparent, _ = parseSize(l.Size)
			byID[id] = img
			out = append(out, img)
		}
		// Only a row with no repository at all is dangling — an image
		// whose name a later build took over. A repository with no tag is
		// not the same thing: that is an image pulled by digest, which
		// still has a name and can still be pulled by it.
		if !isNone(l.Repository) {
			img.dangling = false
			if !isNone(l.Tag) {
				ref := l.Repository + ":" + l.Tag
				img.refs = append(img.refs, ref)
				byRef[ref] = img
			}
			if !isNone(l.Digest) {
				ref := l.Repository + "@" + l.Digest
				byRef[ref] = img
				if isNone(l.Tag) {
					img.refs = append(img.refs, ref)
				}
			}
		}
	}

	// The join runs ID first and reference second on purpose. `docker ps`
	// reports whichever of the two the container was created with, and
	// only the ID survives a tag being moved to a newer build — matching
	// on the reference alone would credit a container to the image that
	// now owns the name rather than the one it is actually running.
	for _, c := range containers {
		img := byID[c.imageID]
		if img == nil {
			img = byRef[c.imageRef]
		}
		if img == nil {
			img = byID[shortID(c.imageRef)]
		}
		if img != nil {
			img.users = append(img.users, c)
		}
	}

	// Enrichment last, so a failed inspect costs the detail panel and
	// nothing else: every image above is already listed, sized and joined.
	for _, in := range inspects {
		img := byID[shortID(in.ID)]
		if img == nil {
			continue
		}
		img.layers = in.RootFS.Layers
		img.info = imageInfo(in, history[img.id])
		img.info.ApparentSize = img.apparent
	}
	for _, img := range out {
		if img.info == nil {
			continue
		}
		for _, c := range img.users {
			img.info.Users = append(img.info.Users, c.ref())
		}
	}
	return out
}

// imageInfo assembles one image's detail panel from its inspect record and
// its build history.
func imageInfo(in imageInspect, historyOut []byte) *ImageInfo {
	info := &ImageInfo{
		Digest:          strings.TrimSpace(in.ID),
		RepoDigests:     in.RepoDigests,
		Architecture:    in.Architecture,
		OS:              in.Os,
		Labels:          in.Config.Labels,
		Env:             identifyingEnv(in.Config.Env),
		WorkingDir:      in.Config.WorkingDir,
		Entrypoint:      in.Config.Entrypoint,
		Cmd:             in.Config.Cmd,
		User:            in.Config.User,
		ExposedPorts:    sortedKeys(in.Config.ExposedPorts),
		DeclaredVolumes: sortedKeys(in.Config.Volumes),
		BuiltAt:         parseTime(in.Created),
		PulledAt:        parseTime(in.Metadata.LastTagTime),
		// The RootFS list is the authoritative layer count. Counting the
		// non-empty history entries instead gets a slightly different
		// answer, because a step can create a layer that happens to
		// measure zero bytes, and the two figures side by side would look
		// like an arithmetic mistake.
		FilesystemLayers: len(in.RootFS.Layers),
	}
	if layers, err := parseHistory(historyOut); err == nil && len(layers) > 0 {
		info.Layers = layers
		info.MetadataSteps = metadataSteps(layers)
		info.BaseSystem = baseSystem(layers)
		info.PackageManager = packageManager(layers)
	}
	return info
}

// linkBaseImages works out which images on this machine were built on which
// others, and records the relationship in both directions.
//
// It compares layer lists rather than parsing anything. An image built FROM
// another one keeps that image's layers, in order, underneath its own, so
// image A is a base of image B exactly when A's layer list is a proper
// prefix of B's. That is not a heuristic — the layers are the same content
// digests — and it is the only way to establish a base image, since Docker
// keeps no record of the Dockerfile's FROM line.
//
// The longest matching prefix wins, so an image built on php:8.3-cli is
// credited to php:8.3-cli rather than to the Debian image underneath it.
func linkBaseImages(images []*imageFacts) {
	named := make([]*imageFacts, 0, len(images))
	for _, img := range images {
		if len(img.layers) > 0 && len(img.refs) > 0 {
			named = append(named, img)
		}
	}
	for _, child := range images {
		if child.info == nil || len(child.layers) == 0 {
			continue
		}
		var best *imageFacts
		for _, parent := range named {
			if parent == child || len(parent.layers) >= len(child.layers) {
				continue
			}
			if !slices.Equal(parent.layers, child.layers[:len(parent.layers)]) {
				continue
			}
			if best == nil || len(parent.layers) > len(best.layers) {
				best = parent
			}
		}
		if best == nil {
			continue
		}
		child.info.BaseImage = imageName(best)
		if best.info != nil {
			best.info.Derived = append(best.info.Derived, imageName(child))
		}
	}
}

// buildVolumes assembles the volume listing with df's size, inspect's
// creation time, the containers that reference each volume, and — the part
// that matters — everything that can be recovered about where an anonymous
// volume came from.
func buildVolumes(
	lines []volumeLine,
	inspects []volumeInspect,
	df systemDF,
	containers []*containerFacts,
	images []*imageFacts,
	probes map[string]volumeProbe,
) []*volumeFacts {
	dfLine := make(map[string]volumeLine, len(df.Volumes))
	for _, v := range df.Volumes {
		dfLine[v.Name] = v
	}
	detail := make(map[string]volumeInspect, len(inspects))
	for _, in := range inspects {
		detail[in.Name] = in
	}
	// The join that answers "which container created this volume" runs
	// from the container side, not the volume side: a volume record holds
	// no reference to anything, while every container lists the volumes it
	// mounts and the path it mounts each one at.
	users := make(map[string][]*containerFacts)
	mountedAt := make(map[string]ContainerMount)
	for _, c := range containers {
		for _, name := range c.volumes {
			users[name] = append(users[name], c)
		}
		for _, m := range c.mounts {
			if m.Type == "volume" && m.Name != "" {
				if _, seen := mountedAt[m.Name]; !seen {
					mountedAt[m.Name] = m
				}
			}
		}
	}
	declared, refs := imageVolumeIndex(images)

	out := make([]*volumeFacts, 0, len(lines))
	for _, l := range lines {
		name := strings.TrimSpace(l.Name)
		if name == "" {
			continue
		}
		v := &volumeFacts{name: name, labels: parseLabels(l.Labels), users: users[name]}
		info := &VolumeInfo{
			Driver:     strings.TrimSpace(l.Driver),
			Mountpoint: strings.TrimSpace(l.Mountpoint),
		}
		if in, ok := detail[name]; ok {
			v.created = parseTime(in.CreatedAt)
			if len(in.Labels) > 0 {
				v.labels = in.Labels
			}
			if in.Driver != "" {
				info.Driver = in.Driver
			}
			if in.Mountpoint != "" {
				info.Mountpoint = in.Mountpoint
			}
			info.Options = in.Options
		}
		if d, ok := dfLine[name]; ok {
			if size, ok := parseSize(d.Size); ok {
				v.size, v.sizeOK = size, true
			}
			v.links, v.linksOK = parseCount(d.Links)
		}
		v.project = v.labels[composeProjectLabel]
		// The label is authoritative; the name pattern covers volumes
		// created by daemons old enough to predate the label.
		_, labelled := v.labels[anonymousVolumeLabel]
		v.anonymous = labelled || anonymousVolumeRe.MatchString(name)

		info.Labels = v.labels
		info.ComposeProject = v.project
		info.ComposeService = v.labels[composeServiceLabel]
		info.ComposeVolume = v.labels[composeVolumeLabel]
		info.Contents = describeContents(probes[name])
		info.HostPath = info.Contents.Path
		mountedBy := ""
		for _, c := range v.users {
			ref := c.ref()
			if m, ok := mountFor(c, name); ok {
				ref.Destination = m.Destination
				ref.ReadOnly = m.ReadOnly
			}
			if mountedBy == "" {
				mountedBy = c.imageRef
			}
			info.Users = append(info.Users, ref)
		}
		resolveVolumeOrigin(info, mountedAt[name], mountedBy, declared, refs)
		v.info = info
		out = append(out, v)
	}
	return out
}

// mountFor finds how one container mounts one volume.
func mountFor(c *containerFacts, volume string) (ContainerMount, bool) {
	for _, m := range c.mounts {
		if m.Type == "volume" && m.Name == volume {
			return m, true
		}
	}
	return ContainerMount{}, false
}

// imageVolumeIndex maps every VOLUME declared by an image still on this
// machine to the images that declare it, and returns the full list of image
// references alongside.
func imageVolumeIndex(images []*imageFacts) (map[string][]string, []string) {
	declared := make(map[string][]string)
	var refs []string
	for _, img := range images {
		name := imageName(img)
		refs = append(refs, name)
		if img.info == nil {
			continue
		}
		for _, path := range img.info.DeclaredVolumes {
			declared[path] = append(declared[path], name)
		}
	}
	return declared, refs
}

// resolveVolumeOrigin establishes where a volume was mounted and what
// declared it, in descending order of how much the answer can be trusted.
//
// This is the hard case the package exists for. An anonymous volume is
// created because some image declared a VOLUME and the run did not map it
// to a name — but Docker records no link from the volume back to either the
// image or the container, and once that container is removed there is
// nothing left to join on. So:
//
//  1. A container that still exists is proof, and reports the exact path.
//  2. Compose's own labels name the project and the volume's key.
//  3. Otherwise the contents are the only witness left, and what they say
//     is labelled as inference rather than as record — the conventional
//     mount path of the software that wrote them, and any image still here
//     from the same family.
func resolveVolumeOrigin(info *VolumeInfo, mount ContainerMount, mountedBy string, declared map[string][]string, refs []string) {
	if mount.Destination != "" {
		info.Destination = mount.Destination
		info.DestinationSource = "container"
		info.DeclaringImages = declared[mount.Destination]
		// With a recorded path and image, what is in there can be named
		// even on an installation where the volume cannot be read at all.
		inferContents(&info.Contents, mount.Destination, mountedBy)
		return
	}
	if path := info.Contents.ConventionalPath; path != "" {
		info.Destination = path
		info.DestinationSource = "contents"
		// An image declaring a VOLUME at exactly that path is evidence; an
		// image whose *name* merely belongs to the same software family is
		// a guess, and the two are kept in separate fields so the panel
		// never presents the second as the first.
		if named := declared[path]; len(named) > 0 {
			info.DeclaringImages = named
			info.DestinationSource = "image"
			return
		}
		info.RelatedImages = contentsImages(info.Contents, refs)
	}
}

// containerItem rates one container and writes its prose.
func containerItem(c *containerFacts, now time.Time, cav caveats) Item {
	item := Item{
		Kind:      KindContainer,
		ID:        c.id,
		Name:      c.name,
		Size:      c.size,
		Created:   c.created,
		LastUsed:  c.lastUsed,
		InUse:     c.live,
		Anonymous: isGeneratedName(c.name) && c.project == "",
		Project:   c.project,
		Status:    c.status,
		Container: c.info,
	}
	// Old CLIs and partial output can leave the status column empty; a
	// blank cell reads as a bug, so fall back to the bare state.
	if item.Status == "" {
		if c.state == "" {
			item.Status = "State unknown"
		} else {
			item.Status = strings.ToUpper(c.state[:1]) + c.state[1:]
		}
	}

	image := c.imageRef
	if image == "" {
		image = "an image that is no longer on this machine"
	}

	switch {
	case c.live:
		item.Verdict = VerdictKeep
		item.Description = "A running container" + projectClause(c.project) + ", started from " + image +
			". Whatever it is serving — a database, a web server, a background worker — is serving it right now."
		item.Effects = "Nothing is offered here while the container is running. Removing a live container means " +
			"killing the process inside it first, which this app will not do on your behalf: stop it yourself " +
			"with `docker stop " + c.id + "` (or `docker compose down` for a compose project) and it will show " +
			"up here as a stopped container on the next scan."

	case !settledState(c.state):
		// An unrecognised state means a Docker version reporting
		// something this package has never seen. Say so and offer
		// nothing, rather than guess.
		item.Verdict = VerdictUnknown
		item.Description = "A container in a state this app does not recognise (" + quoteOrUnknown(c.state) +
			"), created from " + image + "."
		item.Effects = "No action is offered, because the container's state could not be established and " +
			"removing something that might still be running is not a risk worth taking. Inspect it yourself " +
			"with `docker inspect " + c.id + "`."

	default:
		item.Verdict = VerdictReview
		item.RemoveCmd = "docker rm " + c.id
		stale := !c.lastUsed.IsZero() && now.Sub(c.lastUsed) >= staleAfter
		switch {
		case item.Anonymous && stale:
			item.Verdict = VerdictDisposable
			item.Description = "A stopped container the daemon named itself (" + c.name + "), built from " +
				image + ". Docker only invents a name when whatever created the container did not supply " +
				"one, which in practice means a bare `docker run` typed by hand rather than anything a " +
				"compose file or a script manages. It last did anything " + agoPhrase(c.lastUsed, now) +
				", so nothing is waiting on it."
		case item.Anonymous:
			item.Description = "A stopped container the daemon named itself (" + c.name + "), built from " +
				image + " — the signature of a one-off `docker run` rather than a managed service. It last " +
				"did something " + agoPhrase(c.lastUsed, now) + ", though, which is recent enough that you " +
				"may still be in the middle of whatever it was for."
		case c.project != "":
			item.Description = "A stopped container belonging to the `" + c.project + "` compose project, " +
				"built from " + image + ". `docker compose up` recreates it from the project's compose file, " +
				"so the container itself is reproducible — what is not reproducible is anything written " +
				"inside it that was not on a mounted volume."
		default:
			item.Description = "A stopped container you named yourself (" + c.name + "), built from " + image +
				". A deliberate name usually means it was meant to be started again, so this is not treated " +
				"as leftovers."
		}
		item.Effects = "Removes the container and its writable layer: anything written inside it that did not " +
			"land on a mounted volume — rows added to a database that has no volume, files created by hand, " +
			"packages installed inside the running container — goes with it and cannot be recovered. The image " +
			"it was built from stays on disk, so re-creating it needs no download. Named volumes it mounted " +
			"survive too, attached to nothing, and are listed separately here."
	}

	if !c.sizeOK {
		item.Size = 0
		if cav.sizes {
			item.Description += " Docker reported no size for this container's writable layer, so the figure " +
				"shown is 0 rather than an estimate."
		}
	}
	item.Description += cav.note()
	return item
}

// imageItem rates one image and writes its prose. This is where the
// shared-image rule lives: an image several containers are built from is
// infrastructure, and the size column is the least interesting thing about
// it.
func imageItem(img *imageFacts, cav caveats) Item {
	projects := distinctProjects(img.users)
	item := Item{
		Kind:      KindImage,
		ID:        img.id,
		Name:      imageName(img),
		Size:      img.size,
		Created:   img.created,
		Anonymous: img.dangling,
		RefCount:  len(img.users),
		Shared:    len(img.users) > 1,
		Image:     img.info,
	}
	for _, c := range img.users {
		item.LastUsed = newest(item.LastUsed, c.lastUsed)
		if c.live {
			item.InUse = true
		}
	}
	if len(projects) == 1 {
		item.Project = projects[0]
	}
	item.Status = imageStatus(img, projects)

	switch {
	case item.Shared:
		// The case this whole package was written for.
		item.Verdict = VerdictKeep
		item.Description = item.Name + " is the base for " + countPhrase(len(img.users), "container") +
			projectsClause(projects) + ". That makes it shared infrastructure rather than leftovers: it is " +
			"not one project's private image that happens to be big, it is the image several things on this " +
			"machine are built on."
		item.Effects = "Docker refuses to delete an image while containers are built on it, so this frees " +
			"nothing until all " + countPhrase(len(img.users), "container") + " are deleted first. Do that, " +
			"delete the image, and every one of the projects sharing it pulls it down from the registry again " +
			"on its next start — not just the project you had in mind. This is the re-pull that is worth " +
			"avoiding, which is why nothing is offered here."

	case item.InUse:
		item.Verdict = VerdictKeep
		item.Description = item.Name + " is the image a container that is running right now was built from" +
			projectsClause(projects) + "."
		item.Effects = "Nothing is offered here. Docker will not delete an image out from under a live " +
			"container, and there is no version of this that ends well: stop and remove the container first " +
			"if you genuinely want the image gone."

	case item.RefCount == 1:
		item.Verdict = VerdictKeep
		item.Description = item.Name + " is the image the container `" + img.users[0].name + "` was built from" +
			projectsClause(projects) + ". The container is stopped, but it still holds a reference to this " +
			"image."
		item.Effects = "Docker refuses to delete an image a container still references, so this row frees " +
			"nothing on its own — remove that container first, and this image becomes removable. After that, " +
			"anything that names " + item.Name + " re-pulls it from the registry the next time it runs."

	case img.dangling:
		item.Verdict = VerdictDisposable
		item.Description = "A dangling image: no repository and no tag point at it any more, only the ID " +
			img.id + ". This is what an image becomes when a later `docker build` takes over the name it used " +
			"to hold, so it is nearly always a superseded build rather than anything anyone still wants. " +
			"Nothing can refer to it by name, and no container on this machine references it."
		item.Effects = "Removes layers that only this image holds — layers it shares with an image you still " +
			"have are not touched, and the size shown is that unique portion, which is what pruning actually " +
			"frees. Nothing loses a base image, because nothing can name this one. The only thing given up is " +
			"the ability to `docker run " + img.id + "` that exact build by ID."
		item.RemoveCmd = "docker rmi " + img.id

	default:
		item.Verdict = VerdictReview
		item.Description = item.Name + " is a tagged image no container on this machine references right " +
			"now. That is not proof it is unwanted: a compose project that is currently down still expects " +
			"its images to be there, and this is exactly how a shared base image looks between runs. Check " +
			"whether a project of yours names it before removing it."
		item.Effects = "Removes the layers unique to this image; layers it shares with other images stay, " +
			"since those images still need them, and the size shown is that unique portion rather than the " +
			"apparent size. Anything that names " + item.Name + " pulls it from the registry again the next " +
			"time it runs, which needs network access and takes as long as the download takes. No container " +
			"and no volume is affected."
		item.RemoveCmd = "docker rmi " + imageRemoveTarget(img)
	}

	if len(img.refs) > 1 && item.RemoveCmd != "" {
		item.Description += " It carries " + countPhrase(len(img.refs), "tag") +
			", so removing it means removing every one of them — the command below does that; dropping a " +
			"single tag would only untag the image and free nothing."
	}
	if !img.sizeOK {
		item.Size = 0
		if cav.sizes {
			item.Description += " Docker reported no reclaimable size for this image, so the figure shown is " +
				"0 rather than an estimate."
		}
	}
	item.Description += cav.note()
	return item
}

// volumeItem rates one volume and writes its prose.
//
// Volumes get the most conservative treatment of the three kinds, because
// they are the only one holding data that no registry can hand back. An
// image is a download; a volume is the database.
func volumeItem(v *volumeFacts, cav caveats) Item {
	referenced := len(v.users) > 0 || v.links > 0
	// A volume has no identifier other than its name. Shortening an
	// anonymous one to 12 characters is right, because its name is a hex
	// ID and a prefix of it still identifies it; shortening a name
	// somebody chose would just corrupt it.
	id := v.name
	if v.anonymous {
		id = shortID(v.name)
	}
	item := Item{
		Kind:      KindVolume,
		ID:        id,
		Name:      v.name,
		Size:      v.size,
		Created:   v.created,
		InUse:     referenced,
		Anonymous: v.anonymous,
		Project:   v.project,
		Status:    volumeStatus(v, referenced),
		Volume:    v.info,
	}
	for _, c := range v.users {
		item.LastUsed = newest(item.LastUsed, c.lastUsed)
	}

	switch {
	case referenced:
		item.Verdict = VerdictKeep
		item.Description = "A volume " + countPhrase(max(len(v.users), v.links), "container") +
			" still references" + projectClause(v.project) + ". Docker counts a stopped container as a " +
			"reference just like a running one, because starting it again has to find its data where it left it."
		item.Effects = "Nothing is offered here. `docker volume rm` refuses to touch a volume a container " +
			"references, and forcing it would leave that container pointing at data that no longer exists. " +
			"Remove the containers using it first if you really want it gone."

	case v.anonymous:
		item.Verdict = VerdictDisposable
		item.Description = contentsClause(v) + "An anonymous volume: the daemon created it because an image " +
			"declared a VOLUME the run never mapped to a name, and gave it a 64-character random name because " +
			"nobody chose one. One of these is left behind by every `docker run` of an image like that which " +
			"is not cleaned up afterwards, which is why they pile up. No container on this machine references " +
			"it any more."
		item.Effects = "Deletes the volume's contents outright, and there is no re-pull that brings them back " +
			"— unlike an image, a volume holds data rather than a copy of something a registry still has. In " +
			"practice what is in one of these is whatever a since-deleted container wrote: a scratch database " +
			"from an experiment, an upload directory, a package cache. Nothing that still exists refers to it, " +
			"and no compose file can, because compose can only name a volume it named itself."
		item.RemoveCmd = "docker volume rm " + v.name

	default:
		item.Verdict = VerdictReview
		if v.project != "" {
			item.Description = contentsClause(v) + "A named volume belonging to the `" + v.project + "` compose project. This is " +
				"where that project keeps the data it intends to survive `docker compose down` — a database's " +
				"data directory is the usual case. Its containers are not around at the moment, which is the " +
				"normal state of a project that is simply not running."
		} else {
			item.Description = contentsClause(v) + "A volume somebody named deliberately (" + v.name + "). " +
				"Nothing references it right now, but a deliberate name means it was created to hold " +
				"something worth keeping, and an unreferenced named volume is the normal state of a project " +
				"between runs."
		}
		item.Effects = "Deletes the data. There is nothing to re-download: whatever this volume holds — " +
			"database contents, uploaded files, generated state — is gone, and the next `docker compose up` " +
			"or `docker run` that mounts this name starts from an empty volume as though it were the first " +
			"time. Make sure the project that created it is really finished with before removing it."
		item.RemoveCmd = "docker volume rm " + v.name
	}

	if !v.sizeOK {
		item.Size = 0
		if cav.sizes {
			item.Description += " Docker reported no size for this volume, so the figure shown is 0 rather " +
				"than an estimate."
		}
	}
	item.Description += cav.note()
	return item
}

// contentsClause opens a volume's description with what is actually in it,
// when that could be established.
//
// It goes first on purpose. Everything else this package can say about an
// anonymous volume — that it is anonymous, that nothing references it, how
// big it is — is true of two dozen volumes at once and settles nothing.
// "A MySQL 8 data directory holding the databases `clip-plus-service` and
// `clip-plus-service-test`" settles it in one sentence.
func contentsClause(v *volumeFacts) string {
	if v.info == nil {
		return ""
	}
	c := v.info.Contents
	switch {
	case c.Empty:
		return "This volume is empty. "
	case c.Software == "":
		return ""
	}
	clause := "Judging by what is inside it, this is " + article(c.Software) + c.Software
	for _, fact := range c.Facts {
		if fact.Label == "Databases" {
			clause += ", holding " + fact.Value
			break
		}
	}
	return clause + ". "
}

// article picks "a" or "an" for a phrase this package assembled, so the
// prose does not read like a template. It only ever sees the software names
// above, which are ordinary English words.
func article(phrase string) string {
	if phrase == "" {
		return ""
	}
	if strings.ContainsRune("aeiouAEIOU", rune(phrase[0])) {
		return "an "
	}
	return "a "
}

// buildCacheItem folds every BuildKit cache record into a single row.
//
// There are typically hundreds of them, with opaque IDs and no individual
// meaning, and — decisively — the CLI offers no way to remove one: `docker
// builder prune` takes filters for age and size, never an ID. A row per
// record would therefore be several hundred lines of noise, each carrying
// a RemoveCmd that could not be written honestly. One aggregate row with
// the one command that exists is the truthful shape.
func buildCacheItem(records []buildCacheLine) (Item, bool) {
	if len(records) == 0 {
		return Item{}, false
	}
	var (
		reclaimable int64
		total       int64
		inUse       int
		sized       bool
		created     time.Time
		lastUsed    time.Time
	)
	for _, r := range records {
		size, ok := parseSize(r.Size)
		if ok {
			sized = true
			total += size
		}
		if parseBool(r.InUse) {
			inUse++
			continue
		}
		reclaimable += size
		lastUsed = newest(lastUsed, parseTime(r.LastUsedAt))
		if c := parseTime(r.CreatedAt); !c.IsZero() && (created.IsZero() || c.Before(created)) {
			created = c
		}
	}

	item := Item{
		Kind:       KindBuildCache,
		Name:       "Build cache",
		Size:       reclaimable,
		Created:    created,
		LastUsed:   lastUsed,
		InUse:      inUse > 0,
		Status:     fmt.Sprintf("%d records, %d in use", len(records), inUse),
		BuildCache: buildCacheInfo(records, total, reclaimable, inUse),
		Description: "BuildKit's build cache: the intermediate layers, downloaded build contexts and cache " +
			"mounts kept from previous `docker build` runs so that a rebuild can skip every step whose inputs " +
			"have not changed. It is " + countPhrase(len(records), "record") + " totalling " +
			humanSize(total) + ", and it only ever grows — Docker never trims it on its own. Individual " +
			"records cannot be removed one at a time, so they are shown here as the single pile they are.",
		Effects: "`docker builder prune` removes the records nothing is using. No image, container or volume " +
			"is touched, and nothing has to be pulled from a registry again — the entire cost is time: the " +
			"next build of each project redoes the steps whose cached result was dropped, so expect one slow " +
			"build per project afterwards and normal speed from then on.",
		RemoveCmd: "docker builder prune -f",
	}
	if inUse > 0 {
		item.Verdict = VerdictReview
		item.Effects += " " + countPhrase(inUse, "record") + " here are in use by a build happening right " +
			"now and are left alone by the command above."
	} else {
		item.Verdict = VerdictDisposable
	}
	if !sized {
		item.Size = 0
		item.Description += " Docker reported no sizes for these records, so the figure shown is 0 rather " +
			"than an estimate."
	}
	return item, true
}

// pulledPrefix is how BuildKit describes a record holding an image it
// downloaded during a build.
const pulledPrefix = "pulled from "

// execFragment is what BuildKit puts before the command a cached record is
// the result of, in either "mount / from exec <cmd>" or "cached mount
// /var/cache/apt from exec <cmd>".
const execFragment = "from exec "

// localSourcePrefix marks a record holding a copy of a build context or
// Dockerfile taken from a directory on this machine.
const localSourcePrefix = "local source for"

// buildCacheInfo mines the cache records for the builds that produced them.
//
// This is the only place on the machine that can name a project whose
// image, containers and volumes have all been deleted: BuildKit records
// every reference it pulled and every command it ran, so a private registry
// path in this list is proof that project was built here, months after
// everything else about it is gone.
func buildCacheInfo(records []buildCacheLine, total, reclaimable int64, inUse int) *BuildCacheInfo {
	info := &BuildCacheInfo{
		Records:     len(records),
		InUse:       inUse,
		Total:       total,
		Reclaimable: reclaimable,
	}
	for _, r := range records {
		size, _ := parseSize(r.Size)
		desc := strings.TrimSpace(r.Description)
		usage, _ := parseCount(r.UsageCount)
		shown := desc

		switch {
		case strings.HasPrefix(desc, pulledPrefix):
			// The digest is what makes two pulls of the same tag look like
			// two different things; the reference is what a human knows.
			ref, _, _ := strings.Cut(strings.TrimPrefix(desc, pulledPrefix), "@")
			if ref = strings.TrimSpace(ref); ref != "" {
				shown = pulledPrefix + ref
				if !slices.Contains(info.Pulled, ref) {
					info.Pulled = append(info.Pulled, ref)
				}
			}
		case strings.HasPrefix(desc, localSourcePrefix):
			info.LocalContexts++
		default:
			if prefix, cmd, ok := strings.Cut(desc, execFragment); ok {
				cmd, _ = cleanHistoryCommand(strings.TrimSpace(cmd))
				if cmd != "" {
					shown = strings.TrimSpace(prefix) + " " + cmd
					if !slices.Contains(info.Steps, cmd) {
						info.Steps = append(info.Steps, cmd)
					}
				}
			}
		}

		info.Biggest = append(info.Biggest, BuildCacheRecord{
			Type:        strings.TrimSpace(r.CacheType),
			Description: shown,
			Size:        size,
			InUse:       parseBool(r.InUse),
			Shared:      parseBool(r.Shared),
			UsageCount:  usage,
			LastUsed:    parseTime(r.LastUsedAt),
		})
	}
	slices.Sort(info.Pulled)
	slices.Sort(info.Steps)
	slices.SortStableFunc(info.Biggest, func(a, b BuildCacheRecord) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		default:
			return 0
		}
	})
	if len(info.Biggest) > 8 {
		info.Biggest = info.Biggest[:8]
	}
	return info
}

// sizesUnavailableNote is appended wherever `docker system df` did not
// run, since every size on the screen is 0 in that case and a row claiming
// to be empty would otherwise look like a finding rather than a gap.
const sizesUnavailableNote = " Sizes are unavailable for this scan: `docker system df` did not answer, so " +
	"every size shown is 0. That is missing information, not an empty object."

// detailsUnavailableNote is appended wherever `docker inspect` did not
// run. Without it there are no real timestamps, so nothing can be shown as
// last used and nothing can be judged abandoned — which is why this scan
// is more cautious than usual rather than simply less informative.
const detailsUnavailableNote = " Timestamps are unavailable for this scan: `docker inspect` did not answer, " +
	"so last-used times are blank and nothing here is rated disposable on age alone."

// imageName renders an image's display name: every tag it carries, or
// Docker's own placeholder when it carries none.
func imageName(img *imageFacts) string {
	if len(img.refs) == 0 {
		return noneRef + ":" + noneRef
	}
	return strings.Join(img.refs, ", ")
}

// imageRemoveTarget is what `docker rmi` has to be given to actually get
// rid of the image. Every tag has to be named: `docker rmi` on one tag of
// a multi-tag image only untags it and frees nothing, and on the bare ID
// it refuses outright when more than one repository refers to it. An image
// with no name left is removed by ID, which is the only handle it has.
func imageRemoveTarget(img *imageFacts) string {
	if len(img.refs) == 0 {
		return img.id
	}
	return strings.Join(img.refs, " ")
}

// imageStatus is the short status line for an image, which for an image
// means "who is using it" — the fact that decides its verdict.
func imageStatus(img *imageFacts, projects []string) string {
	if len(img.users) == 0 {
		if img.dangling {
			return "Dangling, unused"
		}
		return "Unused"
	}
	status := "Used by " + countPhrase(len(img.users), "container")
	switch {
	case len(projects) > 1:
		status += " in " + countPhrase(len(projects), "compose project")
	case len(projects) == 1:
		status += " in the " + projects[0] + " project"
	}
	return status
}

// volumeStatus is the short status line for a volume.
//
// What is inside it beats every other fact here, and by a distance. The
// user's complaint about this tab was that a row reading "Anonymous,
// unused — 275.6 MB" is a black box; "Anonymous — MySQL 8 data directory"
// is the same row answering the question.
func volumeStatus(v *volumeFacts, referenced bool) string {
	var status string
	switch {
	case referenced:
		status = "In use by " + countPhrase(max(len(v.users), v.links), "container")
	case v.anonymous:
		status = "Anonymous"
	default:
		status = "Unused"
	}
	if v.info == nil {
		return status
	}
	switch {
	case v.info.Contents.Software != "":
		return status + " — " + v.info.Contents.Software
	case v.info.Contents.Empty:
		return status + " — empty"
	case !referenced && v.anonymous:
		return status + ", unused"
	default:
		return status
	}
}

// distinctProjects lists the compose projects the given containers belong
// to, sorted so the output is stable between scans.
func distinctProjects(containers []*containerFacts) []string {
	var projects []string
	for _, c := range containers {
		if c.project != "" && !slices.Contains(projects, c.project) {
			projects = append(projects, c.project)
		}
	}
	slices.Sort(projects)
	return projects
}

// projectClause renders " in the `foo` compose project" for prose, or
// nothing when the object belongs to no project.
func projectClause(project string) string {
	if project == "" {
		return ""
	}
	return " in the `" + project + "` compose project"
}

// projectsClause names the compose projects an image is used by. Naming
// them is the point when there is more than one: "postgres:16 is used by
// api and billing" is the sentence that stops someone deleting it.
func projectsClause(projects []string) string {
	switch len(projects) {
	case 0:
		return ""
	case 1:
		return " in the `" + projects[0] + "` compose project"
	case 2:
		return ", spread across the compose projects `" + projects[0] + "` and `" + projects[1] + "`"
	default:
		last := len(projects) - 1
		return ", spread across the compose projects `" + strings.Join(projects[:last], "`, `") + "` and `" +
			projects[last] + "`"
	}
}

// countPhrase renders "1 container" / "3 containers".
func countPhrase(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// agoPhrase renders how long ago t was, or an admission that Docker never
// said. Inventing a date would be worse than saying nothing: the whole
// point of the timestamp is deciding whether a container is abandoned.
func agoPhrase(t, now time.Time) string {
	if t.IsZero() {
		return "at a time Docker does not record"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", max(int(d.Minutes()), 1))
	case d < 24*time.Hour:
		return countPhrase(int(d.Hours()), "hour") + " ago"
	case d < 60*24*time.Hour:
		return countPhrase(int(d.Hours()/24), "day") + " ago"
	default:
		return countPhrase(int(d.Hours()/24/30), "month") + " ago"
	}
}

// quoteOrUnknown renders a state string for an error message.
func quoteOrUnknown(state string) string {
	if state == "" {
		return "no state reported at all"
	}
	return `"` + state + `"`
}

// firstName takes the first of the comma-separated names `docker ps`
// prints, since a container may carry network aliases alongside its own
// name and only the first is the name itself.
func firstName(names string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(names), ",")
	return strings.TrimPrefix(name, "/")
}

// humanSize renders a byte count the way Docker does, in decimal units, so
// a figure quoted in this package's prose matches what `docker system df`
// prints for the same object.
func humanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTP"[exp])
}
