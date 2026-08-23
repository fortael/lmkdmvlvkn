package ui

// Batch cleaning: the user ticks rows going down the table, then runs the
// whole list as one job.
//
// The alternative — clean, wait, watch the table re-sort, hunt for the
// next row — is unusable on a listing of over a thousand entries, because
// every single clean moves everything else. Selecting is decoupled from
// executing so the user reviews once, commits once, and walks away.

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lmkdmvlvkn/internal/history"
	"lmkdmvlvkn/internal/knowledge"
	"lmkdmvlvkn/internal/scan"
)

// batchAction is how one selected row gets cleaned.
type batchAction int

const (
	// batchNative runs the owning tool's own cleanup command. Preferred
	// wherever it exists: the tool knows what of its own state is still
	// referenced, which a glob never can.
	batchNative batchAction = iota
	// batchClean removes the dictionary's CleanPaths, or the folder's
	// contents when it defines none.
	batchClean
	// batchDelete removes the whole path. Used where the entry is the
	// thing to remove rather than a folder to empty — leftovers from
	// uninstalled apps, and reinstallable dependency directories.
	batchDelete
)

func (a batchAction) String() string {
	switch a {
	case batchNative:
		return "native"
	case batchDelete:
		return "delete"
	default:
		return "clean"
	}
}

// batchStep is one queued unit of work, resolved at selection time so the
// confirm screen shows exactly what will run.
type batchStep struct {
	path string
	name string
	// source is the tab label recorded in the deletion history, so the log
	// says where a removal came from.
	source string
	action batchAction
	// command is the shell command for batchNative.
	command string
	// cleanPaths are the globs for batchClean; empty means the folder's
	// entire contents.
	cleanPaths []string
	// estimate is how much we expect this step to free. It is an estimate
	// and not a promise: a native command decides for itself what to
	// remove, and a folder can grow or shrink between selecting and
	// running.
	estimate int64
	// estimateReady is false while a granular CleanPaths measurement is
	// still in flight, so the total can be shown as a lower bound rather
	// than silently under-reporting.
	estimateReady bool
	// virtual marks a step whose path is an identifier rather than a file
	// — Docker objects, whose bytes live inside the VM's disk image. Such
	// a step must never be stat'd or handed to os.RemoveAll, and the space
	// it frees can only come from what Docker reported.
	virtual bool
}

// batchProgressMsg reports one finished step, or the end of the run.
type batchProgressMsg struct {
	index int
	total int
	name  string
	freed int64
	err   error
	done  bool
}

// batchResult accumulates what actually happened, for the summary line.
type batchResult struct {
	running   bool
	index     int
	total     int
	current   string
	freed     int64
	failures  []string
	completed int
}

// runNative executes one of the dictionary's fixed, hand-written commands
// through the shell.
//
// Setsid detaches the child into its own session so it has no controlling
// terminal. Some tools (Homebrew's Ruby-based cleanup among them) open
// /dev/tty directly for progress output when they detect an interactive
// terminal, which bypasses the pipe entirely and writes straight into our
// alt-screen buffer — corrupting it in a way Bubble Tea cannot repaint
// over. Without a controlling terminal that open fails and the tool falls
// back to plain stdout, which we do capture.
func runNative(dir, command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+nativePath())
	// A working directory only makes sense if it is one; entries whose
	// target is a file (or has already vanished) run from the parent.
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		cmd.Dir = filepathDir(dir)
	} else if err == nil {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	return lastLine(string(out)), err
}

// toolDirs are where the tools our native commands invoke actually live.
//
// Every single one of them — ollama, brew, pnpm, npm, go — is in
// /opt/homebrew/bin on this machine, and docker is in /usr/local/bin.
// A process launched from Finder or an IDE inherits launchd's minimal
// PATH (/usr/bin:/bin:/usr/sbin:/sbin), which contains none of them, so
// every native clean would fail with "command not found" depending only
// on how the app happened to be started. Prepending these makes the
// behaviour the same either way.
var toolDirs = []string{
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
	"/usr/local/bin",
}

// nativePath returns the PATH to run native commands with: the inherited
// one, plus the usual tool locations and the user's own ~/.local/bin,
// with duplicates removed so it stays readable in error messages.
func nativePath() string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	for _, d := range strings.Split(os.Getenv("PATH"), ":") {
		add(d)
	}
	for _, d := range toolDirs {
		add(d)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home + "/.local/bin")
	}
	for _, d := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		add(d)
	}
	return strings.Join(dirs, ":")
}

// filepathDir avoids importing path/filepath twice under different names
// in this file; it is only ever called with an absolute path.
func filepathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "/"
}

