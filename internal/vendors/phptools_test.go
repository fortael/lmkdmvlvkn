package vendors

import (
	"path/filepath"
	"testing"
)

// psalmXML renders a config with the given cacheDirectory, namespaces and
// all, so the parser is exercised against the shape psalm actually writes.
func psalmXML(cacheDir string) string {
	return `<?xml version="1.0"?>
<psalm
    errorLevel="3"
    resolveFromConfigFile="true"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
    xmlns="https://getpsalm.org/schema/config"
    xsi:schemaLocation="https://getpsalm.org/schema/config vendor/vimeo/psalm/config.xsd"
    cacheDirectory="` + cacheDir + `"
>
    <projectFiles><directory name="app"/></projectFiles>
</psalm>
`
}

// psalmCache populates a directory with the files psalm writes, so it is
// recognisable as a cache.
func psalmCache(t *testing.T, dir string) string {
	t.Helper()
	write(t, filepath.Join(dir, "good_run"), "1")
	write(t, filepath.Join(dir, "file_cache", "a", "b.cache"), "x")
	write(t, filepath.Join(dir, "classlike_cache", "c.cache"), "x")
	return dir
}

// Psalm's cache is not findable by name. Every PHP project on the machine
// this was written for points cacheDirectory at a shared scratch directory —
// "storage/tmp" or "tmp" — that also holds coverage reports, a tracked
// .gitignore and, in one project, a copy of .bash_history. Offering that
// directory for deletion would destroy work that no command restores; only
// the hashed subdirectory below it is psalm's.
func TestPsalmCacheIsTheHashedSubdirectoryNotTheSharedScratchDir(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "php-service")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "psalm.xml"), psalmXML("storage/tmp"))

	scratch := mkdir(t, app, "storage", "tmp")
	write(t, filepath.Join(scratch, ".gitignore"), "*\n")
	write(t, filepath.Join(scratch, "coverage.cobertura.xml"), "<coverage/>")
	write(t, filepath.Join(scratch, ".bash_history"), "ls\n")
	cache := psalmCache(t, filepath.Join(scratch, "0c35eebf403cf91fe77a64921d76aa1ca6411d20"))

	items := scanned(t, root)

	if hasPath(items, scratch) {
		t.Error("the configured cacheDirectory was offered for deletion; it is shared scratch space holding coverage reports and a tracked .gitignore")
	}
	it := itemAt(t, items, cache)
	if it.Kind.Name != "psalm" || it.Kind.Manifest != "psalm.xml" {
		t.Errorf("kind = {Name:%q Manifest:%q}, want {psalm psalm.xml}", it.Kind.Name, it.Kind.Manifest)
	}
	if it.Project != "php-service" {
		t.Errorf("project = %q, want php-service", it.Project)
	}
}

// The hash is sha1 of the path psalm saw, not the host path: every PHP
// project here runs psalm in a container mounted at /app, so they all share
// the hash 0c35eebf...c6411d20 no matter where they sit on disk. The cache
// therefore has to be recognised by what is inside it, whatever it is called.
func TestPsalmCacheRecognisedByContentsUnderAnyName(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "psalm.xml"), psalmXML("tmp"))

	scratch := mkdir(t, app, "tmp")
	cache := psalmCache(t, filepath.Join(scratch, "d2b6c12ab2d22552b3a7faee0509e6139f633afc"))
	// A sibling that is not psalm's must be left alone.
	write(t, filepath.Join(scratch, "reports", "junit.xml"), "<testsuite/>")

	items := scanned(t, root)

	if !hasPath(items, cache) {
		t.Errorf("the hashed cache directory was not found; got %v", pathsOf(items))
	}
	if hasPath(items, filepath.Join(scratch, "reports")) {
		t.Error("a non-psalm sibling of the cache was reported")
	}
}

// A config saying cacheDirectory="." resolves to the project root. Taking it
// at face value would put the user's entire repository on a list of things
// to delete.
func TestPsalmCacheDirectoryPointingAtTheProjectIsRefused(t *testing.T) {
	for _, value := range []string{".", "", "./"} {
		root := t.TempDir()
		app := mkdir(t, root, "svc")
		write(t, filepath.Join(app, "composer.json"), "{}")
		write(t, filepath.Join(app, "psalm.xml"), psalmXML(value))
		psalmCache(t, app)

		for _, it := range scanned(t, root) {
			if it.Kind.Name == "psalm" {
				t.Errorf("cacheDirectory=%q produced psalm item %s; the project root must never be offered", value, it.Path)
			}
		}
	}
}

