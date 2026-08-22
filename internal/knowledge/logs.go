package knowledge

// This file holds the dictionaries for two roots that sit at opposite ends
// of the confidence scale.
//
// ~/Library/Logs is the easiest root in the app. Everything in it is
// diagnostic text written so a human can read it after something has
// already gone wrong — no app reads its own log back to restore state — so
// almost every entry here is Safe, and the ones that aren't are only held
// back by how useful the history is, never by what breaks. It is also the
// only root that mixes plain files in with directories (Warp writes
// ~/Library/Logs/warp.log straight into it), so several entries below
// describe a single file rather than a folder.
//
// ~/Library/Containers is the most dangerous. A container is a sandboxed
// app's entire home directory: Data/Documents is that app's Documents
// folder, Data/Library/Application Support is where it keeps saved state,
// and Data/Library/Cookies is where it keeps its login session. Deleting a
// container doesn't clear a cache, it factory-resets the app. So entries
// here default to Risky with no clean action at all, and the handful that
// do offer one name the exact cache subpaths to remove and never touch the
// folder as a whole.

// logsFileClean is the CleanPaths value for an entry that describes a
// single file instead of a directory. "." resolves, via filepath.Join,
// back to the entry's own path, so the clean action removes that one file.
// The usual empty CleanPaths can't be used here: it means "delete this
// folder's children", which fails outright on something that is not a
// folder.
var logsFileClean = []string{"."}

// logsWarpRotated builds the entry for one of Warp's rotated log files.
// Warp keeps warp.log plus warp.log.old.0 through warp.log.old.4, each a
// frozen snapshot of an earlier stretch of terminal sessions; they differ
// only in age, so the prose is generated rather than copied five times.
func logsWarpRotated(n string) Entry {
	name := "warp.log.old." + n
	return Entry{
		Score: Safe,
		Description: "A rotated Warp terminal log — what warp.log was renamed to when Warp rolled its log over. " +
			"Warp keeps five of these numbered backups (warp.log.old.0 through warp.log.old.4) directly in " +
			"~/Library/Logs rather than in a subfolder, and nothing appends to them any more: each is a frozen " +
			"record of an earlier stretch of terminal sessions. Individually they routinely reach 20MB or more.",
		Effects: "Deletes one finished log file. Warp never reads its old logs back, so this cannot affect the " +
			"running app, and your shell history, saved workflows, themes and Warp login are all stored elsewhere " +
			"and are untouched. You don't even need to quit Warp first — nothing holds a rotated file open. The " +
			"only thing lost is the ability to look back at what Warp was doing during that older period.",
		Commands:   []string{`rm -f ~/Library/Logs/` + name},
		CleanPaths: logsFileClean,
	}
}

// jetbrainsLogPattern describes one JetBrains IDE version's log directory.
// Unlike the same version's folder under Application Support — which holds
// settings, keymaps and plugin configuration and is therefore never
// offered for a wholesale delete — a log directory is disposable no matter
// how current the version is, so there is no Effective-style promotion
// here and no conservative subpath-only variant: it is always Safe to
// empty completely.
var jetbrainsLogPattern = patternEntry{
	re: jetbrainsVersionRe,
	build: func(name string) Entry {
		return Entry{
			Score: Safe,
			Description: "The log directory for one specific JetBrains IDE install, with the version baked into the " +
				"folder name (" + name + "). It holds the rotated idea.log files, a thread dump directory for every " +
				"UI freeze the IDE noticed, indexing-diagnostic reports, telemetry event dumps, and logs from the " +
				"embedded Chromium browser and any AI agents that ran. The telemetry and freeze dumps are usually " +
				"the bulk of it, which is how a single version's logs reach a gigabyte or more.",
			Effects: "Empties the whole folder, which is safe regardless of whether this version is the one you " +
				"still use: the IDE writes logs but never reads them back. Your settings, keymaps, plugins and " +
				"licence live under ~/Library/Application Support/JetBrains, and your project indexes and Local " +
				"History live under ~/Library/Caches/JetBrains — none of it is here, so nothing reindexes and " +
				"nothing signs out. The single cost is losing the crash and freeze history that JetBrains support " +
				"asks for when you report a hang or an exception; after this you'd have to reproduce the problem " +
				"to hand them anything.",
			Commands: []string{`rm -rf ~/Library/Logs/JetBrains/` + name + `/*`},
		}
	},
}

