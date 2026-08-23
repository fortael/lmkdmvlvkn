package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// This file works out what is actually inside a volume, which is the one
// question `docker inspect` cannot answer and the only one that turns a
// 64-hex-character name into a decision.
//
// The read is a shallow directory listing and nothing else. It is done on
// this machine's own filesystem — no container is started, no image is
// pulled, nothing is written — and it stops at the top level plus a
// handful of named sub-directories whose entries carry provenance. A MySQL
// data directory is recognisable from its top level alone, and its
// sub-directories are the names of the databases it holds, which is
// usually the name of the project the volume belonged to.
//
// The fingerprinting itself is a pure function of an already-collected
// listing (volumeProbe), so the tests describe an imaginary directory and
// need no daemon and no filesystem.

// probeEntryLimit is how many directory entries are read at most. It is
// generous enough that a marker file is never missed and small enough that
// a volume holding a hundred thousand files costs one bounded read.
const probeEntryLimit = 512

// probeDisplayLimit is how many entry names are kept for display.
const probeDisplayLimit = 24

// probeFileLimit caps the marker files read, which are version stamps of a
// few bytes. Nothing larger is ever opened.
const probeFileLimit = 64

// probeSubdirs are the sub-directories worth one extra listing, because
// their entry names identify the project rather than the software: the
// databases a MySQL volume holds, the repositories a Composer cache was
// filled from, the node a RabbitMQ container registered under.
//
// The list is fixed rather than chosen by the fingerprint on purpose. It
// keeps the reader dumb — it collects, it does not interpret — so that all
// the judgement lives in one pure function below.
var probeSubdirs = []string{"mnesia", "repo", "files", "nodes", "data"}

// probeFiles are the tiny marker files worth opening. PG_VERSION is four
// bytes and is the only way to tell which PostgreSQL major version wrote a
// data directory, which decides whether the data is even loadable by
// whatever is installed now.
var probeFiles = []string{"PG_VERSION"}

// volumeProbe is the raw result of looking inside one volume. It exists so
// that collection and interpretation stay separate: everything below
// describeContents is a pure function of this struct.
type volumeProbe struct {
	// path is the directory that was read, empty when none could be.
	path string
	// unavailable explains why nothing was read.
	unavailable string
	// read reports that a listing actually happened, which is different
	// from a listing that came back empty.
	read bool
	// entries are the top-level names, sorted.
	entries []string
	// dirs marks which of them are directories.
	dirs map[string]bool
	// truncated reports that the directory held more than we read.
	truncated bool
	// sub holds the entries of the probeSubdirs that existed.
	sub map[string][]string
	// files holds the contents of the probeFiles that existed.
	files map[string]string
}

// has reports whether the volume's top level contains an entry.
func (p volumeProbe) has(name string) bool {
	return slices.Contains(p.entries, name)
}

// hasAll reports whether every named entry is present.
func (p volumeProbe) hasAll(names ...string) bool {
	for _, n := range names {
		if !p.has(n) {
			return false
		}
	}
	return true
}

// hasAny reports whether at least one of the named entries is present.
func (p volumeProbe) hasAny(names ...string) bool {
	for _, n := range names {
		if p.has(n) {
			return true
		}
	}
	return false
}

// subdirNames returns the directory entries of a probed sub-directory,
// excluding files.
func (p volumeProbe) subdirNames(name string) []string {
	return p.sub[name]
}

// hostVolumeRoots lists the directories on this machine where a Docker
// volume's contents might be readable without going through the daemon.
//
// On Linux the daemon's own mountpoint is the answer. On macOS it is a path
// inside a virtual machine and opening it fails — except under OrbStack,
// which re-exports the VM's volume directory into the user's home folder.
// Docker Desktop exposes nothing, and there this returns nothing and the
// panel says so rather than pretending the volume is empty.
func hostVolumeRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "OrbStack", "docker", "volumes")}
}

