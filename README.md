<div align="center">

**A terminal disk cleaner for macOS that explains itself before it deletes anything.**

Every folder it offers to clean comes with a hand-written answer to *what is this, what breaks if it goes,
and what exactly will run.* Folders nobody has researched are never offered at all.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-macOS-000000?logo=apple&logoColor=white)](#requirements)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble%20Tea-FF75B7)](https://github.com/charmbracelet/bubbletea)

</div>

```
   System Data       Leftovers       Home       Vendors       Applications       Docker       Results

System Data                                          ⇱ TOP (g)   ⇲ END (G)   ↺ SORT (s)   ☐ ONLY SELECTED 1 (f)
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│     TYPE        NAME                                                SIZE▾ RELATIVE SIZE      MOD   SAFE  NAT │
│  ✓  AppSupp     📁 Claude ›                                        9.8 GB ██████████████████ now    ★★☆      │
│     AppSupp     📁 JetBrains  (versions ›)                         8.0 GB ██████████████░░░░ 8m     ★★☆      │
│▸    Cache       📁 JetBrains  (versions ›)                         6.4 GB ███████████░░░░░░░ now    ★★☆      │
│     GroupC      📁 HUAQ24HBR6.dev.orbstack ›  🔒 protected         4.9 GB ████████░░░░░░░░░░ 2m     ★☆☆      │
│  ·  GroupC      📁 6N38VWS5BX.ru.keepcoder.Telegram ›              3.4 GB ██████░░░░░░░░░░░░ 11m    ★★☆      │
│  ·  AppSupp     📁 Steam ›                                         1.9 GB ███░░░░░░░░░░░░░░░ 321d   ★★☆      │
│  ·  AppSupp     📁 Google ›                                        1.2 GB ██░░░░░░░░░░░░░░░░ 1m     ★★☆      │
│  ·  AppSupp     📁 Code ›                                        674.5 MB █░░░░░░░░░░░░░░░░░ 58d    ★★☆      │
│     AppSupp     📁 iMazing ›                                     657.5 MB █░░░░░░░░░░░░░░░░░ 414d   ★☆☆      │
│  ·  AppSupp     📁 dev.warp.Warp-Stable ›                        617.1 MB █░░░░░░░░░░░░░░░░░ 2d     ★★☆      │
│  ·  Cache       📁 go-build ›                                    235.3 MB █░░░░░░░░░░░░░░░░░ 3m     ★★★   ✓  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│scroll: wheel, PgUp/PgDn, or click →                                                                 ▲   ▼    │
│JetBrains                                                                                                     │
│/Users/zakhar/Library/Caches/JetBrains                                                                        │
│                                                                                                              │
│Size: 6.4 GB                                                                                                  │
│Modified: now                                                                                                 │
│Safety: ★★☆ caution — may lose cached state                                                                   │
│Enter to open — holds several version caches, see inside to clean them                                        │
│↓ more below (PgDn)                                                                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
                     ╭─────────────────────────────────────╮  ╭──────────────────────────╮
                     │   CLEAN 1 SELECTED  (7.7 GB)  (c)   │  │   CLEAR SELECTION  (x)   │
                     ╰─────────────────────────────────────╯  ╰──────────────────────────╯
↑/↓ move   space select   c run batch   a all   x clear   f only-selected   g/G top/end   s reset sort   …
```

---

## Why another cleaner

Most disk cleaners show you a number and a checkbox. You tick it because the number is big, and you find
out afterwards what you lost.

This one is built on the opposite bet: **the explanation is the product.** Behind it is a hand-curated
dictionary of **279 entries** describing real folders on a real Mac — what writes to them, what survives a
clean, what has to be quit first, and the literal shell commands the action is equivalent to. If a folder
isn't in the dictionary, it shows as `?` and the app refuses to clean it. You can still delete it yourself,
but the app won't pretend to know something it doesn't.

A few consequences of taking that seriously:

**It measures what you actually get back.** Sizes are allocated blocks, not apparent length, and hard links
are counted once — the same thing `du` does. macOS stores VM images as sparse files, so the naive reading is
not slightly off, it's absurd:

```
OrbStack's data.img.raw     ls -l:  228.3 GB      du / this app:  12.6 GB
```

**It prefers the tool's own cleanup.** Where a tool knows how to clean up after itself — `brew cleanup -s`,
`go clean -cache`, `pnpm store prune`, `docker rmi` — that runs instead of a glob, because the tool knows
what of its own state is still referenced and a wildcard never will.

**Some things it refuses to touch at all.** OrbStack's group container is one sparse image holding every
Docker image, container and volume you own. There is nothing this app can offer there that OrbStack's own
interface doesn't do better, so the folder is shown, explained, and left alone — not even the manual-delete
override applies.

```
│ GroupC  📁 HUAQ24HBR6.dev.orbstack ›  🔒 protected     4.9 GB │
                    ╭──────────────────────────────────────────────────────────────╮
                    │   PROTECTED  (managed by its own app — clean it from there)   │
                    ╰──────────────────────────────────────────────────────────────╯
```

---

## Quick start

Requires **Go 1.26+** and **macOS**. It cross-compiles for Linux, but everything it knows is macOS —
`~/Library`, sandboxed app containers, `defaults read` for bundle identifiers — so there is nothing useful
for it to find anywhere else.

```sh
git clone https://github.com/fortael/lmkdmvlvkn.git
cd lmkdmvlvkn
make run
```

Or build a binary:

```sh
make build     # produces ./maccleaner
./maccleaner
```

Nothing is installed, no daemon is started, and nothing is deleted without a confirmation screen that lists
the exact commands first.

---

## The seven tabs

| | Tab | What it finds |
|---|---|---|
| `1` | **System Data** | Five `~/Library` roots merged into one table — Application Support, Group Containers, Caches, Logs, Containers |
| `2` | **Leftovers** | Folders whose owning app is no longer installed, with a delete-all |
| `3` | **Home** | A curated list of disposable developer state in `$HOME` — never a directory browser |
| `4` | **Vendors** | `node_modules`, composer `vendor`, Rust `target`, psalm caches… grouped by project |
| `5` | **Applications** | Installed apps and what's inside their bundles — descriptive only |
| `6` | **Docker** | Images, containers, volumes and build cache, with provenance |
| `7` | **Results** | Everything this app has ever deleted, and how much it freed |

### Batch cleaning is the main flow

Cleaning one row at a time is unusable on a table of a thousand entries, because every deletion re-sorts
everything under your cursor. So you walk the list ticking rows with <kbd>space</kbd>, and run the lot as one
job with <kbd>c</kbd>. Steps execute in the order you picked them, native command where one exists, and the
button carries the running total.

A failing step doesn't abort the rest — cleaning isn't transactional, and one read-only folder says nothing
about the next twenty. Failures are collected and reported at the end.

### Docker, without `docker inspect`

An anonymous volume is normally a 64-hex name and a size. That's a black box, and you can't make a decision
from it. This tab reconstructs where the thing came from:

```
What is inside it
  Looks like     MySQL 8 data directory
  Databases      clip-plus-service, clip-plus-service-test
                 A database data directory holding 2 application databases. Those names are the
                 project this volume belonged to. Nothing else has these bytes.

Where it came from
  Created by     a container that no longer exists
  Mounted at     /var/lib/mysql — inferred from what is inside it, not recorded anywhere
```

Images get their source repository and revision, the base system, the first build steps with Docker's
`#(nop)` noise stripped, and the biggest layers with the command that created each. Images referenced by
several containers are marked ★ **shared** and rated keep — the compose base images you're tired of
re-pulling.

### Vendors knows when you last worked on a project

`ModTime` is the last change to the **project's own source**, not to the dependency directory. A
`node_modules` installed a year ago in a project you edited yesterday is not stale; a project untouched for
two years is exactly what you're looking for. Every match is gated on its manifest, so a Go `vendor/` is
never mistaken for a PHP one.

### Ollama models, with the server switched off

`ollama rm` talks to a daemon that is usually stopped, which leaves deleting the whole blobs directory as the
only option. This reads the model store directly, so you get per-model removal offline — plus orphaned blobs,
which `ollama rm` can never reach because it only removes what a manifest names.

```
model   llama3.1:8b               4.6 GB   80d   ★★☆  ✓
model   bge-m3:latest             1.1 GB   87d   ★★☆  ✓
model   nomic-embed-text:latest  261.6 MB  87d   ★★☆  ✓
```

---

## Keys

Everything is also clickable — the tab bar, column headers to sort, the navigation buttons and the action
buttons. The mouse wheel scrolls, which matters on compact Mac keyboards with no <kbd>PgUp</kbd>/<kbd>PgDn</kbd>.

| | Navigate | | Select & batch | | Act |
|---|---|---|---|---|---|
| <kbd>↑</kbd><kbd>↓</kbd> <kbd>k</kbd><kbd>j</kbd> | Move | <kbd>space</kbd> | Tick this row | <kbd>d</kbd> | Clean this one |
| <kbd>g</kbd> <kbd>G</kbd> | Top / end | <kbd>a</kbd> | Select all | <kbd>n</kbd> | Native clean |
| <kbd>Enter</kbd> | Open folder | <kbd>c</kbd> | Run the batch | <kbd>D</kbd> | Delete outright |
| <kbd>Esc</kbd> <kbd>⌫</kbd> | Go up | <kbd>f</kbd> | Show only selected | <kbd>r</kbd> | Rescan |
| <kbd>1</kbd>…<kbd>7</kbd> <kbd>Tab</kbd> | Switch tabs | <kbd>x</kbd> | Clear selection | <kbd>q</kbd> | Quit |
| <kbd>s</kbd> | Reset sort | | | | |

---

## The safety model

Four ratings, and they gate what the UI will offer:

| | Rating | Meaning |
|---|---|---|
| `★★★` | **Safe** | A pure, disposable cache the owning app recreates transparently |
| `★★☆` | **Caution** | Probably fine, but costs something recoverable — a re-download, a re-index |
| `★☆☆` | **Risky** | Touches saved state: settings, sessions, licences |
| `?` | **Unknown** | Not researched. Never offered for cleaning, at any rating |

On top of that:

- **`🔒 Protected`** — refused entirely, including the manual override.
- **`⚠ leftover`** — no installed app claims this bundle identifier. Deliberately hedged: background
  updaters and Safari extensions legitimately own folders without shipping an `.app`, so it informs and
  never raises a rating or unlocks an action.
- **Granular cleans.** Where the disposable and the irreplaceable share a folder, only specific subpaths are
  removed. Chrome's profile is the canonical case: the 4 GB Gemini Nano model goes, your passwords stay.
- **Every deletion is logged** to `~/.maccleaner/history.jsonl` — what, where, when, and how much it really
  freed, measured before and after rather than estimated.

---

## Architecture

```
main.go                 thin entrypoint
internal/scan           lists and measures; on-disk sizes, hard links counted once
internal/knowledge      the curated dictionary — 279 entries, scoped by root
internal/docker         read-only Docker inventory and provenance
internal/vendors        reinstallable dependency directories under $HOME
internal/ollama         Ollama's model store, read without the daemon
internal/history        append-only JSONL deletion log
internal/ui             the only package that imports Bubble Tea / Lipgloss
```

Two decisions worth knowing before reading the code:

**Dictionary lookups are scoped by root.** `Caches/Google` is Chrome's disposable disk cache;
`Application Support/Google` is the profile holding your saved passwords. Keying by bare folder name would
let the first answer for the second, so every lookup carries where it was found.

**The size scanner's queue is unbounded.** `Enqueue` is called from the UI's update loop, which must never
block: a bounded channel deadlocks the whole app once it fills, because the blocked UI then stops draining
results and every worker blocks in turn. Five Library roots produce well over a thousand entries.

---

## Development

```sh
make test     # go test ./...
make cover    # coverage report, opens the HTML view
make lint     # dockerized golangci-lint
```

**170 tests**, and the dictionary itself is tested: every entry with granular clean paths must have a
matching command for each, everything cleanable must carry a description and an effects note, no command may
target a home directory or the filesystem root, and no two dictionary files may define the same key.

### Adding a folder to the dictionary

Pick the file for the root it lives under (`caches.go`, `appsupport.go`, `apple.go`, `ides.go`, …) and add an
entry:

```go
"some-tool": {
    Score: Safe,
    Description: "What this folder is, who writes to it, and why it exists.",
    Effects:     "Exactly what happens after cleaning — and what is NOT affected.",
    Commands:    []string{`rm -rf ~/Library/Caches/some-tool/*`},
    Native: &NativeClean{
        Description: "Why letting the tool clean itself is better than a glob here.",
        Command:     "some-tool cache clean",
    },
},
```

Two rules the tests will hold you to: if you set `CleanPaths`, the number of non-comment `Commands` must
match it exactly and in order; and everything you make cleanable needs both a `Description` and an `Effects`.

If you can't verify what a folder does, **leave it out**. Absent means `Unknown`, which the app already
refuses to clean — that's a perfectly good outcome, and much better than a confident wrong answer.
