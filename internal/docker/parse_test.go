package docker

import (
	"strings"
	"testing"
	"time"
)

// Docker formats sizes in decimal units, and the check that settled it was
// arithmetic: summing the UniqueSize column of `docker system df -v` on a
// real machine reproduced the RECLAIMABLE total the same command prints
// only under a 1000-based interpretation. Parsing kB as 1024 would quietly
// overstate every figure this app shows by 2.4%.
func TestParseSize(t *testing.T) {
	tests := []struct {
		in    string
		want  int64
		wantK bool
	}{
		{"0B", 0, true},
		{"734MB", 734_000_000, true},
		{"91.1kB", 91_100, true},
		{"6.27MB", 6_270_000, true},
		{"1.34GB", 1_340_000_000, true},
		{"4.693GB", 4_693_000_000, true},
		{"1KiB", 1024, true},
		{"1MiB", 1024 * 1024, true},
		{" 200.7MB ", 200_700_000, true},
		// Container sizes carry the virtual size in parentheses; only the
		// leading figure is freed by removing the container.
		{"12.4MB (virtual 187MB)", 12_400_000, true},
		{"0B (virtual 187MB)", 0, true},
		// Everything Docker uses to mean "I did not compute this" must be
		// distinguishable from a real zero.
		{"N/A", 0, false},
		{"", 0, false},
		{"-", 0, false},
		{"nonsense", 0, false},
		{"12", 0, false},
		{"MB", 0, false},
		{"-5MB", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseSize(tt.in)
			if got != tt.want || ok != tt.wantK {
				t.Errorf("parseSize(%q) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.wantK)
			}
		})
	}
}

// Docker emits at least three timestamp shapes, and writes the year-1 zero
// time to mean "this never happened" — a container created but never
// started has StartedAt = 0001-01-01. Passing that through would render as
// a container last used two thousand years ago.
func TestParseTime(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{
			name: "inspect RFC 3339 with nanoseconds",
			in:   "2026-08-05T00:37:23.571122414Z",
			want: time.Date(2026, 8, 5, 0, 37, 23, 571122414, time.UTC),
		},
		{
			name: "CLI table format with a zone abbreviation this machine may not know",
			in:   "2026-08-05 03:37:23 +0300 EEST",
			want: time.Date(2026, 8, 5, 0, 37, 23, 0, time.UTC),
		},
		{
			name: "build cache format with a fractional part",
			in:   "2026-06-17 07:15:48.431748818 +0000 UTC",
			want: time.Date(2026, 6, 17, 7, 15, 48, 431748818, time.UTC),
		},
		{name: "never started", in: "0001-01-01T00:00:00Z", want: time.Time{}},
		{name: "never started, table format", in: "0001-01-01 00:00:00 +0000 UTC", want: time.Time{}},
		{name: "not available", in: "N/A", want: time.Time{}},
		{name: "empty", in: "", want: time.Time{}},
		{name: "garbage", in: "last tuesday", want: time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTime(tt.in)
			if !got.Equal(tt.want) {
				t.Errorf("parseTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
			if tt.want.IsZero() && !got.IsZero() {
				t.Errorf("parseTime(%q) must report the zero time so the UI can render it as %q", tt.in, "-")
			}
		})
	}
}

// newest is how LastUsed is chosen: finished, started and created are each
// meaningful when set, and the most recent of them is the last moment the
// object did anything. Zero values must never win.
func TestNewestIgnoresZeroTimes(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if got := newest(time.Time{}, early, time.Time{}, late); !got.Equal(late) {
		t.Errorf("newest() = %v, want %v", got, late)
	}
	if got := newest(late, early); !got.Equal(late) {
		t.Errorf("newest() = %v, want %v (order must not matter)", got, late)
	}
	if got := newest(); !got.IsZero() {
		t.Errorf("newest() with no arguments = %v, want the zero time", got)
	}
	if got := newest(time.Time{}, time.Time{}); !got.IsZero() {
		t.Errorf("newest() of only zero times = %v, want the zero time", got)
	}
}