// hostVolumePath finds a readable path for one volume, or "" when there is
// none. Note that OrbStack drops the "_data" component the daemon reports,
// so both spellings are tried.
func hostVolumePath(mountpoint, name string) string {
	if mountpoint != "" && isDir(mountpoint) {
		return mountpoint
	}
	if name == "" {
		return ""
	}
	for _, root := range hostVolumeRoots() {
		for _, candidate := range []string{
			filepath.Join(root, name),
			filepath.Join(root, name, "_data"),
		} {
			if isDir(candidate) {
				return candidate
			}
		}
	}
	return ""
}

// isDir reports whether path exists and is a directory, treating every
// error as "no". A permissions error and a missing path lead to the same
// place here: we cannot read it, so we will not claim to know what is in it.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// probeVolume lists one volume's top level and the handful of
// sub-directories worth a second look. It never writes, never follows a
// symlink out of the directory, and never opens a file that is not in
// probeFiles.
func probeVolume(ctx context.Context, mountpoint, name string) volumeProbe {
	if err := ctx.Err(); err != nil {
		return volumeProbe{unavailable: "The scan was cancelled before this volume could be read."}
	}
	path := hostVolumePath(mountpoint, name)
	if path == "" {
		return volumeProbe{unavailable: "A volume's contents live inside the Docker virtual machine, and this " +
			"Docker installation does not expose them to the Mac's own filesystem, so this app cannot see " +
			"what is in there without starting a container — which it will not do on its own."}
	}

	p := volumeProbe{path: path, dirs: map[string]bool{}, sub: map[string][]string{}, files: map[string]string{}}
	dir, err := os.Open(path)
	if err != nil {
		p.unavailable = "This volume's directory could not be opened: " + truncate(firstLine(err.Error()), 160)
		return p
	}
	// Nothing here writes, so a failing Close has nothing to report.
	defer func() { _ = dir.Close() }()

	// ReadDir with a limit rather than os.ReadDir, so a volume holding a
	// hundred thousand files costs one bounded read instead of a full
	// listing we would throw away.
	items, err := dir.ReadDir(probeEntryLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		p.unavailable = "This volume's directory could not be listed: " + truncate(firstLine(err.Error()), 160)
		return p
	}
	p.read = true
	p.truncated = len(items) == probeEntryLimit
	for _, item := range items {
		p.entries = append(p.entries, item.Name())
		if item.IsDir() {
			p.dirs[item.Name()] = true
		}
	}
	slices.Sort(p.entries)

	for _, sub := range probeSubdirs {
		if !p.dirs[sub] || ctx.Err() != nil {
			continue
		}
		if names := readDirNames(filepath.Join(path, sub)); len(names) > 0 {
			p.sub[sub] = names
		}
	}
	for _, file := range probeFiles {
		if !p.has(file) || p.dirs[file] || ctx.Err() != nil {
			continue
		}
		if value := readSmallFile(filepath.Join(path, file)); value != "" {
			p.files[file] = value
		}
	}
	return p
}

// readDirNames lists one sub-directory, bounded and sorted, swallowing
// every error: a sub-directory we cannot read costs a detail row, never the
// scan.
func readDirNames(path string) []string {
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	// Nothing here writes, so a failing Close has nothing to report.
	defer func() { _ = dir.Close() }()
	items, err := dir.ReadDir(probeEntryLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	slices.Sort(names)
	return names
}

// readSmallFile reads a version stamp.
//
// It only ever runs on the names in probeFiles, and it still refuses to
// read more than a few bytes, and still refuses anything that is not a
// small regular file. A volume is data somebody else's container wrote:
// following a symlink out of it and printing what is on the other end is
// not a thing this app should be capable of, even by accident.
func readSmallFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > probeFileLimit {
		return ""
	}
	buf := make([]byte, probeFileLimit)
	n, err := f.Read(buf)
	if n <= 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return ""
	}
	return strings.TrimSpace(string(buf[:n]))
}

