package docker

import (
	"regexp"
	"slices"
	"strings"
)

// This file turns `docker history --no-trunc` into something a human can
// read. Nothing here runs a subprocess.
//
// Docker records a build step as the shell invocation the daemon ran, not
// as the Dockerfile line somebody wrote, and it spells that invocation
// differently depending on which builder produced the image:
//
//	classic   /bin/sh -c #(nop)  CMD ["php" "-a"]
//	classic   /bin/sh -c apt-get update && apt-get install -y curl
//	buildkit  RUN /bin/sh -c apt-get update && apt-get install -y curl # buildkit
//	buildkit  COPY docker-php-entrypoint /usr/local/bin/ # buildkit
//
// The `#(nop)` marker means "this step added no filesystem content", the
// `# buildkit` suffix is BuildKit tagging its own work, and the `/bin/sh
// -c` wrapper is the daemon's, not the author's. Stripping all three
// recovers something very close to the original Dockerfile line, which is
// the only form worth showing anyone.

// historyLine is one record of `docker history --no-trunc --format
// '{{json .}}'`. Size arrives as a formatted string like every other CLI
// column; ID is "<missing>" for every layer but the top one, because a
// pulled image only keeps an ID for its own final layer.
type historyLine struct {
	ID           string
	CreatedAt    string
	CreatedSince string
	CreatedBy    string
	Size         string
	Comment      string
}

// dockerfileVerbs are the instructions whose name may lead a history entry.
// Recognising the verb is what lets the detail panel keep the steps that
// build something and drop the ones that only set metadata.
var dockerfileVerbs = map[string]bool{
	"ADD": true, "ARG": true, "CMD": true, "COPY": true, "ENTRYPOINT": true,
	"ENV": true, "EXPOSE": true, "FROM": true, "HEALTHCHECK": true, "LABEL": true,
	"MAINTAINER": true, "ONBUILD": true, "RUN": true, "SHELL": true, "STOPSIGNAL": true,
	"USER": true, "VOLUME": true, "WORKDIR": true,
}

// contentDigestRe matches the content-addressed placeholders the classic
// builder writes into a COPY/ADD step: "file:<64 hex>", "multi:<64 hex>",
// "dir:<64 hex>". They identify the copied bytes to the daemon and are pure
// noise to a reader, so they are shortened rather than shown in full.
var contentDigestRe = regexp.MustCompile(`\b(file|multi|dir):[0-9a-f]{16,}\b`)

// whitespaceRe collapses the runs of spaces a multi-line RUN leaves behind
// when Docker flattens it onto one line. This is a display transform only —
// nothing here is ever executed.
var whitespaceRe = regexp.MustCompile(`\s+`)

// nopMarker is what the classic builder writes for a step that added no
// filesystem content.
const nopMarker = "#(nop)"

// buildkitMarker is the suffix BuildKit appends to every step it records.
const buildkitMarker = "# buildkit"

// parseHistory decodes `docker history` output into layers in build order —
// oldest first, the order a Dockerfile reads — because Docker prints them
// newest first and "the first layers" is the question being asked.
func parseHistory(out []byte) ([]Layer, error) {
	lines, err := decodeLines[historyLine](out)
	if err != nil {
		return nil, err
	}
	layers := make([]Layer, 0, len(lines))
	for _, l := range lines {
		command, empty := cleanHistoryCommand(l.CreatedBy)
		size, sizeOK := parseSize(l.Size)
		layers = append(layers, Layer{
			Command:     command,
			Instruction: historyInstruction(command),
			Size:        size,
			SizeKnown:   sizeOK,
			Created:     parseTime(l.CreatedAt),
			// A step is empty when the builder said so, or when it plainly
			// added nothing. Trusting the marker alone would miss every
			// BuildKit image, which does not write one.
			Empty: empty || (sizeOK && size == 0),
		})
	}
	slices.Reverse(layers)
	return layers, nil
}

