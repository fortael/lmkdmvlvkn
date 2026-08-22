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
- `internal/scan` — lists directories and measures their size in the
  background via a throttled worker pool (`Scanner`); UI-agnostic.
- `internal/knowledge` — hand-curated dictionary of what each cache
  folder is, the effects of deleting it, and the literal shell commands
  a clean action is equivalent to. Folders absent from the dictionary
  are `Unknown` and never offered for cleaning.
- `internal/ui` — the only package that imports bubbletea/lipgloss.
  Split into `model.go` (state + Update), `handlers.go` (key input),
  `render.go` (state → string).

## Commands

- `make run` — run the app directly (`go run .`)
- `make build` — build the `maccleaner` binary
- `make test` — `go test ./...`
- `make cover` — coverage report, opens HTML view
- `make lint` — dockerized golangci-lint (same setup as claude-keeper)