// volumeSignature recognises one software's on-disk layout.
//
// Every entry here was written against a real directory rather than from
// memory: the marker names are the files those programs genuinely create,
// and path is the destination their official images declare as a VOLUME —
// which is what lets this package answer "where was it mounted" for a
// volume whose container was deleted months ago.
type volumeSignature struct {
	// software names what wrote the directory.
	software string
	// require are entries that must all be present.
	require []string
	// oneOf are entries of which at least one must be present. An empty
	// list means require alone decides.
	oneOf []string
	// maxEntries caps how many top-level entries the volume may have and
	// still match, for the signatures that are about what is *not* there —
	// a directory holding only the marker file a program writes before it
	// has anything to store. Zero means no cap.
	maxEntries int
	// path is where the software's own image mounts this directory.
	path string
	// hints are fragments of the image repository name that writes this
	// layout, used to point at a matching image still on this machine.
	hints []string
	// summary is the sentence explaining what losing it would cost.
	summary string
	// describe adds what only this layout can say — the databases, the
	// cached repositories, the node name.
	describe func(p volumeProbe, c *VolumeContents)
}

// volumeSignatures are checked in order, most specific first.
var volumeSignatures = []volumeSignature{
	{
		software: "MySQL data directory",
		require:  []string{"ibdata1"},
		oneOf:    []string{"mysql", "auto.cnf", "#innodb_redo", "ib_logfile0", "mysql.ibd"},
		path:     "/var/lib/mysql",
		hints:    []string{"mysql", "mariadb", "percona"},
		summary: "This is a database's own data directory. Nothing else has these bytes: no registry, no " +
			"image, no rebuild brings them back, and whatever rows were in there go with it.",
		describe: describeMySQL,
	},
	{
		software: "PostgreSQL data directory",
		require:  []string{"PG_VERSION"},
		oneOf:    []string{"base", "global", "pg_wal", "pg_xlog", "postgresql.conf"},
		path:     "/var/lib/postgresql/data",
		hints:    []string{"postgres", "postgis", "timescale"},
		summary: "This is a database cluster's own data directory. Nothing else has these bytes: no registry, " +
			"no image, no rebuild brings them back.",
		describe: describePostgres,
	},
	{
		software: "RabbitMQ data directory",
		require:  []string{"mnesia"},
		path:     "/var/lib/rabbitmq",
		hints:    []string{"rabbitmq"},
		summary: "This holds a broker's queues, exchanges and users. Losing it loses any message still sitting " +
			"in a durable queue, and the broker starts up empty as though it had never run.",
		describe: describeRabbitMQ,
	},
	{
		software: "Redis persistence directory",
		require:  []string{},
		oneOf:    []string{"dump.rdb", "appendonly.aof", "appendonlydir"},
		path:     "/data",
		hints:    []string{"redis", "valkey", "keydb"},
		summary: "This is a Redis snapshot or append-only file. Redis is usually a cache, so this is usually " +
			"safe to lose — unless something was using it as a store rather than a cache.",
	},
	{
		software: "MongoDB data directory",
		require:  []string{"WiredTiger"},
		oneOf:    []string{"journal", "_mdb_catalog.wt", "storage.bson", "WiredTiger.wt"},
		path:     "/data/db",
		hints:    []string{"mongo"},
		summary: "This is a MongoDB storage engine's own directory, and it is the only copy of whatever " +
			"collections were in it.",
	},
	{
		software: "Elasticsearch or OpenSearch data directory",
		require:  []string{},
		oneOf:    []string{"nodes", "indices", "node.lock"},
		path:     "/usr/share/elasticsearch/data",
		hints:    []string{"elasticsearch", "opensearch"},
		summary: "This holds a search cluster's indices. They are usually rebuildable from whatever fed them, " +
			"but rebuilding is a reindex, not a download.",
	},
	{
		software: "ClickHouse data directory",
		require:  []string{"metadata"},
		oneOf:    []string{"store", "preprocessed_configs", "format_schemas", "user_files"},
		path:     "/var/lib/clickhouse",
		hints:    []string{"clickhouse"},
		summary:  "This is a column store's own data directory and the only copy of the tables in it.",
	},
	{
		software: "Kafka log directory",
		require:  []string{},
		oneOf:    []string{"meta.properties", "recovery-point-offset-checkpoint", "__consumer_offsets-0"},
		path:     "/var/lib/kafka/data",
		hints:    []string{"kafka", "redpanda"},
		summary: "This holds a broker's partition logs, including consumer offsets. Deleting it resets every " +
			"topic and every consumer group on that broker.",
	},
	{
		software: "Composer package cache",
		require:  []string{"files", "repo"},
		path:     "/tmp/cache",
		hints:    []string{"composer"},
		summary: "This is a PHP dependency cache: downloaded package archives and repository metadata. " +
			"Everything in it can be downloaded again, so losing it costs one slow `composer install` per " +
			"project and nothing else.",
		describe: describeComposerCache,
	},
	{
		// Composer writes a deny-all .htaccess into its cache directory
		// the first time it opens it, before there is anything to cache.
		// A volume holding that and nothing else is a cache mount from a
		// build that never downloaded a package — which on a machine full
		// of anonymous volumes is a common and completely disposable one.
		software:   "Composer cache directory that was never filled",
		require:    []string{".htaccess"},
		maxEntries: 2,
		path:       "/tmp/cache",
		hints:      []string{"composer"},
		summary: "Composer writes a deny-all .htaccess into a cache directory before it caches anything, and " +
			"there is nothing else in here. Nothing was ever stored in this volume.",
	},
	{
		software: "npm package cache",
		require:  []string{},
		oneOf:    []string{"_cacache"},
		path:     "/root/.npm",
		hints:    []string{"node"},
		summary: "This is an npm download cache. Everything in it can be downloaded again, so losing it costs " +
			"one slow install and nothing else.",
	},
	{
		software: "Go module cache",
		require:  []string{"cache"},
		oneOf:    []string{"sumdb", "download"},
		path:     "/go/pkg/mod",
		hints:    []string{"golang"},
		summary: "This is a Go module download cache. Everything in it comes back from the proxy on the next " +
			"build, so losing it costs download time and nothing else.",
	},
	{
		software: "Grafana data directory",
		require:  []string{},
		oneOf:    []string{"grafana.db"},
		path:     "/var/lib/grafana",
		hints:    []string{"grafana"},
		summary: "This holds Grafana's dashboards, users and datasource definitions. Dashboards defined in " +
			"code are reproducible; anything drawn by hand in the UI is not.",
	},
	{
		software: "Prometheus time-series database",
		require:  []string{"wal"},
		oneOf:    []string{"chunks_head", "queries.active"},
		path:     "/prometheus",
		hints:    []string{"prometheus"},
		summary: "This holds collected metrics. History is lost for good; new metrics keep arriving as soon as " +
			"it is scraping again.",
	},
	{
		software: "MinIO object store",
		require:  []string{},
		oneOf:    []string{".minio.sys"},
		path:     "/data",
		hints:    []string{"minio"},
		summary:  "This is an S3-compatible object store's own directory: every bucket and object it held.",
	},
	{
		software: "Jenkins home directory",
		require:  []string{"jobs"},
		oneOf:    []string{"secrets", "config.xml", "plugins"},
		path:     "/var/jenkins_home",
		hints:    []string{"jenkins"},
		summary:  "This holds job definitions, build history and credentials.",
	},
	{
		software: "PhpStorm remote interpreter helpers",
		require:  []string{},
		oneOf:    []string{"phpstorm_bootstrap.php", "phpunit.php", "phpspec.php"},
		path:     "/opt/.phpstorm_helpers",
		hints:    []string{"phpstorm_helpers"},
		summary: "These are the small PHP scripts the IDE copies into a container so it can run tests and a " +
			"debugger inside it. The IDE recreates them the next time it needs them.",
	},
}

