package docker

import (
	"strings"
	"testing"
	"time"
)

// The tests in this file cover the half of the package that turns Docker's
// output into an explanation: history parsing, volume-content
// fingerprinting, and the detail panel itself. Everything here is a pure
// function over recorded output, so none of it needs a daemon.

// containsRow reports whether a section list has a row whose label matches
// and whose value contains want.
func containsRow(sections []DetailSection, label, want string) bool {
	for _, s := range sections {
		for _, r := range s.Rows {
			if r.Label == label && strings.Contains(r.Value, want) {
				return true
			}
		}
	}
	return false
}

// sectionTitled returns the named section, or the zero value.
func sectionTitled(sections []DetailSection, title string) DetailSection {
	for _, s := range sections {
		if s.Title == title {
			return s
		}
	}
	return DetailSection{}
}

// allText flattens a panel so a test can assert that a fact appears
// somewhere without pinning which row it landed in.
func allText(sections []DetailSection) string {
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(s.Title + "\n")
		for _, r := range s.Rows {
			b.WriteString(r.Label + ": " + r.Value + "\n")
		}
		for _, l := range s.Lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// Docker records a build step as the shell invocation the daemon ran, in
// one of two spellings depending on which builder made the image, and
// neither is what anybody wrote in a Dockerfile. Getting back to something
// close to the original line is the whole point of showing layers at all.
func TestCleanHistoryCommand(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		want      string
		wantEmpty bool
	}{
		{
			name:      "classic builder marks a metadata step with #(nop)",
			in:        `/bin/sh -c #(nop)  CMD ["php" "-a"]`,
			want:      `CMD ["php" "-a"]`,
			wantEmpty: true,
		},
		{
			name:      "classic ENV keeps its value",
			in:        `/bin/sh -c #(nop)  ENV PHP_VERSION=7.4.33`,
			want:      `ENV PHP_VERSION=7.4.33`,
			wantEmpty: true,
		},
		{
			// The content digest identifies the copied bytes to the daemon
			// and is pure noise to a reader.
			name:      "classic COPY hides the content digest",
			in:        `/bin/sh -c #(nop) COPY file:ce57c04b70896f77cc11eb2766417d8a1240fcffe5bba92179ec78c458844110 in /usr/local/bin/ `,
			want:      `COPY <file ce57c04b…> in /usr/local/bin/`,
			wantEmpty: true,
		},
		{
			name:      "classic multi-file COPY",
			in:        `/bin/sh -c #(nop) COPY multi:6edd033b037aa2d7697fc3b9f82c2f162146c1920a0c6d25a165dc56783204db in /usr/local/bin/ `,
			want:      `COPY <several files> in /usr/local/bin/`,
			wantEmpty: true,
		},
		{
			// Everything the classic builder ran through a shell was a RUN,
			// and it records no verb for it at all.
			name: "classic RUN gets its verb back",
			in:   `/bin/sh -c docker-php-ext-enable sodium`,
			want: `RUN docker-php-ext-enable sodium`,
		},
		{
			name: "buildkit strips its own marker and shell wrapper",
			in:   `RUN /bin/sh -c docker-php-ext-enable opcache # buildkit`,
			want: `RUN docker-php-ext-enable opcache`,
		},
		{
			name: "buildkit COPY keeps its arguments",
			in:   `COPY docker-php-ext-* docker-php-entrypoint /usr/local/bin/ # buildkit`,
			want: `COPY docker-php-ext-* docker-php-entrypoint /usr/local/bin/`,
		},
		{
			// BuildKit puts the step's build arguments in front of the
			// shell wrapper. They are the builder's bookkeeping.
			name: "buildkit strips the build-argument prefix",
			in:   `RUN |1 TARGETPLATFORM=linux/arm64 /bin/sh -c git config --global --add safe.directory '*' # buildkit`,
			want: `RUN git config --global --add safe.directory '*'`,
		},
		{
			// A --no-trunc RUN arrives with the line continuations of the
			// original Dockerfile flattened into runs of spaces.
			name: "whitespace from a multi-line RUN is collapsed",
			in:   "RUN /bin/sh -c set -eux;   apt-get update;  apt-get install -y   curl # buildkit",
			want: "RUN set -eux; apt-get update; apt-get install -y curl",
		},
		{
			// The bottom of a distro image is a comment, not an
			// instruction, and it is the most identifying line in the
			// whole history — it must survive untouched.
			name: "a rootfs comment is left alone",
			in:   `# debian.sh --arch 'arm64' out/ 'trixie' '@1783900800'`,
			want: `# debian.sh --arch 'arm64' out/ 'trixie' '@1783900800'`,
		},
		{name: "empty", in: "", want: ""},
		{name: "only whitespace", in: "   \t ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, empty := cleanHistoryCommand(tt.in)
			if got != tt.want {
				t.Errorf("cleanHistoryCommand(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			if empty != tt.wantEmpty {
				t.Errorf("cleanHistoryCommand(%q) empty = %v, want %v", tt.in, empty, tt.wantEmpty)
			}
		})
	}
}