var logsDB = map[string]Entry{
	"JetBrains": {
		Score: Caution,
		Description: "Not one log but a container of them: one subfolder per installed JetBrains IDE version (e.g. " +
			"GoLand2026.2, PhpStorm2026.2), plus folders for the Toolbox App, the shared Daemon/Air background " +
			"services, and any Fleet or remote-development client that has ever run. It is routinely the largest " +
			"thing in ~/Library/Logs — several gigabytes — because every version keeps rotated idea.log files, " +
			"telemetry dumps, indexing diagnostics, and a directory of thread dumps for each UI freeze it saw.",
		Effects: "Open this folder rather than cleaning it here: each per-version log directory is rated and cleaned " +
			"individually, and all of them are safe to empty outright. Nothing in this tree is ever read back by an " +
			"IDE — logs exist for you and for JetBrains support, not for the product — so no settings, indexes, " +
			"plugins or licences are involved either way.",
		Container: true,
	},
	"Claude": {
		Score: Safe,
		Description: "Log directory for the Claude desktop app. It holds the Electron main-process log and its " +
			"rotations (main.log, main2.log, main3.log — around 10MB each), the renderer log for the web view " +
			"(claude.ai-web.log), and per-subsystem logs for MCP servers, SSH sessions and the local Cowork VM " +
			"(mcp.log, ssh.log, coworkd.log, vzgvisor.log). All of it is plain text written purely for debugging.",
		Effects: "Removes the diagnostic history and nothing else. Your conversations belong to your Claude account, " +
			"your settings and MCP server configuration live in Application Support, and your login session is kept " +
			"outside this folder — you stay signed in with everything intact. Quit the app first if you want the " +
			"space back immediately: macOS keeps a deleted file's blocks allocated for as long as a running process " +
			"still holds it open.",
		Commands: []string{`# Quit Claude Desktop first`, `rm -rf ~/Library/Logs/Claude/*`},
	},
	"warp.log": {
		Score: Safe,
		Description: "The Warp terminal's current log file, written directly into ~/Library/Logs instead of into a " +
			"subfolder of its own. Warp appends to it continuously while it runs — startup and GPU/renderer state, " +
			"AI and sync requests, block bookkeeping — so on a machine where Warp is the daily terminal it grows " +
			"steadily until Warp rotates it out to warp.log.old.0.",
		Effects: "Deletes the active log file. Your shell history, saved workflows, themes and Warp login live " +
			"elsewhere and are untouched, and Warp opens a fresh log the next time it launches. Quit Warp first if " +
			"you want the disk space back straight away — while the app is running it holds this file open, so the " +
			"blocks are only released once it exits. The only loss is the record of what Warp did recently, which " +
			"matters solely if you were about to file a bug report.",
		Commands:   []string{`# Quit Warp first`, `rm -f ~/Library/Logs/warp.log`},
		CleanPaths: logsFileClean,
	},
	"warp.log.old.0": logsWarpRotated("0"),
	"warp.log.old.1": logsWarpRotated("1"),
	"warp.log.old.2": logsWarpRotated("2"),
	"warp.log.old.3": logsWarpRotated("3"),
	"warp.log.old.4": logsWarpRotated("4"),
	"zoom.us": {
		Score: Safe,
		Description: "Zoom's log directory. crashlog/ holds a small usage record the client writes around a crash, " +
			"and zoom_feedback/ holds .zenc files — Zoom's own encrypted memory and diagnostic dumps, staged on " +
			"disk so they can be attached if you ever use the in-app \"Report a problem\" flow. Your meeting " +
			"history, settings and Zoom sign-in are kept in Application Support, not here.",
		Effects: "Removes the crash and diagnostic dumps. Zoom keeps working exactly as before, you stay signed in, " +
			"and nothing about past or scheduled meetings changes. The one real consequence: if Zoom support has " +
			"asked you for logs covering a specific past meeting, those are gone and you would have to reproduce " +
			"the problem to generate new ones.",
		Commands: []string{`rm -rf ~/Library/Logs/zoom.us/*`},
	},
	"Setapp": {
		Score: Safe,
		Description: "Setapp's log directory, with one timestamped file per client session (com.setapp " +
			"2026-08-12--00-01-05-145.log). The client starts a new file every time it launches and never prunes " +
			"the old ones, so this grows steadily but slowly — on the order of a hundred kilobytes a day.",
		Effects: "Removes the session logs. Your Setapp subscription, sign-in and installed apps are stored " +
			"elsewhere and are completely unaffected; the client writes a fresh log the moment it next starts. " +
			"Nobody but Setapp support would ever want these files.",
		Commands: []string{`rm -rf ~/Library/Logs/Setapp/*`},
	},
	"switchboard": {
		Score: Safe,
		Description: "Log directory for the Switchboard app, written by electron-log: main.log for the current run " +
			"plus main.old.log for the previous one, in the timestamped and level-tagged format every electron-log " +
			"app uses. The contents are shell/session bookkeeping and per-session terminal activity. Because the " +
			"library only ever keeps two files, this folder is self-limiting at a couple of megabytes.",
		Effects: "Removes both log files. Nothing reads them back, so the app's behaviour, its configuration and " +
			"any scheduled work are all unaffected — it simply opens a new main.log on its next run.",
		Commands: []string{`rm -rf ~/Library/Logs/switchboard/*`},
	},
	"wootonpad": {
		Score: Safe,
		Description: "Log directory for the WootonPad desktop app, written by electron-log. main.log records " +
			"timestamped, level-tagged lines covering shell profile detection, the app's local MCP server " +
			"sessions, and the terminal busy/idle activity it tracks per session.",
		Effects: "Removes the log file. The app's settings, scheduled tasks and project state live elsewhere and " +
			"are untouched; it opens a fresh main.log the next time it starts.",
		Commands: []string{`rm -rf ~/Library/Logs/wootonpad/*`},
	},
	"Electron": {
		Score: Safe,
		Description: "The fallback directory electron-log writes to when an Electron app hasn't set a product name " +
			"of its own — so this is not any one app's folder but whichever unnamed Electron build happened to " +
			"run. It holds main.log in the same timestamped, level-tagged format as the other electron-log folders " +
			"here, and stays small because the library rotates at a fixed size.",
		Effects: "Removes the log file. Nothing reads it back, so no app changes behaviour and whichever build owns " +
			"it recreates main.log on its next launch. Worth opening before you delete it if the name means " +
			"nothing to you: the contents are usually the quickest way to identify which app has been writing here.",
		Commands: []string{`rm -rf ~/Library/Logs/Electron/*`},
	},
	"Zed": {
		Score: Safe,
		Description: "Zed's log directory: Zed.log, the editor's own diagnostic log covering language server " +
			"startup, extension loading and errors, plus telemetry.log, the local record of the usage events Zed " +
			"would send if telemetry is enabled. Both are tens of kilobytes and rotate on their own.",
		Effects: "Removes both files. Your Zed settings, keymap, installed extensions and open projects are stored " +
			"elsewhere and are not touched, and the editor recreates the logs on next launch. The only loss is the " +
			"trace of a language server crash you might have wanted to report upstream.",
		Commands: []string{`rm -rf ~/Library/Logs/Zed/*`},
	},
	"DiagnosticReports": {
		Score: Safe,
		Description: "Where macOS drops a crash report every time a process running as you terminates abnormally. " +
			"Each .ips file is JSON written by ReportCrash at the moment of the fault — process name, exception " +
			"type, the state of every thread and the stack trace that led to it — and is exactly what Console.app " +
			"lists under \"Crash Reports\". The folder is group-owned by _analyticsusers so the analytics daemon " +
			"can read it when crash sharing is on, and a Retired subfolder holds reports macOS has already aged " +
			"out on its own.",
		Effects: "Deletes this account's crash history. Nothing depends on it: a report is only written after a " +
			"process has already died, so removing one can neither cause nor prevent a crash, and macOS prunes " +
			"these itself over time anyway. What you give up is evidence — if an app keeps crashing and you, its " +
			"developer, or Apple support want to read the stack trace, the reports have to be regenerated by " +
			"reproducing the crash.",
		Commands: []string{`rm -rf ~/Library/Logs/DiagnosticReports/*`},
	},
	"CrashReporter": {
		Score: Safe,
		Description: "Two unrelated things under one name. DiagnosticLogs/ holds small heartbeat files written by " +
			"system services (for instance Spotlight's spotlight_heartbeat_last.log), while MobileDevice/ is where " +
			"macOS copies crash logs off an iPhone or iPad each time one is connected, one subfolder per device " +
			"name. On a Mac that regularly syncs a phone the MobileDevice side is the part that grows; otherwise " +
			"the whole folder stays tiny.",
		Effects: "Removes the copied device crash logs and the heartbeat files. Nothing on the Mac or on the phone " +
			"depends on them — they are copies made for diagnosis, and iOS keeps its own originals on the device " +
			"until it rotates them out. Both sides are recreated the next time the relevant service runs or a " +
			"device is plugged in.",
		Commands: []string{`rm -rf ~/Library/Logs/CrashReporter/*`},
	},
	"AppAnalytics": {
		Score: Safe,
		Description: "Staged app-analytics payloads: one JSON file per app session, named " +
			"<bundle id>.<uuid>.json, recording events such as when a user activity began and ended. This is the " +
			"\"Share with App Developers\" side of macOS analytics — the system writes these locally regardless of " +
			"the setting, and the analytics daemon picks them up only when sharing is enabled.",
		Effects: "Removes the queued payloads. No app reads them, none of your apps behave differently afterwards, " +
			"and the analytics settings themselves are untouched; the system simply writes new files as you keep " +
			"using apps. If sharing is on, the only effect is that whatever hadn't been uploaded yet never gets " +
			"uploaded, which for some people is the entire point.",
		Commands: []string{`rm -rf ~/Library/Logs/AppAnalytics/*`},
	},
	"Homebrew": {
		Score: Safe,
		Description: "Where Homebrew writes the build transcript when a formula has to be compiled from source " +
			"instead of installing a prebuilt bottle: one subfolder per formula, holding the configure/make output " +
			"and the exact commands that ran. On a machine that has only ever installed bottles it stays empty, " +
			"and it only grows after a source build, successful or not.",
		Effects: "Removes the build transcripts. Nothing installed is affected — formulas, casks and the Cellar all " +
			"live under the Homebrew prefix, not here, and brew never reads these files back. The only cost is " +
			"that if a source build just failed and you were about to read the log to find out why, you'd have to " +
			"re-run the install to reproduce it.",
		Commands: []string{`rm -rf ~/Library/Logs/Homebrew/*`},
	},
	"com.apple.AMPLibraryAgent": {
		Score: Safe,
		Description: "Log folder for AMPLibraryAgent, the background daemon that maintains the local Music and TV " +
			"library database. In practice all it contains is an \"iTunes Migration\" subfolder: the transcript of " +
			"the one-time migration that converted an old iTunes library into the modern Music library, kept ever " +
			"since it ran.",
		Effects: "Removes a historical migration transcript. The migration itself already completed and its result " +
			"is recorded in the library database, not in this folder, so your music library, playlists and play " +
			"counts are entirely unaffected. Nothing writes here again unless another library migration ever runs.",
		Commands: []string{`rm -rf ~/Library/Logs/com.apple.AMPLibraryAgent/*`},
	},
	"iPhone Updater Logs": {
		Score: Safe,
		Description: "An iTunes-era folder that held the transcript of iOS device restores and updates performed " +
			"from this Mac. Modern macOS moved that work into Finder and files device-side crash logs under " +
			"CrashReporter/MobileDevice instead, so on a current system this is usually an empty leftover that " +
			"nothing has written to in years.",
		Effects: "Removes whatever old restore transcripts remain. Nothing consults them — they are not read during " +
			"a future restore, and iOS keeps its own copy of the restore log on the device itself — so deleting " +
			"them cannot affect syncing, backups or a device update.",
		Commands: []string{`rm -rf ~/Library/Logs/iPhone\ Updater\ Logs/*`},
	},
	"com.macpaw.CleanMyMac-setapp": {
		Score: Safe,
		Description: "Where CleanMyMac (Setapp edition) writes its own diagnostic output, following the macOS " +
			"convention that an app logs into ~/Library/Logs/<bundle id>. It is frequently empty, because the app " +
			"only writes here when something goes wrong during a scan or a cleanup.",
		Effects: "Removes CleanMyMac's own diagnostic output. Its cached scan results, settings and subscription " +
			"state live in Application Support and Caches rather than here, so nothing about the app changes and " +
			"no file it was tracking is touched. Only MacPaw support would ever want these.",
		Commands: []string{`rm -rf ~/Library/Logs/com.macpaw.CleanMyMac-setapp/*`},
	},
	"com.macpaw.CleanMyMac-setapp.Menu": {
		Score: Safe,
		Description: "The same thing as the folder above, but for CleanMyMac's separate menu-bar helper — the " +
			"process that shows the live memory and disk readouts. It runs under its own bundle identifier and so " +
			"logs into its own folder, keeping its output from interleaving with the main app's.",
		Effects: "Removes the helper's diagnostic output only. The menu-bar item keeps running and keeps monitoring; " +
			"nothing it displays is stored in this folder.",
		Commands: []string{`rm -rf ~/Library/Logs/com.macpaw.CleanMyMac-setapp.Menu/*`},
	},
	"TIDAL": {
		Score: Safe,
		Description: "Log directory for the TIDAL desktop music app, created by the convention that an app logs " +
			"into ~/Library/Logs/<app name>. TIDAL writes playback and streaming diagnostics here, and the folder " +
			"is typically empty between problems.",
		Effects: "Removes the playback diagnostics. Your TIDAL sign-in, settings and any offline downloads are " +
			"stored elsewhere and are untouched — offline tracks in particular are not in this folder, so nothing " +
			"you've downloaded for offline listening is at risk.",
		Commands: []string{`rm -rf ~/Library/Logs/TIDAL/*`},
	},
	"ZoomPhone": {
		Score: Safe,
		Description: "Log directory for the Zoom Phone component of the Zoom client — the VoIP calling side, kept " +
			"separate from the meeting client that logs into the zoom.us folder here. In practice it holds little " +
			"more than a ProcessInfo.plist recording the helper's last run.",
		Effects: "Removes the helper's bookkeeping. Your phone number, call history and Zoom sign-in are all " +
			"account-side or in Application Support rather than here, so calling is unaffected, and the file is " +
			"rewritten the next time the component starts.",
		Commands: []string{`rm -rf ~/Library/Logs/ZoomPhone/*`},
	},
}