// describeContents turns a raw listing into the section the panel shows. It
// is a pure function: everything it knows came from the probe.
func describeContents(p volumeProbe) VolumeContents {
	c := VolumeContents{
		Read:        p.read,
		Path:        p.path,
		Unavailable: p.unavailable,
		Truncated:   p.truncated,
	}
	if !p.read {
		if c.Unavailable == "" {
			// A volume the scan never got to, which is different from one
			// it could not read and different again from an empty one.
			c.Unavailable = "This volume's contents were not examined during this scan."
		}
		return c
	}
	c.Entries = slices.Clone(p.entries)
	if len(c.Entries) > probeDisplayLimit {
		c.Entries = c.Entries[:probeDisplayLimit]
		c.Truncated = true
	}
	if len(p.entries) == 0 {
		c.Empty = true
		c.Summary = "This volume is empty. Whatever created it either never wrote anything or had its data " +
			"removed, so there is nothing here to lose."
		return c
	}

	for _, sig := range volumeSignatures {
		if !p.hasAll(sig.require...) {
			continue
		}
		if len(sig.oneOf) > 0 && !p.hasAny(sig.oneOf...) {
			continue
		}
		if sig.maxEntries > 0 && len(p.entries) > sig.maxEntries {
			continue
		}
		c.Software = sig.software
		c.Summary = sig.summary
		c.ConventionalPath = sig.path
		c.ImageHints = sig.hints
		if sig.describe != nil {
			sig.describe(p, &c)
		}
		return c
	}

	describeUnrecognised(p, &c)
	return c
}

