---
description: Research an unknown cache folder and add it to the knowledge base
argument-hint: <folder-path-or-name>
---

Research the cache folder given as `$ARGUMENTS` (a full path like
`~/Library/Caches/FamilyCircle`, or just the folder name like
`FamilyCircle`) and add a real, accurate entry for it to
`internal/knowledge/knowledge.go`, following that file's existing
conventions exactly.

## 1. Ground it in the real filesystem first

Extract the folder's base name (the `db` map key). If the path exists on
this machine, inspect it before searching the web — this catches cases
where the folder's real contents don't match what a generic web search
implies (this happened before: a folder assumed to be Go's module cache
turned out to be an unrelated 0-byte helper cache):

- `ls -la <path>` and `find <path> -maxdepth 2` to see what's actually
  inside.
- `du -sh <path>/*` to see what's actually large enough to matter.
- Note any subfolder that looks like local state rather than a rebuildable
  cache (local history, session tokens, databases with user data, anything
  named like a keychain/credential store).

If the path doesn't exist on this machine, skip straight to research.

## 2. Research on the web

Search for what owns this folder and why: e.g. `"<name>" macOS cache
folder`, `"~/Library/Caches/<name>"`, and the likely owning app/daemon
name if the folder name hints at one (reverse-DNS-style names like
`com.vendor.app` usually identify the app directly). Figure out:

- Which application/daemon/tool writes here, and why it exists.
- Whether it's a pure, disposable cache (rebuilds transparently) or holds
  anything that isn't cheaply reconstructible — auth tokens, session
  state, local history, license data, user-generated content.
- Where that app stores *actual* auth/license state if this folder isn't
  it (usually `~/Library/Application Support/<vendor>` or the system
  Keychain) — say so explicitly in Effects if relevant, the same way the
  JetBrains and browser entries do.

## 3. Decide the safety score

Using `knowledge.Score` from `internal/knowledge/knowledge.go`:

- **Safe (3)** — pure, disposable cache; the owning app recreates it
  transparently.
- **Caution (2)** — probably fine, but costs the app something
  recoverable (re-downloads, re-indexing, a slower next launch, losing
  minor UI state).
- **Risky (1)** — deletion will likely affect the app's saved state
  (settings, login sessions, purchase history) — delete at your own risk.
- **Unknown (0)** — don't add an entry at all; if research is genuinely
  inconclusive, leave the folder undocumented rather than guessing.

## 4. Write the entry

Match the tone and structure of existing entries in the `db` map exactly:

- **Description**: 2–3 sentences — what it is, who writes to it, why it
  exists/grows. State facts you actually found, not boilerplate.
- **Effects**: plain language, concrete specifics — what disappears, what
  doesn't, any prerequisite ("quit the app first"), and explicit
  reassurance about credentials/passwords when relevant (only claim this
  if you actually verified where those are stored).
- **Commands**: literal `rm -rf ~/Library/Caches/<Name>/*` (or the precise
  equivalent), plus a `# Quit X first` comment line if needed. If the
  folder is risky to wipe wholesale but has specific safe-to-clear
  subpaths (like the JetBrains entries' `CleanPaths`), use that pattern
  instead of a blanket wipe.
- Only set `Container: true` if this folder is a pure umbrella holding
  several unrelated versioned/sub-app caches (see the `JetBrains` entry
  for the pattern) — most folders are not this.
- Only set `Native` if there's a real, well-known CLI command from the
  owning tool itself that does the equivalent cleanup (see `Homebrew`,
  `go-build`, `pnpm` for the pattern) — don't invent one.

Add the entry to the `db` map in `internal/knowledge/knowledge.go`.

## 5. Verify and report

Run `gofmt -l -w internal/knowledge/knowledge.go && go build ./... && go vet ./...`
to confirm it compiles cleanly. Report back: what the folder is, the score
you gave it and why, and anything you found that contradicts what the
folder's name might suggest.