// cleanHistoryCommand rewrites one CreatedBy string into the Dockerfile
// line it stands for, and reports whether Docker marked the step as adding
// no filesystem content.
func cleanHistoryCommand(raw string) (command string, empty bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}

	// BuildKit tags its own steps; the tag is not part of the command.
	if trimmed := strings.TrimSuffix(s, buildkitMarker); trimmed != s {
		s = strings.TrimSpace(trimmed)
	}

	// The classic builder wraps everything in the daemon's shell.
	if rest, ok := strings.CutPrefix(s, "/bin/sh -c "); ok {
		s = strings.TrimSpace(rest)
		if trimmed, isNop := strings.CutPrefix(s, nopMarker); isNop {
			// A #(nop) step already carries its own instruction word.
			return tidyCommand(strings.TrimSpace(trimmed)), true
		}
		// Anything else the classic builder ran through a shell was a RUN.
		return "RUN " + tidyCommand(s), false
	}

	// BuildKit writes the verb itself, but still records a RUN as the shell
	// invocation underneath it, sometimes with the build arguments the
	// step was given in front: "RUN |1 TARGETPLATFORM=linux/arm64 /bin/sh
	// -c …". Those arguments are the builder's bookkeeping, not the line
	// anybody wrote.
	if loc := runShellPrefixRe.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return "RUN " + tidyCommand(strings.TrimSpace(s[loc[1]:])), false
	}
	return tidyCommand(s), false
}

// runShellPrefixRe matches BuildKit's rendering of a RUN step, including
// the optional "|<n> NAME=value …" build-argument prefix, up to and
// including the shell wrapper.
var runShellPrefixRe = regexp.MustCompile(`^RUN (\|\d+ (\S+=\S* )*)?/bin/sh -c `)

// tidyCommand does the cosmetic half: flatten the whitespace a multi-line
// shell command leaves behind, and shorten the content digests a COPY step
// carries.
func tidyCommand(s string) string {
	s = contentDigestRe.ReplaceAllStringFunc(s, func(m string) string {
		kind, digest, _ := strings.Cut(m, ":")
		switch kind {
		case "multi":
			return "<several files>"
		case "dir":
			return "<a directory>"
		default:
			return "<file " + digest[:min(len(digest), 8)] + "…>"
		}
	})
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
}

// historyInstruction returns the Dockerfile verb a cleaned command starts
// with, or "" when it does not start with one — which is not a failure. The
// bottom entry of a distro image is a comment describing how its root
// filesystem was assembled, and that comment is the most informative line
// in the whole history.
func historyInstruction(command string) string {
	verb, _, _ := strings.Cut(command, " ")
	verb = strings.ToUpper(strings.TrimSpace(verb))
	if dockerfileVerbs[verb] {
		return verb
	}
	return ""
}

// rootfsImportRe matches the layer that imports a distribution's root
// filesystem, which is the bottom of every image not built FROM scratch.
// The tarball's name carries the distribution and its version, so this one
// line answers "what is this image actually built on" for the large
// majority of images on a developer's machine.
var rootfsImportRe = regexp.MustCompile(`(?i)\b(alpine)-minirootfs-([0-9][0-9.]*)`)

// debuerreotypeRe matches the comment the official Debian and Ubuntu
// images carry at the bottom of their history:
//
//	# debian.sh --arch 'arm64' out/ 'trixie' '@1783900800'
//
// The suite name is the one quoted immediately after the output directory,
// which is what the anchor on "out/" is for — the first quoted word in that
// line is the architecture, and matching it would report every image on
// this machine as running "Debian arm64".
var debuerreotypeRe = regexp.MustCompile(`#\s*(debian|ubuntu)\.sh\s.*\bout/\s+'([a-z][a-z0-9.-]*)'`)

// baseSystem names the distribution at the bottom of an image, read off the
// layer that imported its root filesystem.
//
// This is inference from a filename, so it only ever claims what the
// filename says. An image built FROM scratch, or one whose base was
// squashed, produces "" and the panel says nothing rather than guessing.
func baseSystem(layers []Layer) string {
	for _, l := range layers {
		if l.Command == "" {
			continue
		}
		if m := rootfsImportRe.FindStringSubmatch(l.Command); m != nil {
			return "Alpine Linux " + m[2]
		}
		if m := debuerreotypeRe.FindStringSubmatch(l.Command); m != nil {
			return strings.ToUpper(m[1][:1]) + m[1][1:] + " " + m[2]
		}
		// The official busybox images state it outright, e.g.
		// "BusyBox 1.38.0 (glibc), Debian 13".
		if strings.HasPrefix(l.Command, "BusyBox ") {
			return l.Command
		}
		// Only the bottom few entries can be the base; anything further up
		// mentioning a rootfs tarball is a build step that downloaded one.
		if l.Instruction == "RUN" || l.Instruction == "COPY" {
			break
		}
	}
	return ""
}