// describeUnrecognised is the honest fallback: name what is obviously there
// and admit that the layout means nothing to this app otherwise.
func describeUnrecognised(p volumeProbe, c *VolumeContents) {
	switch {
	case p.has(".git"):
		c.Software = "a checked-out git repository"
		c.Summary = "A working copy of a repository. If it has been pushed, nothing here is unique; if it has " +
			"not, this is the only copy of those commits."
	case p.has("node_modules"):
		c.Software = "a JavaScript project's installed dependencies"
		c.Summary = "Installed packages, all of them reinstallable from the project's lockfile."
	case p.has("vendor") && p.has("composer.json"):
		c.Software = "a PHP project's installed dependencies"
		c.Summary = "Installed packages, all of them reinstallable from the project's lockfile."
	default:
		c.Summary = "The layout of this volume does not match anything this app recognises, so the entry " +
			"names above are all there is to go on. They are usually enough to recognise a project you wrote."
	}
}

// mysqlSystemDirs are the directories every MySQL data directory has,
// which are exactly the ones that say nothing about whose data this is.
var mysqlSystemDirs = map[string]bool{
	"mysql": true, "sys": true, "performance_schema": true, "information_schema": true,
}

// describeMySQL names the databases in a MySQL data directory.
//
// This is the single most valuable thing in this file. MySQL stores each
// database as a directory named after it, so a volume whose container was
// deleted six months ago still says which project's data it holds — which
// is the whole question the user has when looking at a hash.
func describeMySQL(p volumeProbe, c *VolumeContents) {
	switch {
	case p.has("aria_log_control"):
		c.Software = "MariaDB data directory"
	case p.has("mysql.ibd"):
		// The system tablespace moved into mysql.ibd in MySQL 8.0.
		c.Software = "MySQL 8 data directory"
	default:
		c.Software = "MySQL 5 data directory"
	}

	var databases []string
	for _, name := range p.entries {
		if !p.dirs[name] || strings.HasPrefix(name, "#") || mysqlSystemDirs[name] {
			continue
		}
		databases = append(databases, mysqlDecodeName(name))
	}
	if len(databases) == 0 {
		c.Summary = "A database data directory holding only MySQL's own system schemas — no application " +
			"database was ever created in it, which makes it the residue of a container that started and was " +
			"thrown away."
		return
	}
	c.Facts = append(c.Facts, DetailRow{
		Label: "Databases",
		Value: strings.Join(databases, ", "),
	})
	c.Summary = "A database data directory holding " + countPhrase(len(databases), "application database") +
		". Those names are the project this volume belonged to. Nothing else has these bytes: no registry, " +
		"no image, no rebuild brings them back."
}

// mysqlEscapeRe matches MySQL's filename encoding for a character that is
// not allowed in a directory name: "@" followed by the four hex digits of
// the code point. A database called "clip-plus-service" is stored as
// "clip@002dplus@002dservice", and showing the raw form would hide exactly
// the name the user is looking for.
var mysqlEscapeRe = regexp.MustCompile(`@([0-9a-fA-F]{4})`)

// mysqlDecodeName turns a MySQL directory name back into the database name.
func mysqlDecodeName(name string) string {
	if !strings.Contains(name, "@") {
		return name
	}
	return mysqlEscapeRe.ReplaceAllStringFunc(name, func(m string) string {
		code, err := strconv.ParseUint(m[1:], 16, 32)
		if err != nil || code == 0 {
			return m
		}
		return string(rune(code))
	})
}

