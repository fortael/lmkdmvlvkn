package knowledge

// This file is the dictionary for ~/Library/Caches — the folder apps are
// supposed to put throwaway, regenerable data in. Entries here can afford
// to be more permissive than any other root, since by Apple's own
// convention nothing in Caches is meant to survive.

// jetbrainsCleanPaths are folders/files inside one JetBrains IDE version's
// cache directory that are pure, disposable, re-downloadable cache —
// confirmed by inspecting a real GoLand cache directory. Deliberately
// excludes caches/, index/ (the actual project indexes — deleting these
// forces a full reindex) and LocalHistory (JetBrains' built-in per-file
// edit history, which unlike the rest of this directory can't be
// re-downloaded once gone), so a clean here never triggers a reindex or
// loses history, only re-downloads.
var jetbrainsCleanPaths = []string{
	"tmp/hprof-temp",
	"acp-agents/.runtimes/node/*/npm-cache",
	"acp-agents/.runtimes/node/*/bin",
	"jcef_cache/*",
	"plugins/imageCache",
	"plugins/update-meta",
	"icon-cache-v2.db",
	"splash",
}

// jetbrainsCachePattern describes one JetBrains IDE version's cache
// directory. Effective may promote this to a whole-folder delete when a
// newer install of the same IDE supersedes it.
var jetbrainsCachePattern = patternEntry{
	re: jetbrainsVersionRe,
	build: func(name string) Entry {
		commands := make([]string, 0, len(jetbrainsCleanPaths))
		for _, p := range jetbrainsCleanPaths {
			commands = append(commands, `rm -rf ~/Library/Caches/JetBrains/`+name+`/`+p)
		}
		return Entry{
			Score: Caution,
			Description: "A per-version cache for one specific JetBrains IDE install — the version is baked into " +
				"the folder name (" + name + "). Besides project indexes and local edit history, it also holds " +
				"downloaded plugin/icon caches, the embedded browser's cache, and downloaded AI-agent runtimes " +
				"(the acp-agents folder alone is often 1GB+). None of this is your JetBrains account login or " +
				"license — that's stored separately under ~/Library/Application Support/JetBrains, not here, so " +
				"cleaning this folder never signs you out or invalidates your license.",
			Effects: "Only removes disposable, re-downloadable cache: the embedded browser cache, downloaded " +
				"AI-agent runtimes/npm cache, plugin marketplace icon/metadata cache, and crash-report temp " +
				"files. Deliberately leaves your project indexes and Local History alone, so this does not " +
				"trigger a reindex and does not lose any edit history — just re-downloads a few hundred MB to " +
				"a couple GB of caches next time they're needed.",
			Commands:   commands,
			CleanPaths: jetbrainsCleanPaths,
		}
	},
}