// logsSlackCleanPaths are the caches inside Slack's Electron profile —
// confirmed by inspecting a real Slack container. Cache/ and Service
// Worker/CacheStorage/ alone account for well over a gigabyte. The list
// deliberately excludes Cookies, auth-cookie-backup, Local Storage,
// Session Storage and IndexedDB, which between them hold the session that
// keeps you signed in to every workspace, so a clean here can never log
// you out. Dawn*Cache covers Slack's three shader caches (DawnCache,
// DawnGraphiteCache, DawnWebGPUCache) in one pattern.
var logsSlackCleanPaths = []string{
	`Data/Library/Application Support/Slack/Cache/*`,
	`Data/Library/Application Support/Slack/Service Worker/CacheStorage/*`,
	`Data/Library/Application Support/Slack/Code Cache/*`,
	`Data/Library/Application Support/Slack/GPUCache/*`,
	`Data/Library/Application Support/Slack/Dawn*Cache/*`,
	`Data/Library/Application Support/Slack/logs/*`,
}

// logsSlackRm is the shared `rm -rf` prefix for the paths above. Spaces are
// backslash-escaped here but not in logsSlackCleanPaths, because these
// strings are displayed as copy-pasteable shell commands while CleanPaths
// are handed to filepath.Glob, which takes them literally.
const logsSlackRm = `rm -rf ~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/` +
	`Application\ Support/Slack`