// describePostgres reads the four-byte PG_VERSION stamp, which is the only
// thing in a PostgreSQL data directory that says which major version wrote
// it — and therefore whether anything currently installed could still open
// it.
func describePostgres(p volumeProbe, c *VolumeContents) {
	version := p.files["PG_VERSION"]
	if version == "" {
		return
	}
	c.Software = "PostgreSQL " + version + " data directory"
	c.Facts = append(c.Facts, DetailRow{
		Label: "Server version",
		Value: "PostgreSQL " + version + " — only that major version can open this directory",
	})
}

// rabbitNodeRe matches an Erlang node directory, "rabbit@<hostname>".
var rabbitNodeRe = regexp.MustCompile(`^([a-z][a-z0-9_]*)@([A-Za-z0-9][A-Za-z0-9_.-]*)$`)

// mnesiaSuffixes are the sibling directories RabbitMQ creates beside its
// node directory. They carry the node name plus one of these, and treating
// them as separate nodes would list the same broker three times.
var mnesiaSuffixes = []string{"-feature_flags", "-plugins-expand", "-cluster", "-rename", "-upgrade", "-quorum"}

// containerHostnameRe matches the hostname Docker gives a container when
// nothing else does: its own short ID.
var containerHostnameRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// describeRabbitMQ recovers the identity of the container that wrote the
// volume.
//
// RabbitMQ names its Mnesia directory after the Erlang node, and the node
// name is "rabbit@" plus the machine's hostname — which inside a container
// nobody named is the container's own short ID. So a volume whose container
// was deleted long ago still carries that container's ID, and this is the
// one case in the whole package where a deleted container can be named.
func describeRabbitMQ(p volumeProbe, c *VolumeContents) {
	var nodes []string
	for _, name := range p.subdirNames("mnesia") {
		for _, suffix := range mnesiaSuffixes {
			name = strings.TrimSuffix(name, suffix)
		}
		if !rabbitNodeRe.MatchString(name) || slices.Contains(nodes, name) {
			continue
		}
		nodes = append(nodes, name)
	}
	if len(nodes) == 0 {
		return
	}
	c.Facts = append(c.Facts, DetailRow{Label: "Erlang node", Value: strings.Join(nodes, ", ")})
	for _, node := range nodes {
		_, host, _ := strings.Cut(node, "@")
		if !containerHostnameRe.MatchString(host) {
			continue
		}
		c.Facts = append(c.Facts, DetailRow{
			Label: "Written by container",
			Value: host + " — the broker recorded its node as `" + node + "`, and a container's hostname is " +
				"its own short ID unless something set one, so that is the container that created this volume",
		})
	}
}

// composerRepoPrefixRe matches the scheme Composer mangles into a cache
// directory name, so the host is readable again.
var composerRepoPrefixRe = regexp.MustCompile(`^(https?)---`)

// describeComposerCache names the package repositories the cache was filled
// from. A private registry in that list is a strong hint about which
// project's build left the volume behind — a GitLab group ID is enough to
// recognise the work.
func describeComposerCache(p volumeProbe, c *VolumeContents) {
	repos := p.subdirNames("repo")
	if len(repos) > 0 {
		pretty := make([]string, 0, len(repos))
		for _, r := range repos {
			pretty = append(pretty, composerRepoPrefixRe.ReplaceAllString(r, "$1://"))
		}
		c.Facts = append(c.Facts, DetailRow{
			Label: "Repositories cached",
			Value: strings.Join(capLines(pretty, 6), ", "),
		})
	}
	if vendors := p.subdirNames("files"); len(vendors) > 0 {
		c.Facts = append(c.Facts, DetailRow{
			Label: "Packages from",
			Value: strings.Join(capLines(vendors, 8), ", "),
		})
	}
}

