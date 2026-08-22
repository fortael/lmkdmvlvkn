package vendors

import (
	"bufio"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// psalmConfigs and phpstanConfigs are the config file names each tool looks
// for, in the order it looks for them.
var (
	psalmConfigs   = []string{"psalm.xml", "psalm.xml.dist"}
	phpstanConfigs = []string{"phpstan.neon", "phpstan.neon.dist", "phpstan.dist.neon"}
)

// psalmMarkers are entries psalm writes inside its cache directory. Finding
// one is what proves a directory is psalm's cache rather than something else
// that happens to live at the configured path — which is not a theoretical
// distinction. Every PHP service on this machine points psalm at storage/tmp,
// a shared scratch directory that also holds coverage reports, a tracked
// .gitignore and, in one project, a copy of .bash_history. Offering
// storage/tmp for deletion would destroy all of that; offering only the
// subdirectory carrying these markers destroys exactly the cache.
var psalmMarkers = []string{
	"file_cache", "class_cache", "classlike_cache", "file_reference",
	"php-parser", "good_run", "composer_lock_hash", "analyzed_methods",
}

// phpstanMarkers are the two things PHPStan puts in its tmpDir: the result
// cache file and the file-cache tree. Same reasoning as psalmMarkers — a
// tmpDir shared with anything else is only safe to touch below these.
var phpstanMarkers = []string{"resultCache.php", "cache"}

func psalmKind(manifest, dir string) Kind {
	return Kind{Name: "psalm", Dir: dir, Restore: "vendor/bin/psalm", Manifest: manifest}
}

func phpstanKind(manifest, dir string) Kind {
	return Kind{Name: "phpstan", Dir: dir, Restore: "vendor/bin/phpstan analyse", Manifest: manifest}
}

// addPHPToolCaches reports the static-analysis caches belonging to the
// project at dir, if it has a psalm or PHPStan config naming one.
//
// These two kinds cannot be found by directory name the way every other kind
// is. Psalm's default cache lives outside the project altogether (the system
// temp directory), and when a project overrides it the value is whatever the
// author chose: on this machine "storage/tmp", "tmp" and an absolute
// "/tmp/tests/data/psalm_cache" are all in use, and not one project uses the
// ".psalm-cache" name one might have assumed. The config file is the only
// thing that knows.
func (w *walker) addPHPToolCaches(dir string, siblings map[string]bool) {
	for _, name := range psalmConfigs {
		if !siblings[name] {
			continue
		}
		base, ok := psalmCacheRoot(dir, name)
		if !ok {
			continue
		}
		rel := relOrBase(dir, base)
		for _, p := range dirsWithMarkers(base, psalmMarkers) {
			w.add(Item{
				Kind:        psalmKind(name, filepath.Join(rel, filepath.Base(p))),
				Path:        p,
				Project:     filepath.Base(dir),
				ProjectPath: dir,
			})
		}
	}
	for _, name := range phpstanConfigs {
		if !siblings[name] {
			continue
		}
		tmp, ok := phpstanTmpDir(dir, name)
		if !ok {
			continue
		}
		if !hasAnyMarker(tmp, phpstanMarkers) {
			// A tmpDir with none of PHPStan's own files in it is either
			// unused or already clean. Reporting it would offer the user a
			// directory that frees nothing, and risk it being a shared
			// scratch path PHPStan simply has not written to yet.
			continue
		}
		w.add(Item{
			Kind:        phpstanKind(name, relOrBase(dir, tmp)),
			Path:        tmp,
			Project:     filepath.Base(dir),
			ProjectPath: dir,
		})
	}
}

// psalmCacheRoot resolves the cacheDirectory a psalm config names, or reports
// false when there is none usable.
func psalmCacheRoot(projectDir, configName string) (string, bool) {
	raw, ok := xmlRootAttr(filepath.Join(projectDir, configName), "cacheDirectory")
	if !ok {
		return "", false
	}
	return resolveToolDir(projectDir, raw)
}

// phpstanTmpDir resolves the tmpDir a PHPStan config names.
func phpstanTmpDir(projectDir, configName string) (string, bool) {
	raw, ok := neonScalar(filepath.Join(projectDir, configName), "tmpDir")
	if !ok {
		return "", false
	}
	// PHPStan expands %rootDir% to its own installation directory inside
	// vendor/, and several other placeholders besides. Only the working
	// directory can be resolved from here with any confidence; anything else
	// containing a placeholder is left alone rather than guessed at, because
	// guessing produces a path this app would then offer to delete.
	if strings.Contains(raw, "%") {
		expanded := strings.ReplaceAll(raw, "%currentWorkingDirectory%", projectDir)
		if strings.Contains(expanded, "%") {
			return "", false
		}
		raw = expanded
	}
	return resolveToolDir(projectDir, raw)
}

// resolveToolDir turns a value read out of a tool config into an absolute
// path, and refuses everything that is not a real directory strictly inside
// the project.
//
// The "strictly inside" test is the important one. A config saying
// cacheDirectory="." resolves to the project root, and a scan that took it at
// face value would put the user's entire repository on a list of things to
// delete. Absolute paths pointing at shared locations are refused for a
// quieter reason: three projects here point psalm at /tmp/tests/data/
// psalm_cache, so honouring it would list one directory three times under
// three different project names.
func resolveToolDir(projectDir, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(projectDir, p)
	}
	p = filepath.Clean(p)
	if !within(projectDir, p) {
		return "", false
	}
	info, err := os.Lstat(p)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return p, true
}

