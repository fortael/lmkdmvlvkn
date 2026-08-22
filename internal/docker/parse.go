package docker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// This file holds every type that mirrors Docker CLI output and every
// function that turns that output into Go values. Nothing here runs a
// subprocess: each function takes bytes that somebody else fetched, so the
// tests can feed it recorded output and never need a daemon.
//
// Every mirrored field is a string, including counts and booleans. The
// CLI renders `--format '{{json .}}'` by running each column through a
// text template and then quoting the result, so "Containers" arrives as
// "0" and "InUse" as "false". Decoding them as int/bool fails outright,
// and a failed decode would throw away the whole record.

const shortIDLen = 12

// psLine is one line of `docker ps -a --format '{{json .}}'`, and also one
// element of the Containers array in `docker system df -v`, which renders
// containers through the same template context.
//
// Image is the reference the container was *created* with, which may be a
// tag ("postgres:16"), a digest, or a bare image ID if the tag has since
// been reused. That ambiguity is why the container-to-image join also
// consults docker inspect, which reports the resolved image ID.
type psLine struct {
	ID           string
	Names        string
	Image        string
	Command      string
	CreatedAt    string
	RunningFor   string
	State        string
	Status       string
	Labels       string
	LocalVolumes string
	Mounts       string
	Size         string
	Networks     string
	Ports        string
}

// imageLine is one line of `docker images --format '{{json .}}'` and one
// element of the Images array in `docker system df -v`.
//
// The two sources disagree in ways that matter. `docker images` prints a
// 12-character ID and "N/A" for SharedSize/UniqueSize; `docker system df
// -v` prints the full "sha256:..." ID and fills both sizes in. So the
// listing comes from `docker images` (it is fast and includes dangling
// images) while the sizes come from df, joined on the shortened ID.
type imageLine struct {
	ID           string
	Repository   string
	Tag          string
	Digest       string
	CreatedAt    string
	CreatedSince string
	Containers   string
	Size         string
	SharedSize   string
	UniqueSize   string
}

// volumeLine is one line of `docker volume ls --format '{{json .}}'` and
// one element of the Volumes array in `docker system df -v`. Only df fills
// in Size and Links; plain `docker volume ls` reports "N/A" for both
// because computing them means walking every volume's directory.
type volumeLine struct {
	Name       string
	Driver     string
	Scope      string
	Mountpoint string
	Labels     string
	Links      string
	Size       string
	Status     string
}

// buildCacheLine is one element of the BuildCache array in `docker system
// df -v`. Unlike every other object here the build cache reports a real
// LastUsedAt, because BuildKit tracks it in order to evict cold entries.
type buildCacheLine struct {
	ID          string
	Parent      string
	CacheType   string
	Description string
	InUse       string
	Shared      string
	Size        string
	CreatedAt   string
	LastUsedAt  string
	UsageCount  string
}

// systemDF mirrors `docker system df -v --format '{{json .}}'`, which —
// unlike every other command here — emits a single object rather than
// line-delimited records.
type systemDF struct {
	Images     []imageLine
	Containers []psLine
	Volumes    []volumeLine
	BuildCache []buildCacheLine
}

// containerInspect is the slice of `docker inspect` output this package
// reads. It exists because `docker ps` cannot answer two questions that
// the classification depends on: which image ID a container actually
// resolved to, and when it last ran as a timestamp rather than the prose
// "Exited (0) 3 weeks ago".
type containerInspect struct {
	ID      string `json:"Id"`
	Name    string
	Created string
	Image   string
	State   struct {
		Status     string
		Running    bool
		StartedAt  string
		FinishedAt string
	}
	Config struct {
		Image  string
		Labels map[string]string
	}
	Mounts []struct {
		Type        string
		Name        string
		Source      string
		Destination string
	}
}

// volumeInspect is the slice of `docker volume inspect` this package
// reads. It is fetched only for CreatedAt, which the listing formats
// cannot produce at all, and for Labels as a real map rather than the
// flattened string `docker volume ls` prints.
type volumeInspect struct {
	Name       string
	Driver     string
	Mountpoint string
	CreatedAt  string
	Labels     map[string]string
}

// decodeLines decodes Docker's line-delimited JSON, one record per line.
//
// A single unreadable line is skipped rather than fatal: Docker
// occasionally interleaves a warning into stdout, and losing one container
// is much better than losing the listing. Output that produced no usable
// record at all is a different matter — that is the "Docker returned
// garbage" case, and it is reported as an error instead of silently
// looking like an empty machine.
//
// A field whose type is not what this package expects is kept rather than
// dropped. encoding/json fills in every field it could and reports the
// mismatch afterwards, so the record is still worth having, and the
// alternative is losing a whole container over a field nobody reads.
// This is not hypothetical: `docker ps` renders every column as a string
// except Platform, which is an object, and a future release moving
// another column the same way would otherwise blank the listing.
func decodeLines[T any](out []byte) ([]T, error) {
	var (
		items      []T
		seen, bad  int
		firstBadLn string
	)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		seen++
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &typeErr) {
				if bad == 0 {
					firstBadLn = line
				}
				bad++
				continue
			}
		}
		items = append(items, v)
	}
	if seen > 0 && bad == seen {
		return nil, fmt.Errorf("docker printed %d line(s) of unreadable output, first was %q", seen, truncate(firstBadLn, 120))
	}
	return items, nil
}

// parseDF decodes `docker system df -v --format '{{json .}}'`. Empty
// output is an error rather than an empty snapshot, so a df that silently
// produced nothing does not get reported to the user as "everything on
// this machine is 0 bytes".
func parseDF(out []byte) (systemDF, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return systemDF{}, errors.New("docker system df printed nothing")
	}
	var df systemDF
	if err := json.Unmarshal(trimmed, &df); err != nil {
		return systemDF{}, fmt.Errorf("docker system df: %w", err)
	}
	return df, nil
}