var containersDB = map[string]Entry{
	// Scored Caution rather than Risky — the usual rating for a container —
	// because the clean is provably scoped to Chromium cache directories and
	// leaves every file that carries the login session in place.
	"com.tinyspeck.slackmacgap": {
		Score: Caution,
		Description: "Slack's sandbox container, and usually the largest single folder under ~/Library/Containers. " +
			"Inside it, Data/Library/Application Support/Slack is an ordinary Electron/Chromium profile, and the " +
			"split is sharp: Cache/ and Service Worker/CacheStorage/ are the HTTP and service-worker caches holding " +
			"message content, avatars, file previews and the app bundle itself (over a gigabyte between them), " +
			"while Cookies, auth-cookie-backup, Local Storage and IndexedDB hold the session token that keeps you " +
			"signed in to every workspace. Deleting the container outright signs you out of all of them.",
		Effects: "Quit Slack first — it rewrites these files continuously while running. This removes only the " +
			"caches and Slack's own log directory: the HTTP cache, the service worker's cached responses, the " +
			"compiled-JavaScript cache and the GPU/Dawn shader caches. Your workspaces, sign-in and unread state " +
			"are kept in Cookies, auth-cookie-backup, Local Storage, Session Storage and IndexedDB, none of which " +
			"this touches, so you stay logged in everywhere. Slack re-downloads its app bundle and re-fetches " +
			"avatars, emoji and previews as you scroll, which makes the first few minutes after a clean " +
			"noticeably slower and needs a working connection.",
		Commands: []string{
			`# Quit Slack first`,
			logsSlackRm + `/Cache/*`,
			logsSlackRm + `/Service\ Worker/CacheStorage/*`,
			logsSlackRm + `/Code\ Cache/*`,
			logsSlackRm + `/GPUCache/*`,
			logsSlackRm + `/Dawn*Cache/*`,
			logsSlackRm + `/logs/*`,
		},
		CleanPaths: logsSlackCleanPaths,
	},
	"com.apple.AMPArtworkAgent": {
		Score: Caution,
		Description: "The sandbox of AMPArtworkAgent, the background daemon that fetches and stores album and " +
			"artist artwork for the Music and TV apps. Data/Documents/artwork is a flat pile of JPEGs named by " +
			"content hash — one per image ever displayed, including artist photos you never added yourself — and " +
			"artworkd.sqlite is the index mapping tracks to those files. All of it is derived data: every image " +
			"came either from Apple's servers or from a file already in your library.",
		Effects: "Quit Music and TV first. Removes the cached artwork and its index together, so the two can't end " +
			"up disagreeing about what is actually on disk. No music, playlists, play counts or purchases are " +
			"stored in this container, and artwork embedded inside your own audio files stays in those files. " +
			"Music re-fetches images as you browse, so album grids fill back in progressively over the next few " +
			"sessions and need a network connection to do it.",
		Commands: []string{
			`# Quit Music and TV first`,
			`rm -rf ~/Library/Containers/com.apple.AMPArtworkAgent/Data/Documents/artwork/*`,
			`rm -rf ~/Library/Containers/com.apple.AMPArtworkAgent/Data/Documents/artworkd.sqlite*`,
		},
		CleanPaths: []string{`Data/Documents/artwork/*`, `Data/Documents/artworkd.sqlite*`},
	},
	"com.apple.mediaanalysisd": {
		Score: Caution,
		Description: "The sandbox of mediaanalysisd, the daemon that runs Apple's on-device vision models over your " +
			"Photos library to power search by object and scene, Memories, and Live Text. Its " +
			"Data/Library/Caches holds intermediate analysis state alongside the scene-taxonomy and geography " +
			"index files it works against. This is a well-known bloat spot: when Photos keeps re-presenting the " +
			"same assets for analysis, the cache grows and is never reclaimed on its own.",
		Effects: "Quit Photos first. Removes the intermediate analysis caches only — the Photos library itself and " +
			"everything already derived into it (the people you've named, search terms, Memories) live in " +
			"Photos Library.photoslibrary and are not touched. The daemon rebuilds what it needs, so expect it to " +
			"use CPU in the background for a while afterwards, and be aware this can come back if a re-analysis " +
			"loop is what filled the folder in the first place.",
		Commands: []string{
			`# Quit Photos first`,
			`rm -rf ~/Library/Containers/com.apple.mediaanalysisd/Data/Library/Caches/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/*`},
	},
	"com.apple.photoanalysisd": {
		Score: Caution,
		Description: "The sandbox of photoanalysisd, mediaanalysisd's counterpart: it schedules and drives the " +
			"analysis passes over the Photos library — face clustering into People, Memories generation — while " +
			"the Mac is idle and on power. Its Caches folder holds a small Cache.db plus copies of the " +
			"scene-taxonomy index, and it is normally a fraction of the size of mediaanalysisd's.",
		Effects: "Quit Photos first. Removes scheduling and lookup caches only. Recognised faces, the names you gave " +
			"them, Memories and album data are all stored inside the Photos library rather than here and survive " +
			"intact. The daemon re-derives its working state during the next idle analysis pass; the visible cost " +
			"is some extra background CPU while it does.",
		Commands: []string{
			`# Quit Photos first`,
			`rm -rf ~/Library/Containers/com.apple.photoanalysisd/Data/Library/Caches/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/*`},
	},
	"com.apple.geod": {
		Score: Caution,
		Description: "The sandbox of geod, the geo services daemon behind Maps and anything else that resolves " +
			"addresses or draws map tiles. Data/Library/Caches/com.apple.geod holds its map-tile and \"Vault\" " +
			"caches, but the bulk of this container is usually Data/tmp: thousands of small per-request scratch " +
			"files the daemon writes and then never cleans up, which is how it reaches hundreds of megabytes on a " +
			"Mac where Maps is barely opened.",
		Effects: "Quit Maps first. Removes the tile cache and the abandoned temp files. No saved locations, Guides, " +
			"recents or Home/Work addresses are stored here — those sync through your Apple Account — so nothing " +
			"you saved in Maps is lost. Maps re-downloads tiles for the areas you look at next, and geod recreates " +
			"its temp directory the moment it needs one.",
		Commands: []string{
			`# Quit Maps first`,
			`rm -rf ~/Library/Containers/com.apple.geod/Data/Library/Caches/com.apple.geod/*`,
			`rm -rf ~/Library/Containers/com.apple.geod/Data/tmp/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/com.apple.geod/*`, `Data/tmp/*`},
	},
	"com.apple.wallpaper.agent": {
		Score: Caution,
		Description: "The sandbox of the wallpaper agent, the process that renders your desktop picture and drives " +
			"the animated and aerial wallpapers. Everything in it sits under " +
			"Data/Library/Caches/com.apple.wallpaper.caches, grouped by which wallpaper extension produced it, and " +
			"holds rendered stills and thumbnails rather than any source asset. It has a long-standing habit of " +
			"never evicting anything, so on a Mac whose wallpaper changes often it accumulates renders of pictures " +
			"that are no longer on the system at all.",
		Effects: "Removes the rendered wallpaper thumbnails and previews. Your current desktop picture keeps " +
			"working, because the agent re-renders it from the original — the actual wallpaper files live in " +
			"/System/Library/Desktop Pictures and ~/Library/Application Support/com.apple.wallpaper, not here, and " +
			"the wallpaper you picked is a preference rather than a cached image. The Wallpaper and Screen Saver " +
			"panes in System Settings redraw their thumbnail grids more slowly the first time you open them " +
			"afterwards.",
		Commands:   []string{`rm -rf ~/Library/Containers/com.apple.wallpaper.agent/Data/Library/Caches/*`},
		CleanPaths: []string{`Data/Library/Caches/*`},
	},
	"com.apple.NeptuneOneExtension": {
		Score: Caution,
		Description: "The sandbox of NeptuneOneWallpaper, a system extension shipped at " +
			"/System/Library/ExtensionKit/Extensions/NeptuneOneWallpaper.appex that provides macOS Tahoe's " +
			"animated desktop pictures. Data/Library/Application Support/Videos holds the downloaded .mov files " +
			"themselves — one per variant, e.g. \"Tahoe Light Landscape.mov\" and \"Tahoe Dark Portrait.mov\", " +
			"roughly 30-45MB each. \"Neptune\" is an Apple internal codename, which is why a 150MB container shows " +
			"up here under a name that matches no app you have ever installed.",
		Effects: "Removes the downloaded wallpaper videos, which is all this container holds. No setting lives here " +
			"— your wallpaper choice is a preference stored elsewhere — so the desktop keeps showing that " +
			"wallpaper as a still image; only the animation stops, until macOS downloads the videos again, which " +
			"is a ~150MB transfer. The vendor-supported way to reclaim the same space is System Settings > " +
			"Wallpaper, right-click the wallpaper and choose Remove Download. Worth doing only if you don't " +
			"actually use the animated variants.",
		Commands: []string{
			`rm -rf ~/Library/Containers/com.apple.NeptuneOneExtension/` +
				`Data/Library/Application\ Support/Videos/*`,
		},
		CleanPaths: []string{`Data/Library/Application Support/Videos/*`},
	},
	"ru.keepcoder.Telegram": {
		Score: Caution,
		Description: "Telegram Desktop's sandbox container — which is not where Telegram keeps its data. The " +
			"account, message database and downloaded media live in ~/Library/Group " +
			"Containers/6N38VWS5BX.ru.keepcoder.Telegram (frequently several gigabytes), shared with the app's " +
			"share and notification extensions. What remains here is Data/Library/WebKit, the storage backing " +
			"Telegram's built-in web views (Instant View, web logins, mini apps), plus a small URL cache and " +
			"per-request scratch files in Data/tmp.",
		Effects: "Quit Telegram first. Removes the small URL cache and the leftover temp files only. Your account, " +
			"chats and downloaded media are in the group container and are not touched, so you stay signed in " +
			"with everything intact. The 100MB+ WebKit store is deliberately left alone: it holds the cookies and " +
			"local storage for mini apps and in-app web logins, and clearing it would sign you out of those one " +
			"by one.",
		Commands: []string{
			`# Quit Telegram first`,
			`rm -rf ~/Library/Containers/ru.keepcoder.Telegram/Data/Library/Caches/*`,
			`rm -rf ~/Library/Containers/ru.keepcoder.Telegram/Data/tmp/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/*`, `Data/tmp/*`},
	},
	// The entries below deliberately carry no Commands. They are small,
	// nothing in them is meaningfully reclaimable, and each one sits on a
	// path — credentials, the photo library, purchases, user documents —
	// where being wrong is far more expensive than the megabytes involved.
	"com.apple.Passwords": {
		Score: Risky,
		Description: "The sandbox of the Passwords app introduced in macOS Sequoia. The passwords themselves are " +
			"never in here: they live in the iCloud Keychain behind the system keychain services. What this " +
			"container holds is the app's UI state, its Preferences, and a WebKit/HTTPStorages cache backing the " +
			"sign-in views and website icons — a couple of megabytes in total.",
		Effects: "No clean action is offered. There is nothing here worth reclaiming, and this is the wrong folder " +
			"to be wrong about: deleting a credential-adjacent app's container to save two megabytes is a bad " +
			"trade even though the credentials are stored elsewhere. If the app is misbehaving, the supported fix " +
			"is signing out of and back into iCloud, not deleting this.",
	},
	"com.apple.Passwords.MenuBarExtra": {
		Score: Risky,
		Description: "The separate sandbox for the Passwords menu-bar item — the quick-access popover — which runs " +
			"as its own extension and therefore gets its own container. Like the main app it stores no passwords; " +
			"the few megabytes here are HTTPStorages and WebKit cache backing the icons and views it displays.",
		Effects: "No clean action is offered, for the same reason as the main Passwords container: a couple of " +
			"megabytes is not worth touching anything on the credential path, and nothing in here regrows enough " +
			"to be worth revisiting.",
	},
	"com.apple.photolibraryd": {
		Score: Risky,
		Description: "The sandbox of photolibraryd, the daemon that owns the Photos library database — every read " +
			"and write Photos performs goes through it. Its container holds the daemon's own working caches and " +
			"preferences, not any photos: the library itself is a .photoslibrary bundle in your Pictures folder.",
		Effects: "No clean action is offered. The folder is only a few megabytes, and this is the process that " +
			"mediates access to your entire photo library — clearing state out from under a running library " +
			"daemon is real risk for no payoff at this size. If Photos is misbehaving, hold Option while opening " +
			"it and use the built-in repair tool instead.",
	},
	"com.apple.AppStore": {
		Score: Risky,
		Description: "The App Store app's sandbox. It holds the WebKit cache for the storefront pages, a " +
			"com.apple.AppleMediaServices folder with account and purchase-flow state, and Preferences. Apps you " +
			"have bought are installed into /Applications and their receipts live inside those app bundles, so " +
			"nothing you own is stored in this container.",
		Effects: "No clean action is offered. At well under two megabytes there is nothing to reclaim, and the " +
			"AppleMediaServices state is tied to your Apple Account sign-in for purchases — clearing it can force " +
			"a re-authentication prompt for no benefit at all. If the storefront renders wrong, quitting and " +
			"reopening the App Store is the fix.",
	},
	"com.apple.Pages": {
		Score: Risky,
		Description: "Pages' sandbox container, and the clearest illustration of why this root is dangerous. A " +
			"sandboxed app's Data folder is its home directory: Data/Documents is that app's Documents folder, and " +
			"for a document-based app it can hold real, unrecoverable work. Here the bulk sits under " +
			"Data/Library/Application Support, which is where Pages keeps autosave state and custom templates.",
		Effects: "No clean action is offered. There is nothing cache-shaped to reclaim, and the downside is losing " +
			"autosaved documents or templates you made yourself. If you want space back from Pages, delete " +
			"documents you no longer want from inside the app, where they go through the Trash and can still be " +
			"recovered.",
	},
}