// within reports whether path sits strictly below parent — below it, and not
// equal to it.
func within(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// relOrBase describes path relative to projectDir, for display in Kind.Dir.
func relOrBase(projectDir, path string) string {
	if rel, err := filepath.Rel(projectDir, path); err == nil {
		return rel
	}
	return filepath.Base(path)
}

// dirsWithMarkers returns the directories at or one level below base that
// carry one of the given marker entries.
//
// The extra level exists because psalm does not cache in the configured
// directory itself: Config.php appends DIRECTORY_SEPARATOR . sha1($base_dir),
// so the real cache is a 40-character hash subdirectory. The hash cannot be
// computed here — it is taken over the path psalm saw, and every PHP project
// on this machine runs psalm in a container where the project is mounted at
// /app, giving them all the identical hash 0c35eebf...c6411d20 regardless of
// where they sit on the host. Recognising the cache by what is inside it is
// the only approach that survives that.
func dirsWithMarkers(base string, markers []string) []string {
	if hasAnyMarker(base, markers) {
		return []string{base}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name())
		if hasAnyMarker(p, markers) {
			out = append(out, p)
		}
	}
	return out
}

func hasAnyMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if _, err := os.Lstat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// xmlRootAttr reads one attribute off a document's root element. Decoding
// only as far as the first StartElement keeps this cheap and, more usefully,
// tolerant: psalm.xml declares a default namespace and a schema location, and
// unmarshalling into a struct would have to mirror all of that to match.
func xmlRootAttr(path, attr string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	dec := xml.NewDecoder(io.LimitReader(f, 256<<10))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, a := range start.Attr {
			if a.Name.Local == attr {
				return a.Value, true
			}
		}
		return "", false
	}
}

// neonScalar pulls a scalar value out of a NEON file by key.
//
// NEON is PHP's own YAML dialect and there is no parser for it in the
// standard library, which this package is limited to. A line scan is enough
// for the one key needed: tmpDir is a scalar, written at most one level deep
// under "parameters", and a value this misreads simply fails the checks in
// resolveToolDir rather than producing a wrong path to delete.
func neonScalar(path, key string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(io.LimitReader(f, 256<<10))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		trimmed := strings.TrimSpace(line)
		name, value, found := strings.Cut(trimmed, ":")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if value == "" {
			continue
		}
		return value, true
	}
	return "", false
}