// signatureFor finds the signature matching a mount path, an image
// reference, or both.
//
// It runs the recognition backwards: instead of "these files look like
// MySQL", it is "this was mounted at /var/lib/mysql by an image called
// mysql:8, so it is a MySQL data directory". That is the only inference
// available when the volume's contents cannot be read at all — which is
// every Docker Desktop installation — and it is a good one, because the
// path and the image name are both recorded facts rather than guesses.
func signatureFor(destination, imageRef string) (volumeSignature, bool) {
	if destination != "" {
		for _, sig := range volumeSignatures {
			if sig.path == destination {
				return sig, true
			}
		}
	}
	if repo, _, _ := strings.Cut(imageRef, ":"); repo != "" {
		lower := strings.ToLower(repo)
		for _, sig := range volumeSignatures {
			for _, hint := range sig.hints {
				if strings.Contains(lower, hint) {
					return sig, true
				}
			}
		}
	}
	return volumeSignature{}, false
}

// inferContents fills in what a volume probably holds when the volume
// itself could not be read, using only what Docker recorded about the
// container that mounted it. Everything it produces is labelled as an
// inference by the panel, because that is what it is.
func inferContents(c *VolumeContents, destination, imageRef string) {
	if c.Software != "" {
		return
	}
	sig, ok := signatureFor(destination, imageRef)
	if !ok {
		return
	}
	c.Software = sig.software
	c.ConventionalPath = sig.path
	c.ImageHints = sig.hints
	c.Inferred = true
	c.Summary = sig.summary + " That is read off the mount path and the image name — the volume's own " +
		"contents could not be examined."
}

// contentsImages returns the images on this machine whose repository name
// matches what the contents say wrote this volume. It is how the panel gets
// from "a MySQL data directory" to "and the mysql:8 image is still here",
// without ever claiming a match that is not there.
func contentsImages(c VolumeContents, imageRefs []string) []string {
	if len(c.ImageHints) == 0 {
		return nil
	}
	var out []string
	for _, ref := range imageRefs {
		repo, _, _ := strings.Cut(ref, ":")
		lower := strings.ToLower(repo)
		for _, hint := range c.ImageHints {
			if strings.Contains(lower, hint) && !slices.Contains(out, ref) {
				out = append(out, ref)
				break
			}
		}
	}
	return out
}

// listImages are the tiny images this package is willing to start a
// container from, in preference order, for the on-demand read below.
var listImages = []string{"busybox:latest", "alpine:latest", "busybox", "alpine"}

// FetchVolumeContents lists a volume's top-level entries by starting a
// throwaway container that mounts it read-only.
//
// This is NOT part of Scan and is never called by it. Scan only ever reads
// the host filesystem; this function starts a container, which is a side
// effect a scan has no business having. It exists for the one case Scan
// cannot cover — a Docker installation that keeps its volumes inside a
// virtual machine and exposes nothing, which is Docker Desktop — and the
// UI should offer it as something the user asks for, not as something that
// happens to them.
//
// It refuses to pull: the container is started from a small image that is
// already on this machine, and if there is none it says so rather than
// downloading one. The command is `ls -A` on a read-only mount, so nothing
// in the volume can be modified by it, and --rm means the container is gone
// before this returns.
func FetchVolumeContents(ctx context.Context, name string) ([]string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("no volume name given")
	}
	bin, err := dockerPath()
	if err != nil {
		return nil, errors.New("the docker command is not installed, or is not on this app's PATH")
	}
	image := localListImage(ctx, bin)
	if image == "" {
		return nil, fmt.Errorf("reading this volume needs a small image to start a container from, and none of "+
			"%s is on this machine. Pull one first — this app will not download an image on your behalf",
			strings.Join(listImages[:2], " or "))
	}
	out, err := run(ctx, bin, "run", "--rm", "--network", "none",
		"-v", name+":/maccleaner-volume:ro", image, "ls", "-A", "/maccleaner-volume")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	slices.Sort(names)
	return names, nil
}

// localListImage picks an image already present that FetchVolumeContents
// can start, or "" when there is none.
func localListImage(ctx context.Context, bin string) string {
	for _, ref := range listImages {
		if _, err := run(ctx, bin, "image", "inspect", "--format", "{{.Id}}", ref); err == nil {
			return ref
		}
	}
	return ""
}