// IDs arrive in three different forms depending on which command produced
// them, and they all have to collapse to one map key. An image reference
// must survive intact, since the same helper is applied to values that may
// turn out to be one.
func TestShortID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"sha256:4764c2af3ab4d779fe21384f588458d8773e303cd602856f3ef50f1f3fc13022", "4764c2af3ab4"},
		{"4764c2af3ab4d779fe21384f588458d8773e303cd602856f3ef50f1f3fc13022", "4764c2af3ab4"},
		{"4764c2af3ab4", "4764c2af3ab4"},
		{"  4764c2af3ab4  ", "4764c2af3ab4"},
		{"postgres:16", "postgres:16"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortID(tt.in); got != tt.want {
			t.Errorf("shortID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseLabels(t *testing.T) {
	got := parseLabels("com.docker.compose.project=api,com.docker.compose.version=5.1.2")
	if got["com.docker.compose.project"] != "api" {
		t.Errorf("project label = %q, want %q", got["com.docker.compose.project"], "api")
	}

	// The anonymous-volume marker is a label with an empty value, so
	// presence has to be distinguishable from absence.
	anon := parseLabels("com.docker.volume.anonymous=")
	if _, ok := anon[anonymousVolumeLabel]; !ok {
		t.Errorf("parseLabels lost the empty-valued %s label: %v", anonymousVolumeLabel, anon)
	}

	for _, in := range []string{"", "N/A", "   "} {
		if got := parseLabels(in); len(got) != 0 {
			t.Errorf("parseLabels(%q) = %v, want no labels", in, got)
		}
	}
}

// A single unreadable line is skipped so that one warning interleaved into
// stdout does not cost the whole listing, but output that produced nothing
// usable is the "Docker returned garbage" case and must be an error rather
// than an empty machine.
func TestDecodeLines(t *testing.T) {
	t.Run("skips one bad line among good ones", func(t *testing.T) {
		in := []byte(`{"ID":"aaa"}` + "\n" + `not json at all` + "\n" + `{"ID":"bbb"}` + "\n")
		got, err := decodeLines[psLine](in)
		if err != nil {
			t.Fatalf("decodeLines: %v", err)
		}
		if len(got) != 2 || got[0].ID != "aaa" || got[1].ID != "bbb" {
			t.Errorf("decodeLines = %+v, want the two readable records", got)
		}
	})

	t.Run("all bad is an error", func(t *testing.T) {
		_, err := decodeLines[psLine]([]byte("Cannot connect to the Docker daemon\nis it running?\n"))
		if err == nil {
			t.Fatal("decodeLines accepted output with no readable record; garbage must be reported, not shown as an empty machine")
		}
	})

	t.Run("empty output is an empty list, not an error", func(t *testing.T) {
		got, err := decodeLines[psLine]([]byte("\n  \n"))
		if err != nil || len(got) != 0 {
			t.Errorf("decodeLines(blank) = (%v, %v), want (empty, nil) — a machine with no containers is normal", got, err)
		}
	})

	t.Run("unknown fields are tolerated", func(t *testing.T) {
		// docker ps really does emit a Platform object alongside its
		// string columns, and later releases add more fields over time.
		got, err := decodeLines[psLine]([]byte(
			`{"ID":"aaa","Platform":{"architecture":"arm64","os":"linux"},"SomethingDockerAddedLater":"x"}`))
		if err != nil || len(got) != 1 || got[0].ID != "aaa" {
			t.Errorf("decodeLines = (%v, %v), want the record decoded despite the unknown fields", got, err)
		}
	})

	// If a column this package does read ever stops being a string, the
	// rest of the record is still worth having — losing a whole container
	// over one field would blank the listing.
	t.Run("a field of an unexpected type does not cost the record", func(t *testing.T) {
		got, err := decodeLines[psLine]([]byte(`{"ID":"aaa","Names":"web","Labels":{"k":"v"},"Status":"Up 2 hours"}`))
		if err != nil {
			t.Fatalf("decodeLines: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d records, want the record kept despite the type mismatch", len(got))
		}
		if got[0].ID != "aaa" || got[0].Names != "web" || got[0].Status != "Up 2 hours" {
			t.Errorf("decodeLines = %+v, want every well-typed field preserved", got[0])
		}
	})
}

func TestParseDF(t *testing.T) {
	t.Run("empty output is an error", func(t *testing.T) {
		if _, err := parseDF(nil); err == nil {
			t.Error("parseDF(nil) returned no error; a silent df must not read as 0 bytes everywhere")
		}
	})
	t.Run("garbage is an error", func(t *testing.T) {
		if _, err := parseDF([]byte("permission denied")); err == nil {
			t.Error("parseDF accepted non-JSON output")
		}
	})
	t.Run("missing sections are tolerated", func(t *testing.T) {
		df, err := parseDF([]byte(`{"Images":[{"ID":"sha256:abc","UniqueSize":"1MB"}]}`))
		if err != nil {
			t.Fatalf("parseDF: %v", err)
		}
		if len(df.Images) != 1 || len(df.Volumes) != 0 || len(df.BuildCache) != 0 {
			t.Errorf("parseDF = %+v, want the images section and nothing else", df)
		}
	})
}

// A container name is only proof of throwaway intent if the daemon
// invented it. Both halves have to come from Docker's own word lists,
// because flagging a hand-named "my_db" as junk is the one mistake this
// package cannot afford.
func TestIsGeneratedName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"nostalgic_ptolemy", true},
		{"vibrant_curie", true},
		{"boring_wozniak", true},
		{"/nostalgic_ptolemy", true}, // inspect prefixes names with a slash
		{"nostalgic_ptolemy3", true}, // the daemon appends a digit on a name collision
		{"admiring_einstein", true},

		{"my_db", false},
		{"api_cache", false},
		{"postgres", false},
		{"", false},
		{"_", false},
		{"nostalgic_notascientist", false},
		{"notanadjective_ptolemy", false},
		{"NOSTALGIC_PTOLEMY", false},
		// Compose's own default names split into three parts, so they can
		// never be mistaken for a generated one.
		{"api_db_1", false},
		{"api-db-1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGeneratedName(tt.name); got != tt.want {
				t.Errorf("isGeneratedName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// The word lists are a verbatim copy of the daemon's, and a truncated or
// mangled copy would silently stop detecting generated names.
func TestGeneratedWordListsAreIntact(t *testing.T) {
	if len(generatedAdjectives) < 100 {
		t.Errorf("generatedAdjectives has %d entries, expected the daemon's full list", len(generatedAdjectives))
	}
	if len(generatedSurnames) < 200 {
		t.Errorf("generatedSurnames has %d entries, expected the daemon's full list", len(generatedSurnames))
	}
	for word := range generatedAdjectives {
		if strings.ContainsAny(word, "_ ") || word != strings.ToLower(word) {
			t.Errorf("adjective %q is not a bare lowercase word", word)
		}
	}
	for word := range generatedSurnames {
		if strings.ContainsAny(word, "_ ") || word != strings.ToLower(word) {
			t.Errorf("surname %q is not a bare lowercase word", word)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1_000, "1.0 kB"},
		{734_000_000, "734.0 MB"},
		{4_693_000_000, "4.7 GB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.in); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Available has to answer with a sentence a user can act on, in English,
// for every way Docker can be missing.
func TestDaemonReason(t *testing.T) {
	tests := []struct {
		name    string
		stderr  string
		wantSub string
	}{
		{
			name:    "daemon down",
			stderr:  "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?",
			wantSub: "not answering",
		},
		{
			name:    "socket missing",
			stderr:  "dial unix /var/run/docker.sock: connect: no such file or directory",
			wantSub: "not answering",
		},
		{
			name:    "socket not readable by this user",
			stderr:  "Got permission denied while trying to connect to the Docker daemon socket",
			wantSub: "not allowed",
		},
		{
			name:    "timed out",
			stderr:  "context deadline exceeded",
			wantSub: "did not answer in time",
		},
		{
			name:    "anything else is quoted back",
			stderr:  "something nobody has seen before",
			wantSub: "something nobody has seen before",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := daemonReason(tt.stderr)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("daemonReason(%q) = %q, want it to mention %q", tt.stderr, got, tt.wantSub)
			}
			if got == "" {
				t.Error("daemonReason returned an empty reason; the UI has nothing to show")
			}
		})
	}
}