// sizeUnits maps the suffixes Docker's size formatter emits to a byte
// multiplier. Docker formats object sizes in decimal units (kB = 1000
// bytes, not 1024) — summing the UniqueSize column of `docker system df
// -v` this way reproduces the "reclaimable" figure the same command prints
// in its summary table exactly, which is the check that settled it. The
// binary suffixes are accepted too because a handful of other Docker
// surfaces use them.
var sizeUnits = map[string]float64{
	"b":   1,
	"kb":  1e3,
	"mb":  1e6,
	"gb":  1e9,
	"tb":  1e12,
	"pb":  1e15,
	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
	"pib": 1 << 50,
}

// parseSize converts one of Docker's human-readable sizes ("734MB",
// "91.1kB", "0B") into bytes. The second result distinguishes a genuine
// zero from "Docker did not tell us", which the caller needs in order to
// decide between reporting 0 bytes and admitting the size is unknown —
// the listing commands print "N/A" wherever computing a size would have
// cost a directory walk.
//
// Container sizes arrive as "12.4MB (virtual 187MB)": the leading figure
// is the writable layer, the only part that removing the container frees,
// so everything from the parenthesis on is dropped.
func parseSize(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" || strings.EqualFold(s, "N/A") || s == "-" {
		return 0, false
	}
	split := 0
	for split < len(s) && (s[split] >= '0' && s[split] <= '9' || s[split] == '.' || s[split] == '-' || s[split] == '+') {
		split++
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(s[:split]), 64)
	if err != nil || value < 0 {
		return 0, false
	}
	mul, ok := sizeUnits[strings.ToLower(strings.TrimSpace(s[split:]))]
	if !ok {
		return 0, false
	}
	return int64(value*mul + 0.5), true
}

// dockerTimeLayouts are the timestamp shapes seen across Docker's output.
// The API and inspect emit RFC 3339; the CLI's own table formatter emits
// Go's default time rendering, sometimes with a fractional part and
// sometimes without — the fractional layout parses both, because a
// ".999999999" fragment is optional on the way in.
var dockerTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05",
}

// parseTime reads a Docker timestamp, returning the zero time for
// anything it cannot make sense of.
//
// Docker writes the year-1 zero time to mean "this never happened": a
// container that was created but never started has
// StartedAt = "0001-01-01T00:00:00Z". Passing that through as a real date
// would render as a container last used two thousand years ago, so it is
// collapsed back to the zero time, which the UI shows as "-".
//
// Note that the CLI's own format prints a zone abbreviation ("+0300
// EEST") that is not necessarily one this machine knows. That is
// harmless: the numeric offset next to it is what fixes the instant, and
// Go keeps the abbreviation as a label.
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") || s == "-" {
		return time.Time{}
	}
	for _, layout := range dockerTimeLayouts {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Year() <= 1 {
			return time.Time{}
		}
		return t
	}
	return time.Time{}
}

// newest returns the latest of the given times, ignoring zero values, and
// is itself zero when every input was. This is how LastUsed is chosen:
// "finished", "started" and "created" are each meaningful when set, and
// the most recent of them is the last moment the object did anything.
func newest(times ...time.Time) time.Time {
	var out time.Time
	for _, t := range times {
		if t.IsZero() {
			continue
		}
		if out.IsZero() || t.After(out) {
			out = t
		}
	}
	return out
}

// parseLabels splits the "k=v,k2=v2" string the listing formats print for
// a label set.
//
// This encoding is lossy — a label value containing a comma cannot be
// recovered from it — and that is exactly why container labels are read
// from `docker inspect`, which returns a real JSON map. This function is
// the fallback for volumes and for the case where inspect is unavailable;
// the labels we care about (com.docker.compose.project,
// com.docker.volume.anonymous) never contain commas.
func parseLabels(s string) map[string]string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		if k = strings.TrimSpace(k); k != "" {
			out[k] = v
		}
	}
	return out
}

// parseCount reads one of the numeric-looking string fields ("0", "3").
func parseCount(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseBool reads one of the boolean-looking string fields.
func parseBool(s string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	return err == nil && b
}

// shortID normalises an identifier to the 12-character form Docker prints
// in its tables, so IDs from different commands can be used as one map
// key: `docker system df -v` reports images as "sha256:4764c2af3ab4...",
// `docker images` as "4764c2af3ab4", and `docker inspect` as the full
// 64-character digest.
//
// Only the "sha256:" prefix is stripped, never an arbitrary prefix up to a
// colon, because the same helper is applied to values that may turn out to
// be image references like "postgres:16".
func shortID(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "sha256:")
	if len(s) > shortIDLen {
		s = s[:shortIDLen]
	}
	return s
}

// anonymousVolumeRe matches the name Docker invents for a volume nobody
// named: a bare 64-character hex string, the same shape as an ID. A volume
// a human or a compose file named never looks like this, and Docker
// forbids creating one that does.
//
// The com.docker.volume.anonymous label is the authoritative signal and is
// checked first; this pattern covers volumes created by older daemons that
// predate the label.
var anonymousVolumeRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// noneRef is what Docker prints for an image repository or tag that no
// longer exists.
const noneRef = "<none>"

// isNone reports whether a repository/tag field is Docker's placeholder
// for "absent".
func isNone(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s == noneRef
}

// truncate shortens s for inclusion in an error message.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// firstLine returns the first non-empty line of s, used to keep a
// multi-line stderr dump out of a one-line error message.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