// The Dockerfile verb is what lets the panel keep the steps that build
// something and drop the ones that only set metadata.
func TestHistoryInstruction(t *testing.T) {
	tests := []struct{ in, want string }{
		{"RUN apt-get update", "RUN"},
		{`CMD ["php" "-a"]`, "CMD"},
		{"COPY a b", "COPY"},
		{"WORKDIR /go", "WORKDIR"},
		{"VOLUME [/var/lib/mysql]", "VOLUME"},
		{"# debian.sh --arch 'arm64' out/ 'trixie'", ""},
		{"BusyBox 1.38.0 (glibc), Debian 13", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := historyInstruction(tt.in); got != tt.want {
			t.Errorf("historyInstruction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// realHistory is recorded `docker history --no-trunc --format '{{json .}}'`
// output for php:8.3-cli, cut down to the steps that matter. Docker prints
// newest first; this package stores oldest first, because "what do the
// first layers build" is the question being asked.
const realHistory = `{"Comment":"buildkit.dockerfile.v0","CreatedAt":"2026-07-14T04:34:31+03:00","CreatedBy":"CMD [\"php\" \"-a\"]","CreatedSince":"5 weeks ago","ID":"sha256:2a3f699b6cb3","Size":"0B"}
{"Comment":"buildkit.dockerfile.v0","CreatedAt":"2026-07-14T04:34:31+03:00","CreatedBy":"RUN /bin/sh -c docker-php-ext-enable sodium # buildkit","CreatedSince":"5 weeks ago","ID":"\u003cmissing\u003e","Size":"4.1kB"}
{"Comment":"buildkit.dockerfile.v0","CreatedAt":"2026-07-14T04:23:11+03:00","CreatedBy":"ENV PHP_VERSION=8.3.32","CreatedSince":"5 weeks ago","ID":"\u003cmissing\u003e","Size":"0B"}
{"Comment":"buildkit.dockerfile.v0","CreatedAt":"2026-07-14T04:23:11+03:00","CreatedBy":"RUN /bin/sh -c set -eux;  apt-get update;  apt-get install -y --no-install-recommends   $PHPIZE_DEPS   ca-certificates   curl   xz-utils  ;  apt-get dist-clean # buildkit","CreatedSince":"5 weeks ago","ID":"\u003cmissing\u003e","Size":"355MB"}
{"Comment":"debuerreotype 0.17","CreatedAt":"2026-07-13T03:00:00+03:00","CreatedBy":"# debian.sh --arch 'arm64' out/ 'trixie' '@1783900800'","CreatedSince":"5 weeks ago","ID":"\u003cmissing\u003e","Size":"108MB"}`

func TestParseHistory(t *testing.T) {
	layers, err := parseHistory([]byte(realHistory))
	if err != nil {
		t.Fatalf("parseHistory: %v", err)
	}
	if len(layers) != 5 {
		t.Fatalf("got %d layers, want 5", len(layers))
	}

	// Build order, not Docker's print order.
	if !strings.HasPrefix(layers[0].Command, "# debian.sh") {
		t.Errorf("layers[0] = %q; the oldest step must come first, so the list reads like the Dockerfile",
			layers[0].Command)
	}
	if !strings.HasPrefix(layers[len(layers)-1].Command, "CMD") {
		t.Errorf("layers[last] = %q, want the newest step", layers[len(layers)-1].Command)
	}
	if layers[0].Size != 108_000_000 {
		t.Errorf("layers[0].Size = %d, want 108 MB", layers[0].Size)
	}
	if layers[0].Instruction != "" {
		t.Errorf("layers[0].Instruction = %q; a rootfs comment is not an instruction", layers[0].Instruction)
	}
	if layers[0].Created.IsZero() {
		t.Error("layers[0].Created is zero; the history carries a timestamp per step")
	}

	// A zero-size step is metadata even when the builder wrote no #(nop)
	// marker, which BuildKit never does.
	for _, l := range layers {
		if strings.HasPrefix(l.Command, "ENV ") && !l.Empty {
			t.Errorf("%q is not marked empty, but it added nothing to the image", l.Command)
		}
		if strings.HasPrefix(l.Command, "RUN set -eux") && l.Empty {
			t.Errorf("%q is marked empty, but it added 355 MB", l.Command)
		}
	}
}

// Docker occasionally interleaves a warning into stdout, and a single
// unreadable line must not cost the whole history.
func TestParseHistoryTolerance(t *testing.T) {
	t.Run("one bad line among good ones", func(t *testing.T) {
		in := []byte(`{"CreatedBy":"RUN a","Size":"1MB"}` + "\nnot json\n" + `{"CreatedBy":"RUN b","Size":"2MB"}`)
		layers, err := parseHistory(in)
		if err != nil {
			t.Fatalf("parseHistory: %v", err)
		}
		if len(layers) != 2 {
			t.Errorf("got %d layers, want the two readable steps", len(layers))
		}
	})
	t.Run("output with nothing readable is an error, not an empty image", func(t *testing.T) {
		if _, err := parseHistory([]byte("Error response from daemon: no such image\n")); err == nil {
			t.Error("parseHistory accepted output with no readable record")
		}
	})
	t.Run("no output at all", func(t *testing.T) {
		layers, err := parseHistory(nil)
		if err != nil || len(layers) != 0 {
			t.Errorf("parseHistory(nil) = (%v, %v), want (empty, nil)", layers, err)
		}
	})
	t.Run("missing and mistyped fields do not panic", func(t *testing.T) {
		layers, err := parseHistory([]byte(`{"CreatedBy":null,"Size":{"bytes":5},"CreatedAt":"whenever"}`))
		if err != nil {
			t.Fatalf("parseHistory: %v", err)
		}
		if len(layers) != 1 || layers[0].Command != "" || layers[0].SizeKnown {
			t.Errorf("parseHistory = %+v, want one blank layer with no known size", layers)
		}
	})
}

// The size breakdown is the answer to "why is this image 1.3 GB", so it has
// to be ordered by size and free of the zero-byte metadata steps that
// outnumber the real ones three to one.
func TestBiggestLayersOrdering(t *testing.T) {
	layers := []Layer{
		{Command: "ENV PHP_VERSION=8.3.32", Size: 0, Empty: true},
		{Command: "RUN small", Size: 4_100},
		{Command: "# debian.sh", Size: 108_000_000},
		{Command: "RUN big", Size: 355_000_000},
		{Command: "CMD [\"php\"]", Size: 0, Empty: true},
	}
	got := biggestLayers(layers, 6)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want the 3 steps that added bytes:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "RUN big") || !strings.Contains(got[1], "# debian.sh") ||
		!strings.Contains(got[2], "RUN small") {
		t.Errorf("layers are not ordered biggest first:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "355.0 MB") {
		t.Errorf("the size is missing from %q", got[0])
	}
	if len(biggestLayers(layers, 1)) != 1 {
		t.Error("biggestLayers ignored its limit")
	}
	if got := biggestLayers(nil, 6); len(got) != 0 {
		t.Errorf("biggestLayers(nil) = %v, want nothing", got)
	}
}

// The first steps are what identify a build. Metadata steps are not steps
// in that sense, and an unrecognised instruction is kept, because the
// bottom entry of a distro image is a comment and the most useful line
// there is.
func TestBuildStepsKeepsWorkAndDropsMetadata(t *testing.T) {
	layers := []Layer{
		{Command: "# debian.sh --arch 'arm64' out/ 'trixie'", Size: 108_000_000},
		{Command: "ENV PHPIZE_DEPS=autoconf", Instruction: "ENV", Empty: true},
		{Command: "RUN apt-get update", Instruction: "RUN", Size: 355_000_000},
		{Command: "WORKDIR /go", Instruction: "WORKDIR", Empty: true},
		{Command: `CMD ["php"]`, Instruction: "CMD", Empty: true},
		{Command: "COPY a /b", Instruction: "COPY", Size: 100},
	}
	got := buildSteps(layers, 10)
	want := []string{"# debian.sh", "RUN apt-get update", "WORKDIR /go", "COPY a /b"}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("step %d = %q, want it to contain %q", i, got[i], w)
		}
	}
	if strings.Contains(strings.Join(got, "\n"), "PHPIZE_DEPS") {
		t.Error("an ENV step got into the build steps; those belong in the stack section")
	}
	if n := len(buildSteps(layers, 2)); n != 2 {
		t.Errorf("buildSteps returned %d steps despite a limit of 2", n)
	}
}

// The distribution at the bottom of an image is read off the layer that
// imported its root filesystem. The Debian case has a trap in it: the first
// quoted word in that comment is the architecture, and matching it would
// report every image on the machine as running "Debian arm64".
func TestBaseSystem(t *testing.T) {
	tests := []struct {
		name   string
		layers []Layer
		want   string
	}{
		{
			name:   "debian names the suite, not the architecture",
			layers: []Layer{{Command: `# debian.sh --arch 'arm64' out/ 'trixie' '@1783900800'`}},
			want:   "Debian trixie",
		},
		{
			name:   "ubuntu",
			layers: []Layer{{Command: `# ubuntu.sh --arch 'amd64' out/ 'noble' '@1700000000'`}},
			want:   "Ubuntu noble",
		},
		{
			name:   "alpine names the version from the rootfs tarball",
			layers: []Layer{{Command: "ADD alpine-minirootfs-3.21.2-aarch64.tar.gz /", Instruction: "ADD"}},
			want:   "Alpine Linux 3.21.2",
		},
		{
			name:   "busybox states it outright",
			layers: []Layer{{Command: "BusyBox 1.38.0 (glibc), Debian 13"}},
			want:   "BusyBox 1.38.0 (glibc), Debian 13",
		},
		{
			name:   "an anonymous rootfs import says nothing rather than guessing",
			layers: []Layer{{Command: "ADD <file abcdef12…> in / ", Instruction: "ADD"}},
			want:   "",
		},
		{
			// A build step that downloads a rootfs tarball is not the base.
			name: "a later step mentioning a rootfs is not mistaken for the base",
			layers: []Layer{
				{Command: "RUN apk add --no-cache curl", Instruction: "RUN"},
				{Command: "RUN curl -O alpine-minirootfs-3.21.2-aarch64.tar.gz", Instruction: "RUN"},
			},
			want: "",
		},
		{name: "no history at all", layers: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := baseSystem(tt.layers); got != tt.want {
				t.Errorf("baseSystem() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPackageManager(t *testing.T) {
	if got := packageManager([]Layer{{Command: "RUN apk add --no-cache ca-certificates", Instruction: "RUN"}}); got != "Alpine (apk)" {
		t.Errorf("packageManager() = %q, want the Alpine family", got)
	}
	if got := packageManager([]Layer{{Command: "RUN apt-get install -y curl", Instruction: "RUN"}}); !strings.Contains(got, "Debian") {
		t.Errorf("packageManager() = %q, want the Debian family", got)
	}
	// A COPY mentioning apk is not an install.
	if got := packageManager([]Layer{{Command: "COPY apk add /tmp", Instruction: "COPY"}}); got != "" {
		t.Errorf("packageManager() = %q, want nothing", got)
	}
}

// The environment is what identifies an unlabelled image's stack, but only
// a few variables of it. PATH is noise, and anything that looks like a
// credential must never reach the screen.
func TestIdentifyingEnv(t *testing.T) {
	rows := identifyingEnv([]string{
		"PATH=/usr/local/bin:/usr/bin",
		"PHP_VERSION=8.3.32",
		"GOLANG_VERSION=1.26.0",
		"PHP_INI_DIR=/usr/local/etc/php",
		"GPG_KEYS=1198C0117593497A5EC5C199286AF1F9897469DC",
		"MYSQL_ROOT_PASSWORD=hunter2",
		"NPM_TOKEN=abc",
		"DEBIAN_FRONTEND=noninteractive",
		"notanassignment",
	})
	got := map[string]string{}
	for _, r := range rows {
		got[r.Label] = r.Value
	}
	for _, want := range []string{"PHP_VERSION", "GOLANG_VERSION", "PHP_INI_DIR"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s is missing; it is what says which stack this image is", want)
		}
	}
	for _, unwanted := range []string{"PATH", "DEBIAN_FRONTEND", "GPG_KEYS", "MYSQL_ROOT_PASSWORD", "NPM_TOKEN"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%s reached the panel; it is either noise or a secret", unwanted)
		}
	}
	// Stable order, so the panel does not reshuffle between scans.
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Label > rows[i].Label {
			t.Errorf("env rows are not sorted: %q before %q", rows[i-1].Label, rows[i].Label)
		}
	}
}

// MySQL stores each database as a directory named after it, which is why a
// volume whose container was deleted months ago can still say whose data it
// holds. The name is escaped on disk, and showing the raw form would hide
// exactly the name the user is looking for.
func TestMysqlDecodeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"clip@002dplus@002dservice", "clip-plus-service"},
		{"customer@002dservice@002dtest", "customer-service-test"},
		{"plain_name", "plain_name"},
		{"with@0020space", "with space"},
		// Nothing that is not a complete escape is touched.
		{"at@sign", "at@sign"},
		{"@zzzz", "@zzzz"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := mysqlDecodeName(tt.in); got != tt.want {
			t.Errorf("mysqlDecodeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// probeOf builds a volumeProbe the way a real read would have, for the
// fingerprinting tests.
func probeOf(dirs []string, files ...string) volumeProbe {
	p := volumeProbe{path: "/tmp/fake", read: true, dirs: map[string]bool{},
		sub: map[string][]string{}, files: map[string]string{}}
	for _, d := range dirs {
		p.entries = append(p.entries, d)
		p.dirs[d] = true
	}
	p.entries = append(p.entries, files...)
	return p
}

// The single most valuable thing this package does: turn 275 MB of hash
// into the name of the project whose database it is.
func TestDescribeContentsMySQL(t *testing.T) {
	p := probeOf(
		[]string{"#innodb_redo", "#innodb_temp", "mysql", "performance_schema", "sys",
			"clip@002dplus@002dservice", "clip@002dplus@002dservice@002dtest"},
		"ibdata1", "mysql.ibd", "auto.cnf", "binlog.index",
	)
	c := describeContents(p)

	if c.Software != "MySQL 8 data directory" {
		t.Errorf("Software = %q, want the MySQL 8 layout", c.Software)
	}
	var databases string
	for _, f := range c.Facts {
		if f.Label == "Databases" {
			databases = f.Value
		}
	}
	if databases != "clip-plus-service, clip-plus-service-test" {
		t.Errorf("Databases = %q, want the two decoded names and nothing else", databases)
	}
	for _, system := range []string{"performance_schema", "innodb", "sys"} {
		if strings.Contains(databases, system) {
			t.Errorf("Databases lists MySQL's own %s schema, which says nothing about whose data this is", system)
		}
	}
	if c.ConventionalPath != "/var/lib/mysql" {
		t.Errorf("ConventionalPath = %q; the mount path is what makes an anonymous volume intelligible",
			c.ConventionalPath)
	}
	if !strings.Contains(c.Summary, "no rebuild brings them back") {
		t.Errorf("Summary does not warn that the data is irreplaceable.\nGot: %s", c.Summary)
	}

	t.Run("MariaDB is named apart", func(t *testing.T) {
		p := probeOf([]string{"mysql"}, "ibdata1", "aria_log_control", "ib_logfile0")
		if got := describeContents(p).Software; got != "MariaDB data directory" {
			t.Errorf("Software = %q, want MariaDB", got)
		}
	})
	t.Run("a data directory with no application database says so", func(t *testing.T) {
		p := probeOf([]string{"mysql", "sys", "performance_schema"}, "ibdata1", "mysql.ibd")
		c := describeContents(p)
		if len(c.Facts) != 0 {
			t.Errorf("Facts = %v, want none: there are no application databases here", c.Facts)
		}
		if !strings.Contains(c.Summary, "no application database") {
			t.Errorf("Summary does not say the directory holds only system schemas.\nGot: %s", c.Summary)
		}
	})
}

// PG_VERSION is four bytes and decides whether anything installed now could
// still open the directory.
func TestDescribeContentsPostgres(t *testing.T) {
	p := probeOf([]string{"base", "global", "pg_wal"}, "PG_VERSION", "postgresql.conf")
	p.files["PG_VERSION"] = "16"
	c := describeContents(p)

	if c.Software != "PostgreSQL 16 data directory" {
		t.Errorf("Software = %q, want the version read from PG_VERSION", c.Software)
	}
	if c.ConventionalPath != "/var/lib/postgresql/data" {
		t.Errorf("ConventionalPath = %q", c.ConventionalPath)
	}

	t.Run("an unreadable PG_VERSION still identifies the software", func(t *testing.T) {
		p := probeOf([]string{"base", "global"}, "PG_VERSION")
		if got := describeContents(p).Software; got != "PostgreSQL data directory" {
			t.Errorf("Software = %q, want the unversioned name", got)
		}
	})
}

// RabbitMQ's node directory carries the hostname of the container that ran
// it, and inside a container nobody named that hostname is the container's
// own short ID. It is the one case in the package where a container that
// was deleted months ago can still be named.
func TestDescribeContentsRabbitMQ(t *testing.T) {
	p := probeOf([]string{"mnesia"}, ".erlang.cookie")
	p.sub["mnesia"] = []string{
		"rabbit@8c2a98b78850",
		"rabbit@8c2a98b78850-feature_flags",
		"rabbit@8c2a98b78850-plugins-expand",
	}
	c := describeContents(p)

	if c.Software != "RabbitMQ data directory" {
		t.Errorf("Software = %q", c.Software)
	}
	facts := map[string]string{}
	for _, f := range c.Facts {
		facts[f.Label] = f.Value
	}
	if facts["Erlang node"] != "rabbit@8c2a98b78850" {
		t.Errorf("Erlang node = %q; the sibling directories are the same node, not three of them",
			facts["Erlang node"])
	}
	if !strings.Contains(facts["Written by container"], "8c2a98b78850") {
		t.Errorf("the creating container was not recovered from the node name: %q", facts["Written by container"])
	}

	t.Run("a hostname somebody set is not claimed to be a container ID", func(t *testing.T) {
		p := probeOf([]string{"mnesia"}, ".erlang.cookie")
		p.sub["mnesia"] = []string{"rabbit@broker.internal"}
		for _, f := range describeContents(p).Facts {
			if f.Label == "Written by container" {
				t.Errorf("claimed %q is a container ID", f.Value)
			}
		}
	})
}

// A Composer cache names the repositories it was filled from, and a private
// registry in that list is a strong hint about which project's build left
// the volume behind.
func TestDescribeContentsComposerCache(t *testing.T) {
	p := probeOf([]string{"files", "repo"}, ".htaccess")
	p.sub["repo"] = []string{"https---gitlab.com-api-v4-group-86467520---packages-composer", "https---repo.packagist.org"}
	p.sub["files"] = []string{"symfony", "psr"}
	c := describeContents(p)

	if c.Software != "Composer package cache" {
		t.Errorf("Software = %q", c.Software)
	}
	text := ""
	for _, f := range c.Facts {
		text += f.Label + ": " + f.Value + "\n"
	}
	if !strings.Contains(text, "https://repo.packagist.org") {
		t.Errorf("the cached repositories are missing or not readable:\n%s", text)
	}
	if !strings.Contains(text, "gitlab.com") {
		t.Errorf("the private registry is missing, and it is the part that identifies the project:\n%s", text)
	}
	if !strings.Contains(c.Summary, "downloaded again") {
		t.Errorf("Summary does not say the cache is reproducible.\nGot: %s", c.Summary)
	}

	t.Run("a cache that was never filled is recognised too", func(t *testing.T) {
		c := describeContents(probeOf(nil, ".htaccess"))
		if !strings.Contains(c.Software, "never filled") {
			t.Errorf("Software = %q, want the empty-cache case", c.Software)
		}
	})
	t.Run("but a website with an .htaccess is not called a Composer cache", func(t *testing.T) {
		c := describeContents(probeOf([]string{"public", "src", "vendor"}, ".htaccess", "index.php", "composer.json"))
		if strings.Contains(c.Software, "Composer cache") {
			t.Errorf("Software = %q; the marker only counts when there is nothing else there", c.Software)
		}
	})
}

func TestDescribeContentsFallbacks(t *testing.T) {
	t.Run("empty volume", func(t *testing.T) {
		c := describeContents(volumeProbe{path: "/tmp/fake", read: true})
		if !c.Empty || c.Software != "" {
			t.Errorf("describeContents = %+v, want an empty volume with no software", c)
		}
		if !strings.Contains(c.Summary, "nothing here to lose") {
			t.Errorf("Summary = %q", c.Summary)
		}
	})
	t.Run("a layout nothing recognises admits it", func(t *testing.T) {
		c := describeContents(probeOf([]string{"weird"}, "thing.dat"))
		if c.Software != "" {
			t.Errorf("Software = %q, want nothing claimed", c.Software)
		}
		if !strings.Contains(c.Summary, "does not match anything this app recognises") {
			t.Errorf("Summary = %q, want an honest admission", c.Summary)
		}
		if len(c.Entries) != 2 {
			t.Errorf("Entries = %v; when nothing is recognised the names are all the user has", c.Entries)
		}
	})
	t.Run("a checked-out repository", func(t *testing.T) {
		c := describeContents(probeOf([]string{".git", "src"}, "go.mod"))
		if !strings.Contains(c.Software, "git repository") {
			t.Errorf("Software = %q", c.Software)
		}
	})
	t.Run("a volume that could not be read is not reported as empty", func(t *testing.T) {
		c := describeContents(volumeProbe{unavailable: "nope"})
		if c.Empty || c.Read {
			t.Errorf("describeContents = %+v, want neither read nor empty", c)
		}
		if c.Unavailable == "" {
			t.Error("Unavailable is empty; the panel has nothing to explain the gap with")
		}
	})
	t.Run("a volume nothing probed at all", func(t *testing.T) {
		c := describeContents(volumeProbe{})
		if c.Read || c.Empty || c.Unavailable == "" {
			t.Errorf("describeContents(zero) = %+v, want an honest 'not examined'", c)
		}
	})
	t.Run("an enormous directory is truncated rather than dumped", func(t *testing.T) {
		var names []string
		for i := range 200 {
			names = append(names, string(rune('a'+i%26))+strings.Repeat("x", i%5))
		}
		p := probeOf(nil, names...)
		p.truncated = true
		c := describeContents(p)
		if len(c.Entries) > probeDisplayLimit {
			t.Errorf("Entries has %d names, want at most %d", len(c.Entries), probeDisplayLimit)
		}
		if !c.Truncated {
			t.Error("Truncated = false after dropping names")
		}
	})
}

// The hard case, and the common one: an anonymous volume whose container
// was deleted long ago. Nothing on the machine records what created it, so
// the contents have to carry the whole answer — and every claim made from
// them has to be labelled as inference rather than as record.
func TestAnonymousVolumeDetailsWhenTheContainerIsGone(t *testing.T) {
	const name = "9c270dd8d451d8ba9b7040a38f5339ad65757c9f35b173a43d7e6575188035d6"
	probe := probeOf(
		[]string{"mysql", "sys", "performance_schema", "clip@002dplus@002dservice"},
		"ibdata1", "mysql.ibd", "auto.cnf",
	)
	snap := snapshot{
		// No containers at all: this is the state that makes the join
		// impossible and the contents indispensable.
		volumes: jsonLines(t, volumeLine{Name: name, Driver: "local", Labels: anonymousVolumeLabel + "=",
			Mountpoint: "/var/lib/docker/volumes/" + name + "/_data"}),
		df:     dfJSON(t, systemDF{Volumes: []volumeLine{{Name: name, Size: "289MB", Links: "0"}}}),
		probes: map[string]volumeProbe{name: probe},
	}

	v := find(t, mustClassify(t, snap), KindVolume, name)
	if !v.Anonymous || v.Verdict != VerdictDisposable {
		t.Fatalf("Anonymous = %v, Verdict = %v; want an unreferenced anonymous volume", v.Anonymous, v.Verdict)
	}
	if v.Volume == nil {
		t.Fatal("Item.Volume is nil; the detail panel has nothing to show")
	}
	sections := v.details(testNow)
	text := allText(sections)

	// The whole point: the hash is explained.
	if !strings.Contains(text, "MySQL 8 data directory") {
		t.Errorf("the panel never says what is inside the volume:\n%s", text)
	}
	if !strings.Contains(text, "clip-plus-service") {
		t.Errorf("the database name — the project this volume belonged to — is missing:\n%s", text)
	}
	if !containsRow(sections, "Mounted at", "/var/lib/mysql") {
		t.Errorf("the mount path was not recovered:\n%s", text)
	}
	// And every recovered fact is labelled as the inference it is.
	if !containsRow(sections, "Mounted at", "inferred") {
		t.Error("the inferred mount path is presented as if Docker had recorded it")
	}
	if !containsRow(sections, "Created by", "no longer exists") {
		t.Error("the panel does not admit that the creating container is gone")
	}
	if first := sections[0]; first.Title != "What is inside it" {
		t.Errorf("the first section is %q; for an anonymous volume the contents are the answer", first.Title)
	}
	// The list row and the description have to carry it too, since that is
	// what the user sees before opening anything.
	if !strings.Contains(v.Status, "MySQL 8") {
		t.Errorf("Status = %q, want the contents in the row itself", v.Status)
	}
	if !strings.Contains(v.Description, "clip-plus-service") {
		t.Errorf("Description does not lead with what is inside.\nGot: %s", v.Description)
	}
}

// When a container does still exist, its mount list is proof rather than
// inference, and the image that declares the VOLUME can be named outright.
func TestVolumeDetailsJoinsSurvivingContainer(t *testing.T) {
	const volume = "b83795fd7ff211276b9af74f3a8b37b9d0fcd3642987d71bf2fb22026e2e2ef8"
	snap := snapshot{
		ps: jsonLines(t, psLine{ID: "c1", Names: "api-db-1", Image: "postgres:16", State: "exited",
			Status: "Exited (0) 3 days ago", Labels: composeProjectLabel + "=api," + composeServiceLabel + "=db"}),
		inspect: jsonLines(t, containerInspect{
			ID: "c1", Name: "/api-db-1", Image: fullImageID("aaaaaaaaaaaa"),
			State:  containerState{Status: "exited", FinishedAt: ago(72 * time.Hour)},
			Config: containerConfig{Image: "postgres:16", Labels: map[string]string{composeProjectLabel: "api", composeServiceLabel: "db"}},
			Mounts: []containerMount{{Type: "volume", Name: volume, Destination: "/var/lib/postgresql/data"}},
		}),
		images: jsonLines(t, imageLine{ID: "aaaaaaaaaaaa", Repository: "postgres", Tag: "16", Size: "400MB"}),
		imgInspect: jsonLines(t, imageInspect{
			ID:     fullImageID("aaaaaaaaaaaa"),
			RootFS: imageRootFS{Layers: []string{"sha256:l1"}},
			Config: imageConfig{Volumes: map[string]struct{}{"/var/lib/postgresql/data": {}}},
		}),
		volumes: jsonLines(t, volumeLine{Name: volume, Driver: "local", Labels: anonymousVolumeLabel + "="}),
		df:      dfJSON(t, systemDF{Volumes: []volumeLine{{Name: volume, Size: "200MB", Links: "1"}}}),
	}

	v := find(t, mustClassify(t, snap), KindVolume, volume)
	if !v.InUse || v.RemoveCmd != "" {
		t.Fatalf("InUse = %v, RemoveCmd = %q; a referenced volume is never offered", v.InUse, v.RemoveCmd)
	}
	sections := v.details(testNow)
	if !containsRow(sections, "Mounted at", "/var/lib/postgresql/data") {
		t.Errorf("the mount path from the surviving container is missing:\n%s", allText(sections))
	}
	if !containsRow(sections, "Mounted at", "recorded by a container") {
		t.Error("a recorded mount path is being presented as an inference")
	}
	if !containsRow(sections, "Declared by", "postgres:16") {
		t.Errorf("the image declaring the VOLUME was not named:\n%s", allText(sections))
	}
	if !containsRow(sections, "api-db-1", "/var/lib/postgresql/data") {
		t.Errorf("the referencing container is not listed with its mount path:\n%s", allText(sections))
	}
	if !containsRow(sections, "api-db-1", "`api` compose project") {
		t.Error("the referencing container's compose project is missing")
	}
}

// Docker Desktop keeps its volumes inside a virtual machine and exposes
// nothing, so on that installation the contents can never be read. What is
// in there still has to be named — from the mount path and the image that
// mounted it, both of which Docker does record — and the panel has to be
// plain that this is a weaker claim than having looked.
func TestVolumeContentsAreInferredWhenTheyCannotBeRead(t *testing.T) {
	const volume = "c55797507a8b7467188915cb192fd429ce43a84ecc563f11f56208cb0e6c2d98"
	snap := snapshot{
		ps: jsonLines(t, psLine{ID: "c1", Names: "db", Image: "mysql:8", State: "exited",
			Status: "Exited (0) 4 days ago"}),
		inspect: jsonLines(t, containerInspect{
			ID: "c1", Name: "/db", Image: fullImageID("aaaaaaaaaaaa"),
			State:  containerState{Status: "exited", FinishedAt: ago(96 * time.Hour)},
			Config: containerConfig{Image: "mysql:8"},
			Mounts: []containerMount{{Type: "volume", Name: volume, Destination: "/var/lib/mysql"}},
		}),
		volumes: jsonLines(t, volumeLine{Name: volume, Labels: anonymousVolumeLabel + "="}),
		df:      dfJSON(t, systemDF{Volumes: []volumeLine{{Name: volume, Size: "289MB", Links: "1"}}}),
		// No probe: this machine cannot see inside its volumes.
		probes: map[string]volumeProbe{volume: {unavailable: "not exposed to the host"}},
	}

	v := find(t, mustClassify(t, snap), KindVolume, volume)
	sections := v.details(testNow)
	text := allText(sections)

	if !containsRow(sections, "Probably", "MySQL") {
		t.Errorf("the volume was not identified from its mount path:\n%s", text)
	}
	if !containsRow(sections, "Probably", "could not be read") {
		t.Error("an inference is being presented as if the volume had been examined")
	}
	if !strings.Contains(text, "not exposed to the host") {
		t.Errorf("the panel does not explain why the contents are missing:\n%s", text)
	}
	if v.Volume.Contents.Read {
		t.Error("Contents.Read is true for a volume that was never read")
	}
	if v.Volume.Contents.Empty {
		t.Error("an unreadable volume is being reported as empty")
	}
}

// An image's panel has to answer where it came from and why it is that big.
func TestImageDetails(t *testing.T) {
	const history = `{"CreatedAt":"2026-02-17T15:51:39Z","CreatedBy":"CMD [\"golangci-lint\"]","Size":"0B"}
{"CreatedAt":"2026-02-17T15:51:39Z","CreatedBy":"COPY linux/arm64/golangci-lint /usr/bin/ # buildkit","Size":"38MB"}
{"CreatedAt":"2026-02-17T15:51:39Z","CreatedBy":"WORKDIR /go","Size":"0B"}
{"CreatedAt":"2026-02-10T00:00:00Z","CreatedBy":"RUN /bin/sh -c apt-get update \u0026\u0026 apt-get install -y   gcc # buildkit","Size":"284MB"}
{"CreatedAt":"2026-02-01T00:00:00Z","CreatedBy":"# debian.sh --arch 'arm64' out/ 'trixie' '@1769990400'","Size":"154MB"}`

	snap := snapshot{
		images: jsonLines(t, imageLine{ID: "eeeeeeeeeeee", Repository: "golangci/golangci-lint", Tag: "v2.10",
			Size: "1.34GB", CreatedAt: "2026-02-17 17:51:39 +0200 EET"}),
		imgInspect: jsonLines(t, imageInspect{
			ID:           fullImageID("eeeeeeeeeeee"),
			RepoDigests:  []string{"golangci/golangci-lint@sha256:ea84"},
			Created:      "2026-02-17T15:51:39Z",
			Architecture: "arm64",
			Os:           "linux",
			RootFS:       imageRootFS{Layers: []string{"sha256:l1", "sha256:l2", "sha256:l3"}},
			Metadata:     imageMetadata{LastTagTime: "2026-06-17T07:24:24Z"},
			Config: imageConfig{
				Env:        []string{"PATH=/go/bin", "GOLANG_VERSION=1.26.0", "GOPATH=/go"},
				Cmd:        []string{"golangci-lint"},
				WorkingDir: "/go",
				Labels: map[string]string{
					"org.opencontainers.image.source":   "https://github.com/golangci/golangci-lint",
					"org.opencontainers.image.revision": "5d1e709b",
					"org.opencontainers.image.version":  "2.10.1",
				},
			},
		}),
		df: dfJSON(t, systemDF{Images: []imageLine{
			{ID: fullImageID("eeeeeeeeeeee"), Size: "1.34GB", UniqueSize: "1.3GB"},
		}}),
		history: map[string][]byte{"eeeeeeeeeeee": []byte(history)},
	}

	img := find(t, mustClassify(t, snap), KindImage, "golangci/golangci-lint:v2.10")
	if img.Image == nil {
		t.Fatal("Item.Image is nil; the detail panel has nothing to show")
	}
	sections := img.details(testNow)
	text := allText(sections)

	if sections[0].Title != "Where it came from" {
		t.Errorf("the first section is %q; provenance is what a developer opens this for", sections[0].Title)
	}
	if !containsRow(sections, "Source repository", "github.com/golangci/golangci-lint") {
		t.Errorf("the repository the image was built from is missing:\n%s", text)
	}
	if !containsRow(sections, "Revision", "5d1e709b") {
		t.Error("the revision label is missing")
	}
	if !containsRow(sections, "Base system", "Debian trixie") {
		t.Errorf("the base distribution is missing:\n%s", text)
	}
	// Built upstream in February, landed here in June: the second is the
	// one that decides whether it is still wanted.
	if !containsRow(sections, "Built", "2026-02-17") || !containsRow(sections, "Pulled or tagged here", "2026-06-17") {
		t.Errorf("the two dates are not both shown:\n%s", text)
	}

	steps := sectionTitled(sections, "What it builds, first steps first")
	if len(steps.Lines) == 0 {
		t.Fatal("there is no build-step section")
	}
	if !strings.Contains(steps.Lines[0], "# debian.sh") {
		t.Errorf("the first step is %q, want the oldest one", steps.Lines[0])
	}
	if strings.Contains(strings.Join(steps.Lines, "\n"), "/bin/sh -c") {
		t.Errorf("the daemon's shell wrapper reached the panel:\n%s", strings.Join(steps.Lines, "\n"))
	}

	big := sectionTitled(sections, "Biggest layers")
	if len(big.Lines) < 2 || !strings.Contains(big.Lines[0], "284.0 MB") {
		t.Errorf("the size breakdown is wrong or missing:\n%s", strings.Join(big.Lines, "\n"))
	}
	if !containsRow(sections, "GOLANG_VERSION", "1.26.0") {
		t.Error("the identifying environment is missing")
	}
	if containsRow(sections, "PATH", "/go/bin") {
		t.Error("PATH reached the panel; the environment section is meant to be the identifying subset")
	}
	if !containsRow(sections, "Layers", "3 layers holding content") {
		t.Errorf("the layer count does not come from the image's own RootFS:\n%s", text)
	}
	if !containsRow(sections, "Nothing", "No container on this machine") {
		t.Error("the panel does not say that nothing uses the image")
	}
}

// An image built on another image that is still here is the strongest
// argument for keeping the second one, and the only way to establish it is
// that the layers are literally the same layers.
func TestBaseImageIsFoundByLayerIdentity(t *testing.T) {
	base := []string{"sha256:l1"}
	child := []string{"sha256:l1", "sha256:l2", "sha256:l3"}
	unrelated := []string{"sha256:z1", "sha256:z2"}

	snap := snapshot{
		images: jsonLines(t,
			imageLine{ID: "bbbbbbbbbbbb", Repository: "busybox", Tag: "latest", Size: "4MB"},
			imageLine{ID: "cccccccccccc", Repository: "phpstorm_helpers", Tag: "PS-262", Size: "6MB"},
			imageLine{ID: "dddddddddddd", Repository: "alpine", Tag: "3.21", Size: "13MB"},
		),
		imgInspect: jsonLines(t,
			imageInspect{ID: fullImageID("bbbbbbbbbbbb"), RootFS: imageRootFS{Layers: base}},
			imageInspect{ID: fullImageID("cccccccccccc"), RootFS: imageRootFS{Layers: child}},
			imageInspect{ID: fullImageID("dddddddddddd"), RootFS: imageRootFS{Layers: unrelated}},
		),
	}
	items := mustClassify(t, snap)

	helpers := find(t, items, KindImage, "phpstorm_helpers:PS-262")
	if helpers.Image == nil || helpers.Image.BaseImage != "busybox:latest" {
		t.Fatalf("BaseImage = %q, want busybox:latest", helpers.Image.BaseImage)
	}
	busybox := find(t, items, KindImage, "busybox:latest")
	if len(busybox.Image.Derived) != 1 || busybox.Image.Derived[0] != "phpstorm_helpers:PS-262" {
		t.Errorf("Derived = %v, want the image built on it", busybox.Image.Derived)
	}
	if !containsRow(busybox.details(testNow), "Base for", "phpstorm_helpers") {
		t.Error("the panel does not warn that another image is built on this one")
	}
	alpine := find(t, items, KindImage, "alpine:3.21")
	if alpine.Image.BaseImage != "" || len(alpine.Image.Derived) != 0 {
		t.Errorf("an unrelated image was linked: base %q, derived %v", alpine.Image.BaseImage, alpine.Image.Derived)
	}
}

// BuildKit records what it pulled and what it ran, which is the only thing
// on the machine that can still name a project whose image, containers and
// volumes have all been deleted.
func TestBuildCacheDetails(t *testing.T) {
	records := []buildCacheLine{
		{ID: "1", CacheType: "regular", Size: "92.4MB", InUse: "false", UsageCount: "1",
			Description: "pulled from registry.gitlab.com/acme/service-template-go/go1.26:latest@sha256:5189ec27",
			LastUsedAt:  "2026-06-17 07:15:48.431748818 +0000 UTC"},
		{ID: "2", CacheType: "regular", Size: "891kB", InUse: "false", UsageCount: "1",
			Description: "pulled from registry.gitlab.com/acme/service-template-go/go1.26:latest@sha256:5189ec27"},
		{ID: "3", CacheType: "regular", Size: "185MB", InUse: "false", UsageCount: "2",
			Description: "mount / from exec /bin/sh -c apt-get update && apt-get install   git unzip -y"},
		{ID: "4", CacheType: "source.local", Size: "4.1kB", InUse: "false", Description: "local source for dockerfile"},
		{ID: "5", CacheType: "source.local", Size: "0B", InUse: "false", Description: "local source for context"},
		{ID: "6", CacheType: "regular", Size: "10MB", InUse: "true", Description: ""},
	}
	item, ok := buildCacheItem(records)
	if !ok || item.BuildCache == nil {
		t.Fatal("buildCacheItem produced nothing")
	}
	info := item.BuildCache

	if len(info.Pulled) != 1 {
		t.Errorf("Pulled = %v, want the reference once, without its digest", info.Pulled)
	}
	if strings.Contains(info.Pulled[0], "@sha256") {
		t.Errorf("Pulled[0] = %q; the digest makes two pulls of one tag look like two things", info.Pulled[0])
	}
	if len(info.Steps) != 1 || !strings.HasPrefix(info.Steps[0], "RUN apt-get update") {
		t.Errorf("Steps = %v, want the cleaned-up build command", info.Steps)
	}
	if strings.Contains(info.Steps[0], "/bin/sh -c") {
		t.Errorf("Steps[0] = %q; the shell wrapper is the daemon's, not the author's", info.Steps[0])
	}
	if info.LocalContexts != 2 {
		t.Errorf("LocalContexts = %d, want 2", info.LocalContexts)
	}
	if info.InUse != 1 {
		t.Errorf("InUse = %d, want the one record a build is holding", info.InUse)
	}
	if len(info.Biggest) == 0 || info.Biggest[0].Size != 185_000_000 {
		t.Errorf("Biggest is not ordered by size: %+v", info.Biggest)
	}

	sections := item.details(testNow)
	text := allText(sections)
	if !strings.Contains(text, "registry.gitlab.com/acme/service-template-go") {
		t.Errorf("the private registry the builds pulled from is missing:\n%s", text)
	}
	if !containsRow(sections, "Local build contexts", "2 records") {
		t.Errorf("the local build contexts are not counted:\n%s", text)
	}
	if !containsRow(sections, "In use", "1 record") {
		t.Error("the in-use record is not mentioned")
	}
}

// A container's compose labels say which project it belongs to and, better,
// which directory on this machine it was started from.
func TestContainerDetailsUseComposeLabels(t *testing.T) {
	labels := map[string]string{
		composeProjectLabel:     "billing",
		composeServiceLabel:     "worker",
		composeWorkingDirLabel:  "/Users/dev/src/billing",
		composeConfigFilesLabel: "/Users/dev/src/billing/docker-compose.yml",
	}
	snap := snapshot{
		ps: jsonLines(t, psLine{ID: "c1", Names: "billing-worker-1", Image: "billing:latest", State: "exited",
			Status: "Exited (0) 2 days ago", Ports: "8080/tcp", Networks: "billing_default",
			Labels: composeProjectLabel + "=billing," + composeServiceLabel + "=worker"}),
		inspect: jsonLines(t, containerInspect{
			ID: "c1", Name: "/billing-worker-1", Image: fullImageID("aaaaaaaaaaaa"),
			Path: "/app/worker", Args: []string{"--queue", "invoices"},
			State:  containerState{Status: "exited", FinishedAt: ago(48 * time.Hour)},
			Config: containerConfig{Image: "billing:latest", Labels: labels, WorkingDir: "/app", Env: []string{"APP_ENV=staging", "DB_PASSWORD=secret"}},
			Mounts: []containerMount{
				{Type: "volume", Name: "billing_pgdata", Destination: "/var/lib/postgresql/data", RW: true},
				{Type: "bind", Source: "/Users/dev/src/billing", Destination: "/app"},
			},
		}),
	}

	c := find(t, mustClassify(t, snap), KindContainer, "billing-worker-1")
	sections := c.details(testNow)
	text := allText(sections)

	if !containsRow(sections, "Started from", "/Users/dev/src/billing") {
		t.Errorf("the directory compose was run in is missing, and it is the best provenance a container has:\n%s", text)
	}
	if !containsRow(sections, "Compose service", "worker") {
		t.Error("the compose service is missing")
	}
	if !containsRow(sections, "Command", "/app/worker --queue invoices") {
		t.Errorf("the resolved command is missing:\n%s", text)
	}
	if !containsRow(sections, "APP_ENV", "staging") {
		t.Error("the identifying environment is missing")
	}
	if strings.Contains(text, "secret") {
		t.Error("a password reached the panel")
	}
	mounts := sectionTitled(sections, "Storage it mounts")
	if len(mounts.Lines) != 2 {
		t.Fatalf("got %d mount lines, want 2:\n%s", len(mounts.Lines), strings.Join(mounts.Lines, "\n"))
	}
	if !strings.Contains(mounts.Lines[0], "volume billing_pgdata → /var/lib/postgresql/data") {
		t.Errorf("mount line = %q", mounts.Lines[0])
	}
	if !strings.Contains(mounts.Lines[1], "bind /Users/dev/src/billing → /app") {
		t.Errorf("mount line = %q", mounts.Lines[1])
	}
}

// Every panel has to survive an Item that Docker told us nothing about,
// which is what a degraded scan produces.
func TestDetailsDegradeRatherThanPanic(t *testing.T) {
	for _, kind := range []Kind{KindContainer, KindImage, KindVolume, KindBuildCache, Kind(99)} {
		item := Item{Kind: kind, Name: "x"}
		sections := item.Details()
		if kind == Kind(99) {
			if sections != nil {
				t.Errorf("an unknown kind produced %d sections", len(sections))
			}
			continue
		}
		if len(sections) != 1 {
			t.Fatalf("%s produced %d sections, want one explaining the gap", kind, len(sections))
		}
		if len(sections[0].Lines) == 0 {
			t.Errorf("%s explains nothing about why it has no detail", kind)
		}
	}

	// And an Item with detail structs that are present but empty.
	empty := Item{Kind: KindImage, Name: "x", Image: &ImageInfo{}}
	if len(empty.Details()) == 0 {
		t.Error("an image with an empty ImageInfo produced no sections at all")
	}
	emptyVol := Item{Kind: KindVolume, Name: "x", Volume: &VolumeInfo{}}
	if len(emptyVol.Details()) == 0 {
		t.Error("a volume with an empty VolumeInfo produced no sections at all")
	}
	for _, s := range append(empty.Details(), emptyVol.Details()...) {
		if len(s.Rows) == 0 && len(s.Lines) == 0 {
			t.Errorf("section %q is empty and should have been dropped", s.Title)
		}
	}
}

// Images are sorted biggest first before the history cap is applied, so if
// anything goes without a build history it is the 6 MB image rather than
// the 1.3 GB one. Tags must be collapsed too: fetching one image's history
// three times is three subprocesses wasted.
func TestImageIDsFromIsBiggestFirstAndDistinct(t *testing.T) {
	out := jsonLines(t,
		imageLine{ID: "aaaaaaaaaaaa", Repository: "small", Tag: "1", Size: "6.27MB"},
		imageLine{ID: "bbbbbbbbbbbb", Repository: "big", Tag: "1", Size: "1.34GB"},
		imageLine{ID: "bbbbbbbbbbbb", Repository: "big", Tag: "2", Size: "1.34GB"},
		imageLine{ID: "cccccccccccc", Repository: "middle", Tag: "1", Size: "734MB"},
		imageLine{ID: "", Repository: "broken", Tag: "1"},
	)
	got := imageIDsFrom(out)
	want := []string{"bbbbbbbbbbbb", "cccccccccccc", "aaaaaaaaaaaa"}
	if len(got) != len(want) {
		t.Fatalf("imageIDsFrom = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("imageIDsFrom = %v, want %v", got, want)
			break
		}
	}
	if got := imageIDsFrom([]byte("not json at all")); got != nil {
		t.Errorf("imageIDsFrom(garbage) = %v, want nothing", got)
	}
}

// The mountpoint map lets the contents probe start without waiting for
// `docker volume inspect`.
func TestMountpointsFrom(t *testing.T) {
	out := jsonLines(t,
		volumeLine{Name: "a", Mountpoint: "/var/lib/docker/volumes/a/_data"},
		volumeLine{Name: "", Mountpoint: "/nowhere"},
	)
	got := mountpointsFrom(out)
	if got["a"] != "/var/lib/docker/volumes/a/_data" {
		t.Errorf("mountpointsFrom = %v", got)
	}
	if len(got) != 1 {
		t.Errorf("mountpointsFrom kept a nameless volume: %v", got)
	}
}