// Three projects here point psalm at the same absolute /tmp path. Honouring
// that would list one directory three times under three project names, and
// the first delete would silently gut the other two.
func TestPsalmCacheOutsideTheProjectIsRefused(t *testing.T) {
	shared := t.TempDir()
	psalmCache(t, shared)

	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "psalm.xml"), psalmXML(shared))

	for _, it := range scanned(t, root) {
		if it.Kind.Name == "psalm" {
			t.Errorf("a cacheDirectory outside the project produced %s; shared paths must be refused", it.Path)
		}
	}
}

// A project with no psalm config has no psalm cache, however its directories
// happen to be named.
func TestNoPsalmConfigMeansNoPsalmItem(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	psalmCache(t, mkdir(t, app, "storage", "tmp", "0c35eebf403cf91fe77a64921d76aa1ca6411d20"))

	for _, it := range scanned(t, root) {
		if it.Kind.Name == "psalm" {
			t.Errorf("reported %s without any psalm.xml to point at it", it.Path)
		}
	}
}

// PHPStan's tmpDir is read from its NEON config, which has no parser in the
// standard library — a line scan is enough for one scalar key.
func TestPhpstanTmpDirIsReadFromNeon(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "phpstan.neon"), `includes:
    - vendor/larastan/larastan/extension.neon

parameters:
    paths:
        - app/
    level: 2
    tmpDir: storage/tmp/phpstan  # analysis cache
`)
	tmp := mkdir(t, app, "storage", "tmp", "phpstan")
	write(t, filepath.Join(tmp, "resultCache.php"), "<?php return [];")
	write(t, filepath.Join(tmp, "cache", "PHPStan", "ab", "cd.php"), "<?php")

	it := itemAt(t, scanned(t, root), tmp)
	if it.Kind.Name != "phpstan" || it.Kind.Restore != "vendor/bin/phpstan analyse" {
		t.Errorf("kind = {Name:%q Restore:%q}, want {phpstan vendor/bin/phpstan analyse}", it.Kind.Name, it.Kind.Restore)
	}
	if it.Kind.Dir != filepath.Join("storage", "tmp", "phpstan") {
		t.Errorf("Dir = %q, want the configured path relative to the project", it.Kind.Dir)
	}
}

// An empty tmpDir frees nothing, and may be a shared scratch path PHPStan has
// simply not written to yet. One project on this machine is in exactly that
// state, holding only a .gitignore.
func TestPhpstanTmpDirWithoutMarkersIsNotReported(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "phpstan.neon"), "parameters:\n    tmpDir: storage/tmp\n")
	write(t, filepath.Join(app, "storage", "tmp", ".gitignore"), "*\n")

	for _, it := range scanned(t, root) {
		if it.Kind.Name == "phpstan" {
			t.Errorf("reported %s, which holds nothing PHPStan wrote", it.Path)
		}
	}
}

// PHPStan expands %rootDir% to its own installation directory inside
// vendor/. Guessing at a placeholder produces a path this app would then
// offer to delete, so an unresolvable one is left alone.
func TestPhpstanUnresolvablePlaceholderIsRefused(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "phpstan.neon"), "parameters:\n    tmpDir: %rootDir%/../../../tmp\n")
	tmp := mkdir(t, app, "tmp")
	write(t, filepath.Join(tmp, "resultCache.php"), "<?php return [];")

	for _, it := range scanned(t, root) {
		if it.Kind.Name == "phpstan" {
			t.Errorf("reported %s from an unresolvable %%rootDir%% path", it.Path)
		}
	}
}

// %currentWorkingDirectory% is the one placeholder that can be resolved from
// here with confidence.
func TestPhpstanCurrentWorkingDirectoryPlaceholderResolves(t *testing.T) {
	root := t.TempDir()
	app := mkdir(t, root, "svc")
	write(t, filepath.Join(app, "composer.json"), "{}")
	write(t, filepath.Join(app, "phpstan.neon"), "parameters:\n    tmpDir: %currentWorkingDirectory%/build/phpstan\n")
	tmp := mkdir(t, app, "build", "phpstan")
	write(t, filepath.Join(tmp, "resultCache.php"), "<?php return [];")

	if !hasPath(scanned(t, root), tmp) {
		t.Errorf("%%currentWorkingDirectory%% should resolve to the project directory")
	}
}