// packageManagers maps a command fragment to the distribution family that
// uses it, as a fallback for images whose bottom layer gives nothing away —
// a squashed or scratch-built image still tells you what it is by how it
// installs things.
var packageManagers = []struct{ fragment, family string }{
	{"apk add", "Alpine (apk)"},
	{"apt-get install", "Debian or Ubuntu (apt)"},
	{"apt install", "Debian or Ubuntu (apt)"},
	{"microdnf install", "RHEL family (microdnf)"},
	{"dnf install", "RHEL family (dnf)"},
	{"yum install", "RHEL family (yum)"},
	{"zypper install", "openSUSE (zypper)"},
	{"pacman -S", "Arch (pacman)"},
}

// packageManager reports the distribution family an image's build steps
// imply.
func packageManager(layers []Layer) string {
	for _, l := range layers {
		if l.Instruction != "RUN" {
			continue
		}
		for _, pm := range packageManagers {
			if strings.Contains(l.Command, pm.fragment) {
				return pm.family
			}
		}
	}
	return ""
}

// metadataSteps counts the history entries that added nothing to the
// filesystem — ENV, LABEL, CMD and friends. They are typically most of an
// image's history, which is why `docker history` prints twenty lines for
// an image made of nine layers.
func metadataSteps(layers []Layer) int {
	n := 0
	for _, l := range layers {
		if l.Empty {
			n++
		}
	}
	return n
}

// identifyingEnvSuffix is the naming convention every language runtime
// image follows for the one variable that says which version it is:
// PHP_VERSION, GOLANG_VERSION, NODE_VERSION, PYTHON_VERSION, RUBY_VERSION.
const identifyingEnvSuffix = "_VERSION"

// identifyingEnvNames are the other variables worth surfacing: the ones
// that say where a toolchain keeps its things, which is how you recognise
// which stack an unlabelled image belongs to and, for PGDATA in
// particular, where a database image will put its volume.
var identifyingEnvNames = map[string]bool{
	"COMPOSER_CACHE_DIR": true, "COMPOSER_HOME": true, "GEM_HOME": true,
	"GOPATH": true, "GOROOT": true, "GOTOOLCHAIN": true, "JAVA_HOME": true,
	"MYSQL_DATABASE": true, "NODE_ENV": true, "PGDATA": true, "PHP_INI_DIR": true,
	"POSTGRES_DB": true, "POSTGRES_USER": true, "PYTHONPATH": true, "VIRTUAL_ENV": true,
	"MAVEN_HOME": true, "RAILS_ENV": true, "APP_ENV": true, "ELASTIC_CONTAINER": true,
}

// secretishEnvFragments are the substrings that mark a variable as
// something never to print. An image's environment is baked into layers
// anybody can read, but that is no reason for this app to put a password on
// screen, and a build argument leaking a token into ENV is a real and
// common mistake.
var secretishEnvFragments = []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "APIKEY", "API_KEY", "CREDENTIAL", "PRIVATE_KEY", "_KEY", "KEYS"}

// identifyingEnv picks the environment variables that say what stack an
// image is, discarding PATH and the rest of the noise.
func identifyingEnv(env []string) []DetailRow {
	var rows []DetailRow
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		upper := strings.ToUpper(name)
		if !identifyingEnvNames[upper] && !strings.HasSuffix(upper, identifyingEnvSuffix) {
			continue
		}
		if isSecretish(upper) {
			continue
		}
		rows = append(rows, DetailRow{Label: name, Value: truncate(strings.TrimSpace(value), 120)})
	}
	slices.SortStableFunc(rows, func(a, b DetailRow) int { return strings.Compare(a.Label, b.Label) })
	return rows
}

// isSecretish reports whether a variable name suggests it holds a secret.
func isSecretish(upperName string) bool {
	for _, fragment := range secretishEnvFragments {
		if strings.Contains(upperName, fragment) {
			return true
		}
	}
	return false
}
