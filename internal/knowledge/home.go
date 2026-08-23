package knowledge

// This file is the curated Home-tab list: a flat, hand-picked set of paths
// under $HOME that are known-disposable developer/tool state. Unlike the
// Library roots nothing here is discovered — $HOME is overwhelmingly the
// user's own work (source checkouts, documents, screenshots), so it is never
// listed, only declared. Anything absent from this list is invisible to the
// app, which makes the bar for adding an entry "I am confident this
// regenerates itself", not "this looked big in du".
//
// Three rules follow from that:
//
//   - No projects or source directory ever goes on this list. ~/Projects,
//     ~/GolandProjects and friends are obvious; less obvious but equally
//     excluded are ~/go/bin (binaries the user chose to `go install`),
//     ~/.local/share (the XDG *data* directory, not a cache) and ~/.Trash
//     (the user's own files, pending their decision).
//   - Where a folder mixes cache with credentials or installed binaries —
//     ~/.cargo, ~/.gradle, ~/.rustup, ~/.docker — the entry uses CleanPaths
//     to name only the disposable subfolders, never the whole folder.
//   - A Native action is only offered when the tool ships a real executable
//     that does the job. `nvm cache clear` is a sourced shell function and
//     would not exist in the non-interactive shell we run commands in, and
//     neither ollama nor rustup nor stable cargo has a prune command at all,
//     so those entries point at the manual alternative in a comment instead
//     of pretending.
//
// HomeItems filters this list down to paths that actually exist, so entries
// for tools this machine has never had installed cost nothing and are
// expected — most of the list below is invisible on any given Mac.
var homeItems = []HomeItem{
	{
		RelPath: ".gradle",
		Entry: Entry{
			Score: Caution,
			Description: "Gradle's per-user home, shared by every Gradle and Android project on the machine and " +
				"routinely the single largest cache on a JVM machine. caches/modules-2 is every jar and aar Gradle " +
				"has ever resolved, caches/build-cache-1 is the local build cache of task outputs, wrapper/dists " +
				"holds a complete ~130 MB Gradle distribution per version any project's wrapper asked for, daemon/ " +
				"is daemon logs and native/ is extracted platform helper binaries.",
			Effects: "Stop running daemons first with `gradle --stop`. Removes only those five regenerable " +
				"subfolders: each project's next build re-downloads its dependencies and, if needed, its wrapper's " +
				"Gradle distribution — commonly several GB and a few slow minutes the first time — and re-runs tasks " +
				"the build cache would have skipped. Deliberately leaves ~/.gradle/gradle.properties and " +
				"~/.gradle/init.d alone, since those hold your signing keys, repository credentials and JVM tuning " +
				"and nothing can regenerate them.",
			Commands: []string{
				`# Stop running Gradle daemons first: gradle --stop`,
				`rm -rf ~/.gradle/caches/modules-2/*`,
				`rm -rf ~/.gradle/caches/build-cache-1/*`,
				`rm -rf ~/.gradle/wrapper/dists/*`,
				`rm -rf ~/.gradle/daemon/*`,
				`rm -rf ~/.gradle/native/*`,
			},
			CleanPaths: []string{
				"caches/modules-2/*",
				"caches/build-cache-1/*",
				"wrapper/dists/*",
				"daemon/*",
				"native/*",
			},
		},
	},
	// Blobs and manifests are always cleaned together. Deleting blobs alone
	// leaves manifests pointing at weights that no longer exist, which makes
	// `ollama list` advertise models that fail the moment you run them — a
	// worse state than having no models at all. The ed25519 keypair at the
	// top level is this machine's Ollama identity and stays put.
	{
		RelPath: ".ollama",
		Entry: Entry{
			Score: Caution,
			Description: "Ollama's data directory, almost all of which is models/blobs: the raw weight files of " +
				"every LLM you have pulled, stored content-addressed as sha256-* and shared between models built on " +
				"the same base layer. models/manifests is the tiny index mapping a name:tag such as llama3.1:8b to " +
				"the blobs it needs, and the id_ed25519 keypair beside them is this machine's Ollama identity, used " +
				"to push models and sign in to ollama.com.",
			Effects: "Quit Ollama and any running `ollama serve` first. Removes every downloaded model — blobs and " +
				"manifests together, deliberately, because deleting blobs alone leaves manifests advertising models " +
				"whose weights are gone. This is the most expensive entry here to undo: each model is a fresh " +
				"multi-gigabyte download (`ollama pull llama3.1`) over the network. Your keypair, your Modelfiles, " +
				"and anything you pushed to ollama.com are not touched. To drop models one at a time instead, " +
				"`ollama list` names them and `ollama rm <model>` removes one plus the blobs nothing else references.",
			Commands: []string{
				`# Quit Ollama and any running 'ollama serve' first`,
				`rm -rf ~/.ollama/models/blobs/*`,
				`rm -rf ~/.ollama/models/manifests/*`,
				`# to drop single models instead: ollama list, then ollama rm <model>`,
			},
			CleanPaths: []string{"models/blobs/*", "models/manifests/*"},
			// Ollama ships no bulk prune: `ollama rm` takes model names and
			// nothing else, so the native path has to enumerate them first.
			// Piping `ollama list` into `ollama rm` lets Ollama do its own
			// reference counting — a blob shared by two models survives
			// until the last model referencing it is gone — which a plain
			// rm -rf of blobs/ cannot do. NR>1 drops the NAME/ID/SIZE header
			// row, and the read loop is used instead of xargs because BSD
			// xargs still runs the command once when its input is empty.
			Native: &NativeClean{
				Description: "Removes each installed model through Ollama itself, so it can unlink shared base " +
					"layers correctly and keep its manifests consistent, instead of us deleting weight files out " +
					"from under it. The Ollama server has to be running — this is the one native command here that " +
					"talks to a daemon rather than the filesystem, and it stops with Ollama's own error if it " +
					"can't connect.",
				// Deliberately not one long pipeline. A shell pipeline's exit
				// status is its *last* command's, so piping `ollama list`
				// straight into the removal loop reports success even when
				// the list failed outright — which is exactly what happened
				// with the server stopped: nothing was removed and the app
				// still said it worked. Capturing the list first makes the
				// failure visible and stops before pretending to clean.
				Command: `models=$(ollama list) || { echo 'Ollama server is not running — start Ollama, then try again'; exit 1; }; ` +
					`names=$(echo "$models" | awk 'NR>1 {print $1}'); ` +
					`[ -n "$names" ] || { echo 'No models installed'; exit 0; }; ` +
					`for m in $names; do ollama rm "$m" || exit 1; done`,
			},
		},
	},
	{
		RelPath: ".npm",
		Entry: Entry{
			Score: Safe,
			Description: "npm's cache directory, shared by every Node project on the machine. _cacache is the " +
				"content-addressable cache of downloaded tarballs and registry metadata; _npx holds whole package " +
				"installs that `npx some-tool` created on demand and never cleaned up, often larger than the cache " +
				"itself; _prebuilds caches prebuilt native binaries and _logs keeps one debug log per failed npm " +
				"run. None of it is configuration — your registry settings and auth tokens live in ~/.npmrc, a " +
				"separate file this app never touches.",
			Effects: "Removes the entire cache. Projects with an existing node_modules keep working exactly as they " +
				"are; the next `npm install` or `npx` re-downloads what it needs from the registry, typically a few " +
				"hundred MB and one noticeably slower install. Your ~/.npmrc, its auth tokens, and every installed " +
				"node_modules are unaffected.",
			Commands: []string{
				`rm -rf ~/.npm/*`,
				`# note: npm cache clean --force only clears _cacache, not _npx or _logs`,
			},
			Native: &NativeClean{
				Description: "Lets npm verify and clear its own content-addressable cache through the supported " +
					"interface instead of us deleting the directory. It covers _cacache only, so the _npx and _logs " +
					"folders — frequently the larger half — stay behind.",
				Command: "npm cache clean --force",
			},
		},
	},
	// The next three entries name specific files rather than folders.
	// RelPath is Lstat'd directly with no glob expansion, so each crash
	// artifact has to be spelled out exactly, PID and timestamp included —
	// which is fine, since HomeItems hides the ones a machine doesn't have.
	{
		RelPath: "java_error_in_phpstorm.hprof",
		Entry: Entry{
			Score: Safe,
			Description: "A JVM heap dump written by PhpStorm when it ran out of memory: a byte-for-byte snapshot " +
				"of the entire Java heap at the instant of the crash, dropped straight into $HOME because that is " +
				"the JVM's default working directory. It exists purely so a developer can open it in a memory " +
				"profiler to diagnose that one crash, and nothing ever reads it again.",
			Effects: "Deletes a single diagnostic file. Nothing regenerates it and nothing needs it — PhpStorm, its " +
				"settings, your plugins and your projects are entirely unaffected, and there is no re-download or " +
				"rebuild cost of any kind. These dumps are as large as the IDE's heap was at crash time, so this is " +
				"routinely several gigabytes in one file. Only keep it if you are about to open it in a profiler.",
			Commands: []string{`rm -f ~/java_error_in_phpstorm.hprof`},
		},
	},
	// A plain rm -rf here genuinely fails, so the Commands show the chmod
	// that makes it work and the Effects steer to the native command. The
	// app's own delete uses os.RemoveAll and hits the same permission wall.
	{
		RelPath: "go/pkg/mod",
		Entry: Entry{
			Score: Safe,
			Description: "GOMODCACHE, the Go module cache. cache/download holds the raw .zip/.info/.mod files " +
				"fetched from the module proxy, and everything beside it is those modules extracted, one directory " +
				"per module@version, shared by every Go project on the machine. It is neither ~/go/bin (the " +
				"binaries you chose to `go install`, one directory up) nor the compiler's build cache, which lives " +
				"separately in ~/Library/Caches/go-build.",
			Effects: "The module cache is written read-only on purpose — extracted files are mode 0444 and their " +
				"directories 0555 — so deleting it by hand fails with \"permission denied\" until the permissions " +
				"are relaxed; prefer the native `go clean -modcache`, which handles that itself. Once gone, each " +
				"project's next build re-downloads its dependencies from the module proxy, commonly 1-3 GB across a " +
				"machine's worth of projects. Your source, go.mod/go.sum, installed binaries and toolchains are " +
				"untouched, and go.sum still verifies every re-download, so nothing about the resulting build changes.",
			Commands: []string{
				`# Written read-only, so a plain rm -rf fails with "permission denied" — chmod first:`,
				`chmod -R u+w ~/go/pkg/mod`,
				`rm -rf ~/go/pkg/mod/*`,
				`# both steps together are what 'go clean -modcache' does for you`,
			},
			Native: &NativeClean{
				Description: "Lets the go tool drop the module cache through its documented interface, which knows " +
					"how to get past the read-only permissions the cache is deliberately written with — the reason " +
					"a hand-rolled rm -rf needs a chmod -R u+w first.",
				Command: "go clean -modcache",
			},
		},
	},
	{
		RelPath: ".m2/repository",
		Entry: Entry{
			Score: Caution,
			Description: "Maven's local repository: every jar, pom and plugin Maven has downloaded for any project " +
				"on this machine, laid out by groupId/artifactId/version. Scoped to repository/ on purpose — " +
				"~/.m2/settings.xml sits next to it and holds server credentials, mirror definitions and proxy " +
				"settings that only exist there.",
			Effects: "Removes every cached artifact. Each project's next `mvn` run re-downloads its whole dependency " +
				"tree from Maven Central, frequently 1-2 GB and several minutes on that first build. The one caveat " +
				"that keeps this at Caution rather than Safe: SNAPSHOT artifacts you produced locally with " +
				"`mvn install` and never published to a remote repository also live here, and the only way back is " +
				"to rebuild whichever project produced them. settings.xml and its credentials are outside this " +
				"folder and untouched.",
			Commands: []string{`rm -rf ~/.m2/repository/*`},
		},
	},
	// Narrowed to the legacy `master` clone. Private spec repos added with
	// `pod repo add` are also git clones under repos/, but their remote URLs
	// exist nowhere else on disk, so wiping them would destroy something the
	// user cannot get back from us.
	{
		RelPath: ".cocoapods/repos",
		Entry: Entry{
			Score: Caution,
			Description: "CocoaPods' local clones of podspec repositories. The legacy `master` repo is a full git " +
				"clone of the entire public CocoaPods Specs database — roughly a gigabyte of history for what " +
				"amounts to a list of package versions — left over from before CocoaPods 1.8 made the CDN the " +
				"default, and read by no project that resolves through cdn.cocoapods.org.",
			Effects: "Removes only the legacy `master` clone. The trunk/CDN entry and any private spec repos your " +
				"team added with `pod repo add` are deliberately left in place, because their remote URLs are not " +
				"recorded anywhere this app could restore them from. A Podfile using " +
				"`source 'https://cdn.cocoapods.org/'` switches over transparently; one still pointing at " +
				"github.com/CocoaPods/Specs.git simply re-clones the repo on its next `pod install`.",
			Commands: []string{
				`rm -rf ~/.cocoapods/repos/master`,
				`# equivalent to: pod repo remove master`,
			},
			CleanPaths: []string{"master"},
		},
	},
	{
		RelPath: ".rustup",
		Entry: Entry{
			Score: Caution,
			Description: "rustup's home, holding the actual Rust toolchains. Each directory under toolchains/ is a " +
				"complete compiler install — rustc, cargo, the standard library, rust-docs, plus components like " +
				"clippy or rust-analyzer — and runs 1-2 GB on its own, so machines tracking both stable and nightly " +
				"or cross-compiling to extra targets accumulate several. downloads/ and tmp/ are scratch space from " +
				"interrupted installs. The rustc and cargo in ~/.cargo/bin are only rustup shims that dispatch into " +
				"these toolchains.",
			Effects: "Removes every installed toolchain plus install scratch, keeping settings.toml so rustup still " +
				"remembers your default toolchain and profile. Until you run `rustup toolchain install stable`, " +
				"`cargo` and `rustc` fail with \"toolchain is not installed\" — the shims survive but have nothing " +
				"to dispatch to — and each toolchain is a 1-2 GB download to get back. If you only want to drop " +
				"toolchains you no longer use, `rustup toolchain list` names them and " +
				"`rustup toolchain uninstall <name>` removes one at a time; rustup has no prune command that picks " +
				"for you, which is why none is offered here.",
			Commands: []string{
				`rm -rf ~/.rustup/toolchains/*`,
				`rm -rf ~/.rustup/downloads/*`,
				`rm -rf ~/.rustup/tmp/*`,
				`# reinstall afterwards with: rustup toolchain install stable`,
			},
			CleanPaths: []string{"toolchains/*", "downloads/*", "tmp/*"},
		},
	},
	{
		RelPath: ".cache",
		Entry: Entry{
			Score: Caution,
			Description: "The XDG cache directory ($XDG_CACHE_HOME), used by every cross-platform tool that follows " +
				"the Linux convention instead of macOS's ~/Library/Caches. What lands here varies by machine: " +
				"GitHub Copilot's project-context and project-index caches are frequently the largest thing in it, " +
				"alongside MCP server indexes, powerlevel10k's compiled prompt dumps and its downloaded gitstatusd " +
				"binary, and per-tool scratch from editors and CLI assistants.",
			Effects: "Clears every tool's cache in one pass. By the XDG specification nothing in here is required " +
				"and each tool must be able to regenerate it, so no configuration or credentials are lost — those " +
				"live in ~/.config and ~/.local/share, neither of which this app touches. In practice: your shell " +
				"prompt takes a beat longer on its next start while it recompiles and re-downloads gitstatusd, and " +
				"code-indexing assistants re-index the projects you open next.",
			Commands: []string{`rm -rf ~/.cache/*`},
		},
	},
	{
		RelPath: ".nvm/.cache",
		Entry: Entry{
			Score: Safe,
			Description: "nvm's download cache: the Node.js tarballs — and, for source installs, the extracted and " +
				"compiled source trees, which are by far the bigger half — that it keeps after installing a " +
				"version. Scoped to .cache deliberately: the Node versions you actually use live in " +
				"~/.nvm/versions/node and are installed software, not cache.",
			Effects: "Removes the cached downloads only. Every installed Node version keeps working, along with the " +
				"packages installed globally under each of them, and your default alias is unchanged. The only cost " +
				"is that reinstalling a version you previously had re-downloads its tarball. `nvm cache clear` does " +
				"exactly this in an interactive shell; it isn't offered as a native action because nvm is a sourced " +
				"shell function rather than an executable, so it does not exist in a non-interactive shell.",
			Commands: []string{
				`rm -rf ~/.nvm/.cache/*`,
				`# equivalent to 'nvm cache clear' in an interactive shell`,
			},
		},
	},
	// Scoped to store/ rather than the whole folder: PNPM_HOME also holds
	// the pnpm executable itself and the shims for globally installed
	// packages on standalone (non-npm) pnpm installs.
	{
		RelPath: "Library/pnpm",
		Entry: Entry{
			Score: Caution,
			Description: "PNPM_HOME on macOS. store/ is pnpm's content-addressable package store — the one real " +
				"copy of every package version pnpm has downloaded, hard-linked into each project's node_modules " +
				"rather than copied, which is what makes pnpm installs fast and disk-cheap. Versioned subfolders " +
				"(v10, v11) accumulate as the store format changes and older ones are never read again. This is the " +
				"store proper; the smaller metadata cache next to it lives in ~/Library/Caches/pnpm.",
			Effects: "Deletes the shared store, leaving the pnpm executable and any globally installed package shims " +
				"in PNPM_HOME alone. Projects with a populated node_modules keep working — the hard links keep the " +
				"files alive until the last reference goes — but every future `pnpm install` re-downloads from the " +
				"registry instead of linking locally, so installs are noticeably slower until the store refills. " +
				"Auth tokens live in ~/.npmrc, not here.",
			Commands: []string{
				`rm -rf ~/Library/pnpm/store/*`,
				`# pnpm store prune is narrower: it only drops packages nothing references`,
			},
			CleanPaths: []string{"store/*"},
			Native: &NativeClean{
				Description: "Lets pnpm evict only the packages no project still references, instead of wiping the " +
					"store wholesale and forcing every existing node_modules to be rebuilt from the network.",
				Command: "pnpm store prune",
			},
		},
	},
	{
		RelPath: "Library/Application Support/Code/CachedExtensionVSIXs",
		Display: "~/…/Code/CachedExtensionVSIXs",
		Entry: Entry{
			Score: Safe,
			Description: "The .vsix installer archives VS Code downloaded while installing or updating extensions, " +
				"kept around after the install finished. The extensions themselves are unpacked into " +
				"~/.vscode/extensions and do not read these archives again, so the folder is pure leftover " +
				"installers — it grows by one file per extension update and is never cleaned up.",
			Effects: "Quit VS Code first. Removes the downloaded archives only: every installed extension stays " +
				"installed, enabled and configured, your settings and keybindings are elsewhere entirely, and " +
				"nothing needs reinstalling. VS Code fetches a .vsix again only when it next installs or updates an " +
				"extension, which it does over the network in any case.",
			Commands: []string{
				`# Quit VS Code first`,
				`rm -rf ~/Library/Application\ Support/Code/CachedExtensionVSIXs/*`,
			},
		},
	},
	// No Native: Cargo's manual `cargo clean gc` is still nightly-only
	// (tracking issue rust-lang/cargo#13060). Only the automatic daily GC
	// stabilized, in Rust 1.88, and that has no command to invoke.
	{
		RelPath: ".cargo",
		Entry: Entry{
			Score: Caution,
			Description: "Cargo's home directory, which mixes download caches with things that must survive. " +
				"registry/cache holds the downloaded .crate archives, registry/src the same crates unpacked for the " +
				"compiler to read (usually the largest part), registry/index the crates.io metadata index, and " +
				"git/db plus git/checkouts the clones behind any git dependencies. Sitting right beside them: bin/, " +
				"which holds the rustup shims and every tool you installed with `cargo install`, and " +
				"credentials.toml, which holds your crates.io API token.",
			Effects: "Removes only the five download caches. ~/.cargo/bin is deliberately untouched, so rustup, " +
				"cargo, rustfmt, rust-analyzer and every cargo-installed binary stay on your PATH, and " +
				"credentials.toml and config.toml are left exactly as they are. Each project's next `cargo build` " +
				"re-downloads and re-extracts its dependencies from crates.io, usually a few hundred MB, with " +
				"Cargo.lock still pinning the same versions so the build itself is unchanged. Since Rust 1.88 Cargo " +
				"also garbage-collects these caches by itself about once a day, so some of this reclaims itself.",
			Commands: []string{
				`rm -rf ~/.cargo/registry/cache/*`,
				`rm -rf ~/.cargo/registry/src/*`,
				`rm -rf ~/.cargo/registry/index/*`,
				`rm -rf ~/.cargo/git/db/*`,
				`rm -rf ~/.cargo/git/checkouts/*`,
			},
			CleanPaths: []string{
				"registry/cache/*",
				"registry/src/*",
				"registry/index/*",
				"git/db/*",
				"git/checkouts/*",
			},
		},
	},
	{
		RelPath: ".bun/install/cache",
		Entry: Entry{
			Score: Safe,
			Description: "Bun's global module cache: every package version `bun install` has downloaded, stored as " +
				"name@version and hard-linked or clonefile'd into each project's node_modules instead of copied. " +
				"Scoped to install/cache — ~/.bun/bin holds the bun executable itself and anything you installed " +
				"with `bun add -g`.",
			Effects: "Removes the download cache. Existing node_modules keep working; each project's next " +
				"`bun install` re-fetches from the registry instead of linking from disk, which costs one slower " +
				"install. The bun binary, globally installed packages, and ~/.bunfig.toml are all outside this " +
				"folder and unaffected.",
			Commands: []string{`rm -rf ~/.bun/install/cache/*`},
			Native: &NativeClean{
				Description: "Bun's own cache command, which clears exactly this directory through the supported " +
					"interface. It is all-or-nothing — Bun offers no selective prune — so it removes the same thing " +
					"we would.",
				Command: "bun pm cache rm",
			},
		},
	},
	{
		RelPath: ".yarn/berry/cache",
		Entry: Entry{
			Score: Safe,
			Description: "Yarn Berry's global cache: one zip archive per package version, which Yarn links into a " +
				"project or, in zero-installs setups, copies into that project's own .yarn/cache. This is the " +
				"shared global mirror only — Yarn Classic used ~/Library/Caches/Yarn instead, and a project that " +
				"vendors its dependencies keeps its own copy inside the repository.",
			Effects: "Removes the cached archives. Projects that vendor their own .yarn/cache are unaffected " +
				"entirely; the rest re-download from the registry on their next `yarn install`. Your ~/.yarnrc.yml " +
				"and any npmAuthToken in it are outside this folder and untouched.",
			Commands: []string{
				`rm -rf ~/.yarn/berry/cache/*`,
				`# equivalent to: yarn cache clean --mirror (run from inside a project)`,
			},
		},
	},
	{
		RelPath: ".pyenv/cache",
		Entry: Entry{
			Score: Safe,
			Description: "pyenv's download cache: the CPython source tarballs `pyenv install` fetched from " +
				"python.org before compiling them. pyenv only uses this directory when it already exists, which is " +
				"why it is missing on many installs. Scoped to cache/ — the interpreters themselves live in " +
				"~/.pyenv/versions and are installed software, and ~/.pyenv/version records your global default.",
			Effects: "Removes the cached tarballs. Every installed Python version, every virtualenv built on one, " +
				"and your global and per-project version settings are untouched. Rebuilding a version you " +
				"previously installed simply re-downloads its source tarball first, then compiles as usual.",
			Commands: []string{`rm -rf ~/.pyenv/cache/*`},
		},
	},
	{
		RelPath: ".terraform.d/plugin-cache",
		Entry: Entry{
			Score: Caution,
			Description: "The shared provider plugin cache Terraform fills when plugin_cache_dir is set in " +
				"~/.terraformrc: one copy of each provider binary — aws, google and azurerm run 100-500 MB each — " +
				"that `terraform init` links into every working directory instead of downloading again per project. " +
				"Scoped to plugin-cache because ~/.terraform.d also holds credentials.tfrc.json with your Terraform " +
				"Cloud token.",
			Effects: "Removes the cached provider binaries. Terraform links working directories into this cache " +
				"rather than copying, so already-initialized directories lose those links and need `terraform init` " +
				"again before their next plan or apply — that re-downloads each provider once and refills the " +
				"cache. Your state files, .tf sources, .terraform.lock.hcl files and the Terraform Cloud token in " +
				"credentials.tfrc.json are all outside this folder and unaffected.",
			Commands: []string{
				`rm -rf ~/.terraform.d/plugin-cache/*`,
				`# every initialized working directory needs 'terraform init' again afterwards`,
			},
		},
	},
	{
		RelPath: ".gem",
		Entry: Entry{
			Score: Safe,
			Description: "RubyGems' per-user directory. specs/ is a cached copy of remote gem indexes, and " +
				"ruby/<version>/cache holds the .gem archives downloaded before installing. The gems themselves are " +
				"unpacked into ruby/<version>/gems and are installed software — `gem install --user-install` puts " +
				"real, occasionally hard-to-reproduce versions there — so only the two cache paths are on the " +
				"chopping block.",
			Effects: "Removes the downloaded .gem archives and the cached remote index. Every installed gem and its " +
				"executables keep working exactly as before; RubyGems re-fetches the index on the next " +
				"`gem install` or `gem search`, and re-downloads an archive only if you reinstall that exact gem " +
				"version.",
			Commands: []string{
				`rm -rf ~/.gem/specs/*`,
				`rm -rf ~/.gem/ruby/*/cache/*`,
			},
			CleanPaths: []string{"specs/*", "ruby/*/cache/*"},
		},
	},
	{
		RelPath: ".docker",
		Entry: Entry{
			Score: Caution,
			Description: "The Docker CLI's per-user directory — and, notably, mostly not cache at all: config.json " +
				"holds your registry logins, contexts/ the Docker endpoints you have defined, and cli-plugins/ " +
				"installed CLI plugins. The one disposable part is buildx/refs, the client-side build records " +
				"behind `docker buildx history`, which gain an entry per build and are never pruned automatically.",
			Effects: "Removes past build records only. You lose the ability to inspect, replay or open old builds " +
				"with `docker buildx history`; registry logins, contexts and CLI plugins are untouched, so nothing " +
				"signs you out. Expect megabytes, not gigabytes: Docker's real disk usage is images, containers, " +
				"volumes and the BuildKit cache inside the Linux VM, none of which is in ~/.docker — this app's " +
				"Docker tab is where that cleanup belongs.",
			Commands: []string{
				`rm -rf ~/.docker/buildx/refs/*`,
				`# equivalent to: docker buildx history rm --all`,
				`# for the real space, use this app's Docker tab (or docker system prune / docker buildx prune)`,
			},
			CleanPaths: []string{"buildx/refs/*"},
		},
	},
	{
		RelPath: ".electron-gyp",
		Entry: Entry{
			Score: Safe,
			Description: "Cached Electron header files and node-gyp metadata, downloaded once per Electron version " +
				"so native (C/C++) Node addons can be compiled against the right ABI. The Electron counterpart of " +
				"~/Library/Caches/node-gyp, written whenever a project builds native modules for Electron.",
			Effects: "Removes the cached headers. Nothing currently installed breaks; the next native-addon build " +
				"for Electron re-downloads the headers for that version first, adding a short one-time delay to " +
				"that build and nothing else.",
			Commands: []string{`rm -rf ~/.electron-gyp/*`},
		},
	},
	{
		RelPath: ".cpanm",
		Entry: Entry{
			Score: Safe,
			Description: "cpanminus's work directory: one build tree per module installation attempt under work/, " +
				"the distribution tarballs it downloaded under sources/, and build.log. cpanm keeps all of it so a " +
				"failed install can be inspected afterwards, and never cleans any of it up on its own.",
			Effects: "Removes build scratch and downloaded tarballs. Every Perl module you installed stays " +
				"installed — those went into your Perl lib directory, not here. The only thing lost is the build " +
				"log of a past failure; the next `cpanm` run recreates everything it needs from scratch.",
			Commands: []string{`rm -rf ~/.cpanm/*`},
		},
	},
	{
		RelPath: "GO-261.25134.147_22.06.2026_13.52.56.jfr",
		Entry: Entry{
			Score: Safe,
			Description: "A JDK Flight Recorder dump left in $HOME by a JetBrains IDE — the GO- prefix and build " +
				"number identify a GoLand build, and the timestamp is when the recording was taken. It is a capture " +
				"of JVM events collected for performance diagnostics, written to the JVM's working directory and " +
				"read afterwards by nothing except a human opening it in JDK Mission Control.",
			Effects: "Deletes a single diagnostic recording. The IDE, its settings, its plugins and your projects " +
				"are unaffected, and nothing regenerates or looks for it. Keep it only if you are about to send it " +
				"to JetBrains support or open it in a profiler yourself.",
			Commands: []string{`rm -f ~/GO-261.25134.147_22.06.2026_13.52.56.jfr`},
		},
	},
	{
		RelPath: "tmux-server-66577.log",
		Entry: Entry{
			Score: Safe,
			Description: "A tmux server debug log, written only when the server was started with -v/-vv and named " +
				"after that server process's PID. It records every command and protocol message the server handled " +
				"and is abandoned the moment that process exits, which — given the PID in the name — has long since " +
				"happened.",
			Effects: "Deletes one stale debug log. tmux itself, your sessions, windows, panes and ~/.tmux.conf are " +
				"unaffected, since this file belongs to a server process that is no longer running and no new " +
				"server will ever append to it.",
			Commands: []string{`rm -f ~/tmux-server-66577.log`},
		},
	},
}