// spawnBatchCmd runs every step in order, reporting each one as it
// finishes. Steps run sequentially and deliberately so: several of them
// shell out to package managers that take a lock, and a predictable order
// is what the user was promised when they built the list top-down.
//
// A failing step does not abort the run. Cleaning is not transactional —
// one folder being read-only says nothing about the next twenty — so the
// error is recorded and the batch carries on, with the failures reported
// at the end.
func spawnBatchCmd(steps []batchStep, ch chan<- batchProgressMsg) tea.Cmd {
	return func() tea.Msg {
		for i, s := range steps {
			freed, err := runStep(s)
			ch <- batchProgressMsg{index: i, total: len(steps), name: s.name, freed: freed, err: err}
		}
		ch <- batchProgressMsg{total: len(steps), done: true}
		return nil
	}
}

// runStep performs one removal and reports what it actually freed.
//
// Every removal in the app funnels through here — the single-item d/n/D
// actions as much as a batch — so measurement and history logging can't
// drift apart between the two paths.
func runStep(s batchStep) (int64, error) {
	// A virtual step has no file to measure or remove: the only thing it
	// can legitimately do is run its tool's own command, and the only
	// figure available is what that tool already reported.
	if s.virtual {
		if s.action != batchNative || s.command == "" {
			return 0, errVirtualStep
		}
		_, err := runNative("/", s.command)
		freed := s.estimate
		if err != nil {
			freed = 0
		}
		logStep(s, freed, err)
		return freed, err
	}

	// Measure either side rather than trusting the estimate: this is the
	// only figure that reflects what was really freed, including when a
	// native command decided to keep something.
	before, _, _ := scan.PathSize(s.path)

	var err error
	switch s.action {
	case batchNative:
		_, err = runNative(s.path, s.command)
	case batchDelete:
		err = os.RemoveAll(s.path)
	default:
		err = cleanDir(s.path, s.cleanPaths)
	}

	var freed int64
	if after, _, aerr := scan.PathSize(s.path); aerr != nil {
		// The path is gone entirely — the expected outcome of batchDelete.
		freed = before
	} else {
		freed = before - after
	}
	if freed < 0 {
		freed = 0
	}

	logStep(s, freed, err)
	return freed, err
}

// errVirtualStep guards the one combination that must never happen: a
// Docker object reaching a filesystem removal path.
var errVirtualStep = errors.New("this object can only be removed through its own tool")

// logStep records one removal. A history write failing must never mask the
// cleanup's own outcome: the deletion already happened, and the caller
// needs to hear about that, not about the log.
func logStep(s batchStep, freed int64, err error) {
	rec := history.Record{
		Time:   time.Now(),
		Name:   s.name,
		Path:   s.path,
		Source: s.source,
		Action: historyAction(s.action),
		Freed:  freed,
	}
	if err != nil {
		rec.Err = err.Error()
	}
	_ = history.Append(rec)
}

func historyAction(a batchAction) history.Action {
	switch a {
	case batchNative:
		return history.ActionNative
	case batchDelete:
		return history.ActionDelete
	default:
		return history.ActionClean
	}
}

// waitForBatchCmd delivers the next progress report, re-arming itself for
// the one after — the same streaming pattern the size scanner uses.
func waitForBatchCmd(ch <-chan batchProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// selectableStep resolves what cleaning e on the current tab would mean,
// or reports false when the row offers nothing.
//
// Native is preferred over our own deletion wherever the dictionary
// defines it, which is what the user asked for: let the tool that owns the
// data decide what of it is still needed.
func (m Model) selectableStep(e *scan.Entry) (batchStep, bool) {
	if e == nil {
		return batchStep{}, false
	}
	k := m.knowledgeFor(e)
	if k.Protected {
		return batchStep{}, false
	}

	step := batchStep{path: e.Path, name: e.Name}

	// Docker rows are identifiers, not files: removal is always the CLI
	// command Docker itself supplied.
	if e.Root == string(knowledge.RootDocker) {
		if k.Native == nil {
			return batchStep{}, false
		}
		step.virtual = true
		step.action = batchNative
		step.command = k.Native.Command
		step.estimate, step.estimateReady = e.Size, e.SizeReady
		return step, true
	}

	switch {
	// Leftovers and vendored dependencies are removed outright: the point
	// of both tabs is that the directory itself should not exist.
	case m.activeTab == tabLeftovers || m.activeTab == tabVendors:
		step.action = batchDelete
	case k.Native != nil:
		step.action = batchNative
		step.command = k.Native.Command
	case k.CanClean():
		step.action = batchClean
		step.cleanPaths = k.CleanPaths
	default:
		return batchStep{}, false
	}

	step.estimate, step.estimateReady = m.estimateFor(e, k)
	return step, true
}

// estimateFor predicts how much a step frees. A whole-folder action frees
// what the folder occupies; a granular one frees only its CleanPaths,
// which has to be measured in the background first.
func (m Model) estimateFor(e *scan.Entry, k knowledge.Entry) (int64, bool) {
	if len(k.CleanPaths) == 0 {
		if e.SizeReady {
			return e.Size, true
		}
		return 0, false
	}
	info, ok := m.reclaimCache[e.Path]
	if !ok || !info.ready {
		return 0, false
	}
	return info.total, true
}