var cachesDB = map[string]Entry{
	"Homebrew": {
		Score: Safe,
		Description: "Homebrew's cache folder, only the largest part of which — downloads/ — is the raw download " +
			"cache for formula bottles (precompiled binaries) and source tarballs fetched during `brew install` / " +
			"`brew upgrade`. The rest (api/, bootsnap/, descriptions.json, ...) is Homebrew's own metadata/API " +
			"cache, which this app leaves alone since it's used every time you run a brew command, not just during " +
			"installs.",
		Effects: "Only downloads/ is removed — the downloaded package archives. Every formula, cask, and app " +
			"you've installed stays installed and configured exactly as it was; Homebrew simply re-downloads an " +
			"archive the next time it needs one it no longer has cached. No credentials, tokens, or passwords are " +
			"stored here.",
		Commands:   []string{`rm -rf ~/Library/Caches/Homebrew/downloads/*`},
		CleanPaths: []string{"downloads/*"},
		Native: &NativeClean{
			Description: "Lets Homebrew itself decide what's safe to remove — old formula/cask versions, stale " +
				"downloads, and outdated cache entries — rather than us wiping the download cache wholesale.",
			Command: "brew cleanup -s",
		},
	},
	"go-build": {
		Score: Safe,
		Description: "The Go compiler's build cache: compiled package objects keyed by a content hash, shared across " +
			"every Go project on the machine so repeated builds and `go test` runs can reuse work instead of " +
			"recompiling from scratch. It is not tied to any single project and grows as you build/test Go code.",
		Effects: "Deletes every cached compiled object. Your source code, go.mod/go.sum files, and installed " +
			"toolchains are untouched. The next `go build` or `go test` in any project recompiles from scratch and " +
			"will feel noticeably slower once; after that the cache rebuilds itself transparently.",
		Commands: []string{`rm -rf ~/Library/Caches/go-build/*`, `# equivalent to: go clean -cache`},
		Native: &NativeClean{
			Description: "Lets the go tool clear its own build cache through its documented interface instead of " +
				"us deleting GOCACHE's contents by hand.",
			Command: "go clean -cache",
		},
	},
	"go": {
		Score: Safe,
		Description: "A small helper cache written by the Go toolchain (an \"imports\" subfolder used by tooling " +
			"like gopls/go list when resolving import paths). Despite the name, this is not the Go module cache — " +
			"GOMODCACHE defaults to ~/go/pkg/mod, entirely outside ~/Library/Caches, and GOCACHE (compiled build " +
			"objects) is the separate go-build folder next to this one.",
		Effects: "Removes a small, purely disposable lookup cache. Nothing about your modules, downloaded " +
			"dependencies, or compiled build objects lives here, so this has no effect on either — it's rebuilt " +
			"automatically and you likely won't notice any slowdown at all.",
		Commands: []string{`rm -rf ~/Library/Caches/go/*`},
	},
	"goimports": {
		Score: Safe,
		Description: "A small cache written by the goimports tool (used by editors and `gofmt`-adjacent tooling to " +
			"automatically add/remove Go import lines) to speed up repeated lookups of package import paths.",
		Effects: "Removes the cached import-path lookups. goimports keeps working immediately and rebuilds this " +
			"cache the next time it runs; the only cost is a barely noticeable delay on its next invocation.",
		Commands: []string{`rm -rf ~/Library/Caches/goimports/*`},
	},
	"gopls": {
		Score: Safe,
		Description: "Cache for gopls, the official Go language server that editors (VS Code, GoLand, Zed, Neovim) " +
			"use for autocomplete, go-to-definition, and diagnostics. It stores parsed/type-checked package data so " +
			"it doesn't have to reanalyze your whole module on every keystroke.",
		Effects: "Removes the cached analysis data. Your editor keeps working, but gopls has to reanalyze open " +
			"projects from scratch on its next run — expect a slower, laggier autocomplete/diagnostics experience " +
			"for a few seconds to a minute the first time you open a project afterward.",
		Commands: []string{`rm -rf ~/Library/Caches/gopls/*`},
	},
	"pnpm": {
		Score: Caution,
		Description: "pnpm's content-addressable package store and metadata cache — the actual downloaded npm " +
			"package contents that pnpm hard-links into every project's node_modules instead of copying them, which " +
			"is what makes pnpm installs fast and disk-efficient in the first place.",
		Effects: "Deletes the shared package store. Existing projects with an already-populated node_modules keep " +
			"working, but every future `pnpm install` (in any project) has to re-download packages from the " +
			"registry instead of linking them locally, so installs will be noticeably slower until the store " +
			"rebuilds. No credentials are stored here (npm auth tokens live in ~/.npmrc).",
		Commands: []string{`rm -rf ~/Library/Caches/pnpm/*`, `# equivalent to: pnpm store prune`},
		Native: &NativeClean{
			Description: "Lets pnpm prune only unreferenced packages from its store instead of wiping the whole " +
				"thing, so packages still used by an existing node_modules aren't force-evicted.",
			Command: "pnpm store prune",
		},
	},
	"node-gyp": {
		Score: Safe,
		Description: "Cached Node.js header files and prebuilt artifacts that node-gyp downloads once per Node " +
			"version so it can compile native (C/C++) Node addons without re-fetching headers every time.",
		Effects: "Removes the cached headers. Nothing currently installed breaks; the next time any project needs " +
			"to compile a native addon, node-gyp re-downloads the headers for your Node version first, adding a " +
			"short one-time delay to that build.",
		Commands: []string{`rm -rf ~/Library/Caches/node-gyp/*`},
	},
	"ms-playwright": {
		Score: Caution,
		Description: "Downloaded browser binaries (Chromium, Firefox, WebKit builds — often several hundred MB " +
			"each) that Playwright installs once and reuses across all projects on the machine for browser " +
			"automation and end-to-end testing.",
		Effects: "Deletes every downloaded browser binary. Any project that runs Playwright tests will fail on its " +
			"next run until it re-downloads the browsers it needs (`npx playwright install`), which requires " +
			"network access and can take a few minutes depending on connection speed.",
		Commands: []string{`rm -rf ~/Library/Caches/ms-playwright/*`, `# re-download with: npx playwright install`},
	},
	"electron": {
		Score: Safe,
		Description: "Cached prebuilt Electron framework binaries, downloaded by the `electron` npm package's " +
			"post-install step whenever a project depends on Electron for building desktop apps.",
		Effects: "Removes the cached binaries. The next `npm install` in a project that depends on Electron " +
			"re-downloads the matching Electron binary automatically; nothing else is affected.",
		Commands: []string{`rm -rf ~/Library/Caches/electron/*`},
	},
	"electron-builder": {
		Score: Safe,
		Description: "Cache used by electron-builder — the tool that packages Electron apps into installers " +
			"(.dmg, .exe, etc.) — for downloaded packaging dependencies like winCodeSign, nsis, and app icon " +
			"generation tools.",
		Effects: "Removes the cached packaging tools. The next time you run an electron-builder packaging step, it " +
			"re-downloads whatever tooling it needs before continuing, adding a one-time delay to that build only.",
		Commands: []string{`rm -rf ~/Library/Caches/electron-builder/*`},
	},
	"JetBrains": {
		Score: Caution,
		Description: "Not one cache but a container of them: one subfolder per installed JetBrains IDE version " +
			"(e.g. GoLand2026.1, GoLand2026.2, WebStorm2026.1) plus a Toolbox folder for the JetBrains Toolbox App. " +
			"Each version's folder holds that install's project indexes, local edit history, and downloaded " +
			"plugin/AI-agent caches — kept separate per version so upgrading never corrupts an older install. Your " +
			"JetBrains account login and license are not stored here — they live under ~/Library/Application " +
			"Support/JetBrains — so nothing in this folder can sign you out or invalidate a license.",
		Effects: "Open this folder instead of cleaning it here: versions superseded by a newer install of the same " +
			"IDE (e.g. an old GoLand2026.1 once GoLand2026.2 is installed) are fully disposable and rated safe to " +
			"delete outright, while the version you're still actively using gets a narrower, conservative cleanup " +
			"that only clears re-downloadable caches (embedded browser cache, AI-agent runtimes, plugin/icon " +
			"caches) without touching its indexes or Local History.",
		Container: true,
	},
	"Zed": {
		Score: Caution,
		Description: "Cache for the Zed editor: downloaded language servers, extensions, and other tooling that " +
			"Zed fetches on demand the first time you open a project using a given language.",
		Effects: "Removes every downloaded language server and extension. Zed itself still opens fine, but the " +
			"next time you open a project, it re-downloads whatever language servers/extensions that project needs " +
			"— this requires network access and briefly disables autocomplete/diagnostics for that language until " +
			"the download finishes.",
		Commands: []string{`rm -rf ~/Library/Caches/Zed/*`},
	},
	"dev.warp.Warp-Stable": {
		Score: Safe,
		Description: "Cache for the Warp terminal app — rendered UI assets, block/session scratch data, and local " +
			"telemetry buffers it writes as you use the terminal.",
		Effects: "Removes cached UI assets and scratch data only. Your shell history, saved workflows, and settings " +
			"live elsewhere and are unaffected; Warp simply rebuilds this cache the next time it launches.",
		Commands: []string{`# Quit Warp first`, `rm -rf ~/Library/Caches/dev.warp.Warp-Stable/*`},
	},
	"dev.kiro.desktop.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Sparkle's \"ShipIt\" auto-update helper, used transiently " +
			"by the Kiro desktop app while installing an update and normally cleaned up automatically afterward.",
		Effects: "Removes stale updater scratch files only. Kiro itself, its settings, and your data are completely " +
			"unaffected — this folder is only ever touched during an in-progress update.",
		Commands: []string{`rm -rf ~/Library/Caches/dev.kiro.desktop.ShipIt/*`},
	},
	"dev.kdrag0n.MacVirt": {
		Score: Risky,
		Description: "Cache for MacVirt, the bundle identifier OrbStack ships under — a lightweight VM/container " +
			"runtime for running Linux VMs and Docker containers on macOS. Depending on version this can include VM " +
			"disk overlay data, downloaded base images, or just UI/update scratch files. Note that the bulk of " +
			"OrbStack's disk usage is not here at all: the Linux VM disk image lives under ~/Library/Group " +
			"Containers/HUAQ24HBR6.dev.orbstack.",
		Effects: "Risky because, unlike a typical browser/IDE cache, some virtualization tools do stage VM disk " +
			"state under Caches rather than Application Support. Deleting this while a VM is running or the app is " +
			"open can corrupt VM state; quit the app fully first and expect to re-download any base VM images " +
			"afterward. No account passwords are stored here, but treat this one more carefully than a plain app " +
			"cache.",
		Commands: []string{`# Quit the app completely first`, `rm -rf ~/Library/Caches/dev.kdrag0n.MacVirt/*`},
	},
	"com.docker.docker": {
		Score: Caution,
		Description: "Docker Desktop's own cache folder — UI state, update-check results, and log scratch space " +
			"for the Docker Desktop application itself. This is separate from the actual Linux VM disk image that " +
			"stores your images/containers/volumes, which lives under ~/Library/Containers, not here.",
		Effects: "Quit Docker Desktop first. Clearing this does not remove any Docker images, containers, volumes, " +
			"or the VM disk — those are untouched. You'll just lose some UI preferences (window size, recently " +
			"viewed tabs) and Docker Desktop will re-check for updates on next launch.",
		Commands: []string{`# Quit Docker Desktop first`, `rm -rf ~/Library/Caches/com.docker.docker/*`},
	},
	"com.google.GoogleUpdater": {
		Score: Safe,
		Description: "Google Software Update helper cache — small scratch data and update-check results used to " +
			"keep Chrome and other installed Google apps up to date in the background.",
		Effects: "Removes cached update-check state only. At most this causes one extra update check the next time " +
			"the updater runs; no app data, history, or credentials of any kind live here.",
		Commands: []string{`rm -rf ~/Library/Caches/com.google.GoogleUpdater/*`},
	},
	"Google": {
		Score: Safe,
		Description: "Chrome's (and other Google apps') on-disk rendering cache: the HTTP disk cache for page " +
			"resources (images, scripts, stylesheets), the GPU shader cache, and thumbnail previews. Browsing " +
			"history, cookies, saved passwords, and site data (localStorage/IndexedDB) are stored elsewhere, under " +
			"~/Library/Application Support/Google, not in this folder.",
		Effects: "Quit Chrome first. This clears the disk cache that lets pages load instantly on repeat visits — " +
			"expect pages to reload resources from the network the first time you revisit them, and a brief GPU " +
			"shader recompile. Your browsing history, cookies, saved logins, and passwords are stored outside this " +
			"folder and are not touched by this action.",
		Commands: []string{`# Quit Chrome first`, `rm -rf ~/Library/Caches/Google/*`},
	},
	"com.hnc.Discord": {
		Score: Caution,
		Description: "Discord desktop app cache: image/GIF/emoji cache, the GPU shader cache, and downloaded " +
			"update modules. Your login session, message history, and settings are stored in a separate " +
			"Application Support folder, not here.",
		Effects: "Quit Discord first. Clears cached images and rendering data — expect avatars, emoji, and images " +
			"in channels to reload from the network the first time you see them again. You stay logged in and keep " +
			"all message history; Discord will also re-download its update modules on next launch.",
		Commands: []string{`# Quit Discord first`, `rm -rf ~/Library/Caches/com.hnc.Discord/*`},
	},
	"com.hnc.Discord.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Sparkle's \"ShipIt\" auto-update helper for Discord, used " +
			"transiently while installing an update.",
		Effects:  "Removes stale updater scratch files only; Discord itself and your account are unaffected.",
		Commands: []string{`rm -rf ~/Library/Caches/com.hnc.Discord.ShipIt/*`},
	},
	"com.microsoft.VSCode": {
		Score: Caution,
		Description: "VS Code's cache: GPU shader cache, extension host scratch data, and workspace storage " +
			"indexes used to speed up reopening recently used folders. Your extensions, settings, and source files " +
			"are stored elsewhere and are not part of this folder.",
		Effects: "Quit VS Code first. Rebuilt automatically on next launch; you may lose some per-workspace UI " +
			"state such as recent search history or cached extension activation data, and the first launch " +
			"afterward may feel slightly slower while caches rebuild. Installed extensions and settings are not " +
			"affected.",
		Commands: []string{`# Quit VS Code first`, `rm -rf ~/Library/Caches/com.microsoft.VSCode/*`},
	},
	"com.microsoft.VSCode.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Sparkle's \"ShipIt\" auto-update helper for VS Code, used " +
			"transiently while installing an update.",
		Effects:  "Removes stale updater scratch files only; VS Code itself, extensions, and settings are unaffected.",
		Commands: []string{`rm -rf ~/Library/Caches/com.microsoft.VSCode.ShipIt/*`},
	},
	"com.anthropic.claudefordesktop": {
		Score: Caution,
		Description: "Claude desktop app cache: UI assets, GPU shader cache, and session scratch data. Your " +
			"conversation history is synced to your account and stored server-side / in a separate Application " +
			"Support folder, not in this cache.",
		Effects: "Quit Claude Desktop first. Rebuilt automatically on next launch. This will sign you out of the " +
			"app, so you'll need to log in again next time you open it — but your conversation history is not " +
			"stored in this folder and is not lost.",
		Commands: []string{`# Quit Claude Desktop first`, `rm -rf ~/Library/Caches/com.anthropic.claudefordesktop/*`},
	},
	"com.anthropic.claudefordesktop.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Sparkle's \"ShipIt\" auto-update helper for Claude " +
			"Desktop, used transiently while installing an update.",
		Effects:  "Removes stale updater scratch files only; the app and your login session are unaffected.",
		Commands: []string{`rm -rf ~/Library/Caches/com.anthropic.claudefordesktop.ShipIt/*`},
	},
	"claude-cli-nodejs": {
		Score: Caution,
		Description: "Cache for the Claude Code CLI: npm/node scratch data and downloaded assets the CLI keeps " +
			"around between invocations to start up faster.",
		Effects: "Removes the CLI's local scratch cache. The CLI regenerates whatever it needs on its next run; " +
			"you may see a one-time re-fetch of some assets and a slightly slower first startup. Your logged-in " +
			"session and API credentials are stored separately (in your config directory), not here.",
		Commands: []string{`rm -rf ~/Library/Caches/claude-cli-nodejs/*`},
	},
	"com.openai.chat": {
		Score: Caution,
		Description: "ChatGPT desktop app cache: UI assets, GPU shader cache, and session scratch data. Your " +
			"conversation history is tied to your account and stored server-side, not in this cache.",
		Effects: "Quit the ChatGPT app first. Rebuilt automatically on next launch. This signs you out of the app, " +
			"so you'll need to log in again — but your conversation history lives on your account, not in this " +
			"folder, and is not lost.",
		Commands: []string{`# Quit the ChatGPT app first`, `rm -rf ~/Library/Caches/com.openai.chat/*`},
	},
	"ChatGPTHelper": {
		Score: Safe,
		Description: "A small helper-process cache for the ChatGPT desktop app's background/menu-bar helper " +
			"process, separate from the main app's cache.",
		Effects: "Removes cached helper-process scratch data only; regenerated automatically the next time the " +
			"helper runs. No account data is stored here.",
		Commands: []string{`rm -rf ~/Library/Caches/ChatGPTHelper/*`},
	},
	"us.zoom.xos": {
		Score: Caution,
		Description: "Zoom client cache: virtual background assets, meeting UI resources, and downloaded update " +
			"packages. Your meeting/account settings live in a separate Application Support folder.",
		Effects: "Quit Zoom first. Zoom re-downloads virtual backgrounds and other assets the next time you use " +
			"them, and re-checks for updates on next launch. You stay logged in; no meeting history or credentials " +
			"are stored in this folder.",
		Commands: []string{`# Quit Zoom first`, `rm -rf ~/Library/Caches/us.zoom.xos/*`},
	},
	"com.setapp.DesktopClient": {
		Score: Caution,
		Description: "Setapp desktop client cache: the app catalog data and thumbnails it displays in its browse " +
			"UI, refreshed periodically from Setapp's servers.",
		Effects: "Quit Setapp first. Rebuilt automatically; you may briefly see a stale or empty app catalog until " +
			"it resyncs on next launch. Your Setapp subscription, installed apps, and login are not affected.",
		Commands: []string{`# Quit Setapp first`, `rm -rf ~/Library/Caches/com.setapp.DesktopClient/*`},
	},
	"com.macpaw.CleanMyMac-setapp": {
		Score: Caution,
		Description: "CleanMyMac (Setapp edition) cache: results and scratch data from previous scans of your " +
			"system, kept so it doesn't have to rescan everything to show you the same report twice.",
		Effects: "Quit CleanMyMac first. Rebuilt automatically; you lose previously cached scan results (you'll " +
			"need to re-run a scan to see them again), but no files CleanMyMac was tracking are deleted by this " +
			"action — only its own cached report data.",
		Commands: []string{`# Quit CleanMyMac first`, `rm -rf ~/Library/Caches/com.macpaw.CleanMyMac-setapp/*`},
	},
	"eu.exelban.Stats": {
		Score: Safe,
		Description: "Cache for the Stats menu-bar system monitor app — small scratch data for the graphs and " +
			"history it displays in the menu bar.",
		Effects: "Removes cached graph/history scratch data only. Stats rebuilds it automatically and keeps " +
			"monitoring normally; no settings or credentials are stored here.",
		Commands: []string{`rm -rf ~/Library/Caches/eu.exelban.Stats/*`},
	},
	"com.apple.Safari": {
		Score: Risky,
		Description: "Safari's cache: the disk cache for page resources (images, scripts, fonts), plus, on some " +
			"macOS versions, per-site favicon and preview data used for tab/session restore. Browsing history, " +
			"cookies, and saved passwords are stored in separate system databases, not in this folder.",
		Effects: "Quit Safari and close all windows first. Usually safe — you'll just see pages reload resources " +
			"from the network on first revisit — but this folder has occasionally been reported to interfere with " +
			"open-tab/session restore on some macOS versions if Safari isn't fully quit first. Your browsing " +
			"history, cookies, and saved passwords (in Keychain) are not stored here and are unaffected either way.",
		Commands: []string{`# Quit Safari and close all windows first`, `rm -rf ~/Library/Caches/com.apple.Safari/*`},
	},
	"com.apple.Safari.SafeBrowsing": {
		Score: Safe,
		Description: "Safari's locally cached copy of Apple's Safe Browsing (malware/phishing) block-list " +
			"database, downloaded and updated periodically in the background.",
		Effects: "Removes the local block-list copy. Safari re-downloads a fresh copy automatically the next time " +
			"it checks for updates; until then, Safe Browsing protection may briefly rely on an online lookup " +
			"instead. No browsing history or personal data is stored here.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.Safari.SafeBrowsing/*`},
	},
	"com.apple.Music": {
		Score: Caution,
		Description: "Apple Music app cache: album artwork thumbnails and a local buffer of streamed track data " +
			"used to avoid re-buffering songs you've played recently.",
		Effects: "Quit Music first. Rebuilt automatically; artwork re-downloads and recently streamed songs need " +
			"to re-buffer from the network on next play. Your library, playlists, and Apple ID sign-in are stored " +
			"elsewhere and are unaffected.",
		Commands: []string{`# Quit Music first`, `rm -rf ~/Library/Caches/com.apple.Music/*`},
	},
	"com.apple.iTunes": {
		Score: Safe,
		Description: "A legacy cache folder from the old iTunes app, mostly unused and left behind on modern " +
			"macOS versions that replaced iTunes with separate Music/TV/Podcasts apps.",
		Effects: "Safe leftover cruft — nothing on a current macOS version actively reads or writes here, so " +
			"removing it has no effect on any running app.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.iTunes/*`},
	},
	"com.apple.python": {
		Score: Safe,
		Description: "Cache written by Apple's bundled system Python framework — compiled bytecode (.pyc-style) " +
			"and resource caches for the Python interpreter that ships with macOS.",
		Effects: "Removes cached compiled bytecode. Regenerated transparently and automatically the next time any " +
			"tool invokes the system Python; you might notice an imperceptible one-time delay on that next call.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.python/*`},
	},
	"com.apple.Spotlight": {
		Score: Risky,
		Description: "A Spotlight helper cache tied to the state of macOS's system-wide search indexer, which " +
			"tracks what has and hasn't been indexed yet across your disk.",
		Effects: "Deleting this can make Spotlight think large parts of your disk need reindexing, which is a " +
			"CPU- and disk-intensive background process (mdworker running hot) that can take a long time on a " +
			"large disk, and search results may be incomplete or stale until it finishes. Only clear this if " +
			"you're actively troubleshooting broken Spotlight search — not as routine cleanup.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.Spotlight/*`, `# Spotlight will rebuild its index afterward — this can take a while`},
	},
	"com.apple.akd": {
		Score: Risky,
		Description: "Cache for akd, Apple's Apple ID/iCloud authentication kit daemon — it holds state related " +
			"to Apple ID sign-in and token refresh for iCloud-backed services.",
		Effects: "Can force Apple ID-dependent services (iCloud Drive, iCloud Mail, Messages, Find My, etc.) to " +
			"re-authenticate, which may prompt you to re-enter your Apple ID password or two-factor code. Your " +
			"Apple ID password itself is stored securely in the system Keychain, not in this cache, and is never " +
			"lost — only the cached sign-in state is. Only clear this if you're troubleshooting Apple ID sign-in " +
			"problems, not as routine cleanup.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.akd/*`},
	},
	"CloudKit": {
		Score: Risky,
		Description: "A shared local cache of CloudKit-synced data, used by many Apple and third-party apps that " +
			"store their data in iCloud (Notes, Reminders, and plenty of third-party apps with iCloud sync) to " +
			"avoid re-downloading everything from iCloud on every launch.",
		Effects: "Forces every app that uses CloudKit to re-download its iCloud-synced data from scratch the next " +
			"time it launches, which can be slow and may briefly show that app's data as missing/empty until the " +
			"resync completes. Nothing is deleted from iCloud itself — this is only the local cached copy — but " +
			"several apps can be affected at once since the cache is shared.",
		Commands: []string{`rm -rf ~/Library/Caches/CloudKit/*`},
	},
}
