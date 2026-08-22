# CLAUDE.md

## Language policy

Everything in this project must be written in English: code comments,
UI copy/labels, error messages, commit messages, and this file itself —
regardless of what language the user is communicating in during the
session. Do not localize any in-app text unless the user explicitly asks
for a specific string to be in another language.

## Project

A terminal UI (Bubble Tea + Lipgloss) for cleaning macOS cache/junk
directories, modeled after ../claude-keeper's architecture and style.

- `main.go` — thin entrypoint.
- `internal/scan` — lists directories and measures them in the background
  via a throttled worker pool (`Scanner`); UI-agnostic. Sizes are
  *on-disk* usage (allocated blocks, hard links counted once), never
  `Size()` — macOS stores VM images as sparse files, where the logical
  length can be twenty times what the file occupies. `Scanner`'s pending
  queue is unbounded because `Enqueue` is called from the UI loop, which
  must never block.
- `internal/knowledge` — hand-curated dictionary of what each folder is,
  the effects of deleting it, and the literal shell commands a clean
  action is equivalent to. Folders absent from the dictionary are
  `Unknown` and never offered for cleaning.
  - Every lookup is scoped by `Root`, because the same folder name means
    different things in different places: `Caches/Google` is a disposable
    disk cache, `Application Support/Google` is the Chrome profile with
    saved passwords. One table per root, one file per table.
  - `Protect` marks storage the app refuses to touch at all, including
    the manual-delete override (OrbStack's VM, whose disk image holds
    every Docker image, container and volume).
  - `AnnotateOrphan` flags folders whose owning app is uninstalled. It
    only informs — it never raises a `Score` or unlocks an action.
- `internal/docker` — read-only Docker inventory via the CLI. Classifies
  images shared by several containers as worth keeping, and dangling
  images / anonymous volumes as the residue of one-off experiments.
  Never runs a destructive command itself; it only returns the string.
- `internal/vendors` — finds reinstallable dependency directories
  (`node_modules`, composer `vendor`, psalm caches, …) under `$HOME`,
  gated on the matching manifest so a Go `vendor/` is never mistaken for
  a PHP one. `ModTime` is the *project's* last edit, not the dependency
  directory's, since that's what answers "am I still working here".
- `internal/history` — append-only JSONL log of every deletion under
  `~/.maccleaner/`, backing the Results tab.
- `internal/ui` — the only package that imports bubbletea/lipgloss.
  Split into `model.go` (state + Update), `handlers.go` (key input),
  `render.go` (state → string), `batch.go` (multi-select execution),
  `sources.go` (Docker/Vendors adapters), `results.go`.
  - Seven tabs, each with its own navigation stack: System Data (merges
    five `~/Library` roots into one table), Leftovers (folders whose
    owning app is uninstalled), Home (a curated flat list — `$HOME` is
    never browsed, since it is mostly the user's own work), Vendors,
    Applications (descriptive only; editing a signed `.app` bundle makes
    Gatekeeper refuse to launch it), Docker, and Results.
  - **Batch cleaning** is the primary flow: space ticks rows, `c` runs
    them in one job, in selection order, preferring each tool's own
    native command. Cleaning one row at a time re-sorts a
    thousand-row table under the cursor, which is unusable.
  - Docker rows are *virtual*: their path is an identifier, not a file.
    `batchStep.virtual` keeps them away from `os.RemoveAll` and `Stat`.

## Commands

- `make run` — run the app directly (`go run .`)
- `make build` — build the `maccleaner` binary
- `make test` — `go test ./...`
- `make cover` — coverage report, opens HTML view
- `make lint` — dockerized golangci-lint (same setup as claude-keeper)
