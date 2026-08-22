package knowledge

import "strings"

// This file is the IDE/editor half of the dictionary, split across the two
// roots those tools actually write to. ideAppSupportDB covers folders under
// ~/Library/Application Support and ideCachesDB covers folders under
// ~/Library/Caches; both are merged into the main per-root tables.
//
// The split matters more here than almost anywhere else, because an editor's
// two folders are opposites. Under Caches an editor keeps downloaded language
// servers, compiled shader caches and staged installers — all of it
// re-downloadable. Under Application Support it keeps settings, keymaps,
// snippets, extension state, and in JetBrains' case the license file. So the
// Caches entries below hand out whole-folder deletes freely, while most of
// the Application Support entries are description-only or carve out a couple
// of narrow subfolders with CleanPaths.
//
// A note on how the JetBrains keys work. Lookups are keyed by (Root, folder
// name), and a folder's children inherit the root they were found under, so
// drilling into ~/Library/Application Support/JetBrains produces rows looked
// up by their own bare name. That is why "Toolbox", "Daemon", "discovery" and
// friends appear as top-level keys here even though they only ever exist one
// level down inside JetBrains.

// ideVSCodeCleanPaths is the regenerable half of a VS Code-style profile
// folder, shared by every Code-OSS fork below because they all inherit the
// exact same layout. Confirmed against the Code/ and Cursor/ folders already
// described in appsupport.go.
//
// Deliberately excluded: User/ (settings.json, keybindings.json, snippets,
// workspaceStorage, globalStorage and the editor's own file History),
// Backups/ (unsaved editor contents), and Local Storage/, Cookies/ and
// IndexedDB/, which is where these apps park the auth token — wiping those
// signs the user out of the editor and of every extension account.
var ideVSCodeCleanPaths = []string{
	"CachedExtensionVSIXs/*",
	"CachedData/*",
	"CachedProfilesData/*",
	"Cache/*",
	"Code Cache/*",
	"GPUCache/*",
	"DawnWebGPUCache/*",
	"DawnGraphiteCache/*",
	"logs/*",
}

// ideShellEscape backslash-escapes spaces in a path fragment. Commands are
// shown to the user as literal, copy-pasteable shell, so "Code Cache" has to
// come out as Code\ Cache the same way the hand-written entries elsewhere in
// the dictionary write it.
func ideShellEscape(p string) string {
	return strings.ReplaceAll(p, " ", `\ `)
}

// ideVSCodeCommands renders the clean commands for one VS Code-derived
// editor's Application Support folder: a leading "quit first" comment, then
// one rm per entry in ideVSCodeCleanPaths, in the same order, so the
// Commands/CleanPaths pairing the dictionary requires stays in lockstep no
// matter how many forks get added here.
func ideVSCodeCommands(app, folder string) []string {
	cmds := make([]string, 0, len(ideVSCodeCleanPaths)+1)
	cmds = append(cmds, `# Quit `+app+` first`)
	for _, p := range ideVSCodeCleanPaths {
		cmds = append(cmds, `rm -rf ~/Library/Application\ Support/`+folder+`/`+ideShellEscape(p))
	}
	return cmds
}

// ideJetBrainsProduct builds the entry for one unversioned, product-level
// JetBrains folder — the "Goland" and "Phpstorm" that sit beside the versioned
// GoLand2026.1 and PhpStorm2026.2 and confuse everyone who notices them.
//
// The odd spelling is not a typo on JetBrains' part. The IntelliJ Platform
// builds this path from ApplicationNamesInfo.getLowercaseProductName(), which
// despite its name returns a sentence-cased product name ("Idea" for IntelliJ
// IDEA, "Webstorm" for WebStorm), and joins it onto PathManager's common data
// directory — the vendor-level folder shared by every version of every IDE.
// Verified against JetBrains/intellij-community and against the two folders
// present on this machine, both of which contain nothing but
// consentOptions/cached and measure 4KB.
//
// All of these are description-only: they are kilobytes, and the folder is
// reserved for cross-version shared state, so a blanket delete here would be
// betting that no future IDE version puts anything else in it.
func ideJetBrainsProduct(name, product, note string) Entry {
	return Entry{
		Score: Caution,
		Description: "Cross-version shared state for " + product + ", written by every installed copy of it rather " +
			"than by one particular install — which is why it carries no version number and sits beside the " +
			"versioned " + product + "<year>.<n> folders instead of inside one. The odd spelling is JetBrains' " +
			"own: the IntelliJ Platform sentence-cases the product name for this path, so " + product + " becomes " +
			"\"" + name + "\". In practice the entire content is consentOptions/cached, a downloaded copy of the " +
			"data-sharing and AI consent texts the IDE shows on first launch, which makes the folder a few " +
			"kilobytes at most. " + note,
		Effects: "No clean action is offered, and there is nothing here worth reclaiming — this folder is measured " +
			"in kilobytes, not megabytes. Which consents you actually accepted is recorded separately in the " +
			"shared consentOptions folder next to this one, and your settings, keymaps, plugins and license live " +
			"in the versioned " + product + "<year>.<n> folder, which this app never offers to delete either. If " +
			"you want the space back from a JetBrains install, the Cache tab is where the gigabytes are.",
	}
}

var ideAppSupportDB = map[string]Entry{

	// --- Shared JetBrains folders, all found inside Application Support/JetBrains ---

	"Toolbox": {
		Score: Risky,
		Description: "The JetBrains Toolbox App's own state, and the reason your IDEs can update themselves. " +
			"accounts.json holds the JetBrains account Toolbox is signed in to, .settings.json and state.json " +
			"its configuration and window state, images.json the catalog of installed builds, and channels/ a " +
			"large downloaded release-notes catalog per product (a couple of hundred KB each). scripts/ is the " +
			"important one: it contains the `goland`, `phpstorm`, `webstorm` and `rustrover` shell shims that " +
			"Toolbox puts on your PATH so you can launch an IDE from a terminal.",
		Effects: "No clean action is offered. Deleting this signs Toolbox out of your JetBrains account, so every " +
			"IDE that gets its license through Toolbox has to be re-authorised, and it removes the shell " +
			"launchers, breaking `goland .` and friends until you re-enable them in Toolbox's settings. The " +
			"folder is well under a megabyte on this machine, so there is nothing to gain by risking that. " +
			"Toolbox's genuinely large cache — staged IDE installers — is under ~/Library/Caches/JetBrains/" +
			"Toolbox and is cleanable from the Cache tab.",
	},
	"Daemon": {
		Score: Caution,
		Description: "The JetBrains Toolbox Daemon (bundle id com.jetbrains.app.daemon), a background helper " +
			"Toolbox runs to install and update IDEs, manage remote-development sessions and answer the CLI " +
			"shims. Almost all of the size is bundles/: current/jetbrainsd.app is the daemon actually running " +
			"(80MB here) and current.backup/jetbrainsd.app is the previous version kept so a bad update can be " +
			"rolled back (another 86MB), with bundles/extract/ used as staging while a new one is unpacked. " +
			"data/ next to them is tiny — a pid file, a socket path, an askpass helper and a small rhizome " +
			"database recording which IDEs and workspaces the daemon knows about.",
		Effects: "Quit JetBrains Toolbox first so the daemon is not mid-update. Only the rollback copy and the " +
			"unpack staging directory are removed, about 86MB here; the daemon binary in bundles/current stays " +
			"exactly where it is, so Toolbox keeps working normally and nothing has to be re-downloaded on next " +
			"launch. The one thing you give up is automatic rollback: if a future daemon update misbehaves, " +
			"Toolbox has to re-download a working version instead of reverting to the copy it had kept.",
		Commands: []string{
			`# Quit JetBrains Toolbox first`,
			`rm -rf ~/Library/Application\ Support/JetBrains/Daemon/bundles/current.backup/*`,
			`rm -rf ~/Library/Application\ Support/JetBrains/Daemon/bundles/extract/*`,
		},
		CleanPaths: []string{
			"bundles/current.backup/*",
			"bundles/extract/*",
		},
	},
	"PrivacyPolicy": {
		Score: Safe,
		Description: "A downloaded copy of the legal texts JetBrains IDEs have to show you — on this machine a " +
			"single aiEua.cached holding the AI end-user agreement, around 50KB. The IDE fetches these so it can " +
			"display the current wording offline and notice when a new version needs re-accepting. The record of " +
			"what you actually accepted is not here; it is in the consentOptions folder beside it and on your " +
			"JetBrains account.",
		Effects: "Removes the cached policy text only. Nothing you agreed to is revoked and no setting changes — " +
			"the IDE simply re-downloads the current wording the next time it needs it, and at worst shows you " +
			"the agreement dialog once more. The folder is around 50KB, so treat this as tidying rather than as " +
			"reclaiming space.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/JetBrains/PrivacyPolicy/*`},
	},
	"consentOptions": {
		Score: Risky,
		Description: "The machine-wide record of which JetBrains data-sharing consents you accepted, shared by " +
			"every installed JetBrains IDE rather than kept per version. The `accepted` file is a single line of " +
			"consentId:version:state:timestamp triples covering things like anonymous usage statistics, " +
			"automatic exception reporting and detailed AI data collection, and consent-log-data/ holds the log " +
			"of when those choices changed.",
		Effects: "No clean action is offered. This is a record of decisions you made, not a cache: deleting it " +
			"makes every JetBrains IDE on the machine ask again on next launch, and anything you deliberately " +
			"turned off — usage statistics, AI prompt collection — reverts to whatever the default is until you " +
			"turn it off a second time. It is a couple of kilobytes. Change these from Settings → Appearance & " +
			"Behavior → System Settings → Data Sharing instead, where you can see what each one does.",
	},
	"discovery": {
		Score: Caution,
		Description: "One small JSON file per running JetBrains IDE, named <pid>-ide-instance.json and written " +
			"the moment the IDE starts so other tools can find it. Each file advertises the address of that " +
			"IDE's built-in web server (127.0.0.1:63342 by default), its pid, product code, build number and " +
			"runtime, the full paths to its config/system/log/plugin directories, and the list of projects it " +
			"currently has open. JetBrains Toolbox and the JetBrains browser extension read these to route " +
			"\"open in IDE\" requests to a live instance. The folder is mode 0700 for exactly that reason.",
		Effects: "Quit every running JetBrains IDE first, then this is free: an IDE rewrites its own file on the " +
			"next launch and nothing here is state you would miss. Doing it while an IDE is running instead " +
			"breaks Toolbox's and the browser extension's ability to hand files to that already-open instance " +
			"until you restart it. The folder is a few kilobytes, so the real reason to clear it is that stale " +
			"files left behind by crashed IDEs record which projects you had open.",
		Commands: []string{
			`# Quit every running JetBrains IDE first`,
			`rm -rf ~/Library/Application\ Support/JetBrains/discovery/*`,
		},
	},
	"acp-agents": {
		Score: Risky,
		Description: "The machine-wide registry of AI coding agents installed into your JetBrains IDEs over the " +
			"Agent Client Protocol. installed.json names each one (GitHub Copilot, Anthropic's Claude Agent, " +
			"JetBrains' own Junie, OpenAI's Codex and so on) with its pinned version, npx/uvx launch command, " +
			"install and last-used timestamps, auto-update policy, the terms-of-service version you accepted for " +
			"it, and a disabledAgents list of the ones you switched off. The agents' actual downloaded runtimes " +
			"are not here — those are per-IDE-version under ~/Library/Caches/JetBrains/<IDE>/acp-agents and can " +
			"run to a gigabyte.",
		Effects: "No clean action is offered. The file is a few kilobytes and it is entirely your configuration: " +
			"deleting it un-installs every agent from the IDE's point of view, drops the agents you had " +
			"deliberately disabled back to their defaults, and makes you re-accept each agent's terms of service " +
			"before it will run again. Clean the per-IDE-version cache folders instead — that is where the " +
			"downloaded Node runtimes and npm caches actually live.",
	},
	"bl": {
		Score: Risky,
		Description: "Not a folder but a file: JetBrains' downloaded license blocklist. Its contents are " +
			"base64, and decoding it yields a signed envelope of the form <!-- SHA1withRSA-<signature>-" +
			"<certificate> --> issued by \"JetProfile CA\", the certificate authority JetBrains signs license " +
			"tickets with, prefixed by a four-byte revision counter. The IDE fetches it from JetBrains' " +
			"licensing servers and checks your license ticket against it, which is how a license reported stolen " +
			"or refunded stops working on the machines it was used on.",
		Effects: "No clean action is offered. The file is about 2.5KB, so there is nothing to reclaim, and it is " +
			"part of how the IDE decides whether your license is still valid. Removing or editing licence-" +
			"validation state is a good way to end up with an IDE that deactivates itself and demands you sign " +
			"in again, and on a legitimately licensed machine there is no upside at all. If an IDE is wrongly " +
			"refusing a license you own, that is a JetBrains support question, not a disk-cleanup one.",
	},
	"crl": {
		Score: Risky,
		Description: "Not a folder but a file: the certificate revocation list for \"JetProfile CA\", JetBrains' " +
			"licensing certificate authority. Same shape as the bl file beside it — base64 wrapping a signed " +
			"<!-- SHA1withRSA-... --> envelope — except that this one carries a literal " +
			"-----BEGIN X509 CRL----- block naming the license-signing certificates JetBrains has revoked. The " +
			"IDE downloads it so it can reject a license whose signing certificate is no longer trusted.",
		Effects: "No clean action is offered. Under 4KB, and the same reasoning as bl applies: this is license-" +
			"validation material, not cache. Deleting it frees nothing measurable and risks putting the IDE into " +
			"a state where it re-validates from scratch and asks you to sign in again. Leave it alone.",
	},

	// --- Unversioned, product-level JetBrains folders ---
	//
	// Every one of these is the "Goland"/"Phpstorm" case: a sentence-cased
	// product name sitting beside the versioned folders. See
	// ideJetBrainsProduct for why they are all description-only.

	"Idea": ideJetBrainsProduct("Idea", "IntelliJ IDEA",
		"IntelliJ IDEA Community and Ultimate share this one folder, because both report their product name "+
			"as IDEA — their versioned folders do differ, IdeaIC<year>.<n> for Community against "+
			"IntelliJIdea<year>.<n> for Ultimate. Android Studio, Google's IntelliJ IDEA-based Android IDE, "+
			"does not appear under JetBrains at all: it ships with Google as its vendor, so its equivalent "+
			"folders are ~/Library/Application Support/Google/AndroidStudio<year>.<n> and "+
			"~/Library/Caches/Google/AndroidStudio<year>.<n>."),
	"Pycharm": ideJetBrainsProduct("Pycharm", "PyCharm",
		"Since 2025.1 the Community and Professional editions have been merged into a single unified PyCharm "+
			"with a paid tier, and 2025.2 was the last standalone Community release, so on an up-to-date "+
			"machine the only versioned folder beside this one is PyCharm<year>.<n>; older machines may still "+
			"have a PyCharmCE<year>.<n> from the Community era."),
	"Webstorm": ideJetBrainsProduct("Webstorm", "WebStorm",
		"WebStorm is JetBrains' JavaScript and TypeScript IDE, and has been free for non-commercial use since "+
			"late 2024."),
	"Phpstorm": ideJetBrainsProduct("Phpstorm", "PhpStorm",
		"PhpStorm is the PHP IDE; it is a superset of WebStorm, so having both installed is common and each "+
			"gets its own folder here."),
	"Goland": ideJetBrainsProduct("Goland", "GoLand",
		"GoLand is the Go IDE. Note that its heavy state is entirely elsewhere: project indexes and Local "+
			"History under ~/Library/Caches/JetBrains/GoLand<year>.<n>, settings and plugins under the "+
			"same-named folder in this directory."),
	"Clion": ideJetBrainsProduct("Clion", "CLion",
		"CLion is the C and C++ IDE, free for non-commercial use since 2025. It absorbed the embedded-"+
			"development features of the separate CLion Nova preview."),
	"Rider": ideJetBrainsProduct("Rider", "Rider",
		"Rider is the .NET and Unity IDE, free for non-commercial use since late 2024."),
	"Rubymine": ideJetBrainsProduct("Rubymine", "RubyMine",
		"RubyMine is the Ruby and Rails IDE."),
	"Datagrip": ideJetBrainsProduct("Datagrip", "DataGrip",
		"DataGrip is the standalone database and SQL IDE. Its downloaded JDBC drivers are not here — they sit "+
			"in jdbc-drivers/ inside the versioned DataGrip<year>.<n> folder."),
	"Dataspell": ideJetBrainsProduct("Dataspell", "DataSpell",
		"DataSpell is the data-science IDE built around Jupyter notebooks."),
	"Rustrover": ideJetBrainsProduct("Rustrover", "RustRover",
		"RustRover is the Rust IDE, released in 2024 and free for non-commercial use. Its Cargo registry cache "+
			"is not here — that is ~/.cargo, in your home directory."),
	"Appcode": ideJetBrainsProduct("Appcode", "AppCode",
		"AppCode was the Swift and Objective-C IDE and is discontinued: JetBrains stopped selling it on "+
			"14 December 2022 with the 2022.3 release, and support ended at the end of 2023. If this folder "+
			"exists on a current machine it is a leftover from an install you no longer have."),
	"Aqua": ideJetBrainsProduct("Aqua", "Aqua",
		"Aqua was the IDE for test automation — Selenium, Playwright, Cypress, Appium — and is discontinued "+
			"and no longer sold. Its presence here means an old install, not a current one."),
	"Writerside": ideJetBrainsProduct("Writerside", "Writerside",
		"Writerside was JetBrains' technical-writing IDE for docs-as-code, and has been discontinued. Note it "+
			"also shipped as a plugin for the other IDEs, in which case the plugin lives in the versioned "+
			"folder of whichever IDE you installed it into rather than here."),
	"Mps": ideJetBrainsProduct("Mps", "MPS",
		"MPS is JetBrains' Meta Programming System, a language workbench for building domain-specific "+
			"languages with projectional editors. It is free and open source, and much rarer on a developer "+
			"machine than the mainstream IDEs."),

	// --- Standalone JetBrains products ---

	"Fleet": {
		Score: Caution,
		Description: "The configuration directory for JetBrains Fleet, the lightweight polyglot editor JetBrains " +
			"positioned as a next-generation alternative to IntelliJ. It holds Fleet's settings and its " +
			"plugins/ subfolder. Fleet was discontinued on 22 December 2025 — JetBrains said outright that it " +
			"could neither replace IntelliJ IDEA with Fleet nor narrow it into a clear niche — and its " +
			"server-dependent features have been winding down since, with the agentic Air taking its place.",
		Effects: "No clean action is offered, even though the product is dead. This folder holds Fleet's " +
			"settings and installed plugins rather than cache, and the rule this dictionary applies to every " +
			"JetBrains IDE — that discarding a configuration directory is your decision, not ours — does not " +
			"stop applying because the product was cancelled. Fleet's cache is a separate folder under " +
			"~/Library/Caches/JetBrains/Fleet, and that one is offered for deletion outright.",
	},
	"Air": {
		Score: Caution,
		Description: "State for JetBrains Air, the \"agentic development environment\" JetBrains announced in " +
			"December 2025 as Fleet's successor and released in preview for macOS in March 2026. Air is built " +
			"around delegating work to coding agents — Claude first, then Codex, Gemini, Junie and other ACP-" +
			"compatible agents — and reviewing their diffs, rather than around typing code yourself. Per-" +
			"project agent configuration is not here: that lives in a .air folder inside each project, " +
			"alongside whatever AGENTS.md, CLAUDE.md or .claude the project already had.",
		Effects: "No clean action is offered. Air is a fast-moving preview whose on-disk layout has not settled, " +
			"and this folder is where its account state and machine-level settings go, so a blanket delete here " +
			"could sign you out of the agents you have connected. If Air itself is misbehaving, its own reset " +
			"options are a better tool than a folder delete.",
	},

	// --- VS Code and its forks ---
	//
	// Code, Cursor and Kiro are already covered in appsupport.go. The four
	// below are the same Electron layout with a different product name, so
	// they share ideVSCodeCleanPaths.

	"Windsurf": {
		Score: Caution,
		Description: "Profile folder for Windsurf, the VS Code fork originally shipped by Codeium and since " +
			"folded into Cognition's Devin line. Same layout as VS Code: User/ holds settings, keybindings and " +
			"per-workspace state, Backups/ unsaved editors, and CachedData/, CachedExtensionVSIXs/, Cache/, " +
			"Code Cache/, logs/ and the GPU caches are the disposable half — this folder commonly reaches half " +
			"a gigabyte. Two much larger siblings live outside it in your home directory: ~/.windsurf holds " +
			"installed extensions and ~/.codeium the AI engine, and neither is touched from here.",
		Effects: "Quit Windsurf first. Removes only the V8 compile caches, leftover extension installers, " +
			"Chromium caches and logs. Settings, keybindings, workspace history, unsaved-editor backups and " +
			"your Windsurf/Codeium sign-in all survive, as do the extensions in ~/.windsurf. The first launch " +
			"afterward is a little slower while CachedData rebuilds.",
		Commands:   ideVSCodeCommands("Windsurf", "Windsurf"),
		CleanPaths: ideVSCodeCleanPaths,
	},
	"Trae": {
		Score: Caution,
		Description: "Profile folder for Trae, ByteDance's VS Code fork — the app launches itself with " +
			"--user-data-dir pointed straight at this directory, so it is a stock VS Code profile: User/ for " +
			"settings, keybindings and workspace state, Backups/ for unsaved editors, CachedData/, Cache/, " +
			"Code Cache/, logs/ and the GPU caches for the regenerable half. Trae's separate Solo product uses " +
			"its own \"TRAE SOLO\" folder rather than this one.",
		Effects: "Quit Trae first. Only the compile caches, extension installers, Chromium caches and logs are " +
			"removed; settings, keybindings, workspace state and your signed-in session are untouched. Worth " +
			"knowing separately: Trae has been reported to send substantial telemetry to ByteDance servers even " +
			"with telemetry settings disabled, and clearing caches does nothing about that either way.",
		Commands:   ideVSCodeCommands("Trae", "Trae"),
		CleanPaths: ideVSCodeCleanPaths,
	},
	"Positron": {
		Score: Caution,
		Description: "Profile folder for Positron, Posit's data-science IDE built on Code OSS — the company " +
			"behind RStudio and Quarto. Standard VS Code layout, so User/ holds your settings, keybindings and " +
			"per-workspace state while CachedData/, Cache/, Code Cache/, logs/ and the GPU caches are " +
			"disposable. Positron also keeps a ~/.positron folder in your home directory, outside this one.",
		Effects: "Quit Positron first. Removes the compile and Chromium caches and the logs only. Your settings, " +
			"keybindings, workspace state and the R/Python interpreters you registered are all left alone, and " +
			"nothing about your R or Python environments is touched — those live in the projects and package " +
			"libraries themselves, not here. Expect one slower launch while CachedData rebuilds.",
		Commands:   ideVSCodeCommands("Positron", "Positron"),
		CleanPaths: ideVSCodeCleanPaths,
	},
	"VSCodium": {
		Score: Caution,
		Description: "Profile folder for VSCodium, the community build of VS Code's source without Microsoft's " +
			"branding, telemetry or proprietary marketplace. Identical layout to VS Code's Code/ folder — " +
			"User/ for settings, keybindings, snippets and workspace state, and CachedData/, " +
			"CachedExtensionVSIXs/, Cache/, Code Cache/, logs/ and the GPU caches for the throwaway half. Its " +
			"extensions live in ~/.vscode-oss/extensions rather than ~/.vscode, so a VS Code install beside it " +
			"shares nothing with it.",
		Effects: "Quit VSCodium first. Only the compile caches, stale extension installers, Chromium caches and " +
			"logs go. Settings, keybindings, snippets, workspace state and installed extensions all survive, " +
			"and the only visible cost is a slower first launch while CachedData rebuilds.",
		Commands:   ideVSCodeCommands("VSCodium", "VSCodium"),
		CleanPaths: ideVSCodeCleanPaths,
	},

	// --- Other editors ---

	"Zed": {
		Score: Caution,
		Description: "Zed's data directory, which despite the name is mostly downloaded binaries rather than " +
			"settings — Zed keeps settings.json, keymap.json, snippets and themes in ~/.config/zed instead, " +
			"outside this folder entirely. What is here: languages/ (language servers Zed fetches per " +
			"language), debug_adapters/ (downloaded DAP implementations), prettier/ (the bundled default " +
			"formatter), extensions/ and remote_extensions/, copilot/, remote_servers/ and devcontainer/ for " +
			"remote work, hang_traces/ for diagnostics, and two you would miss: db/ (workspace history and " +
			"restored session state) and prompts/ plus embeddings/ (your saved assistant prompts and the " +
			"semantic-search index).",
		Effects: "Quit Zed first. Only the downloaded language servers, debug adapters, the bundled Prettier and " +
			"the hang-trace diagnostics are removed. Your settings and keymap were never in this folder, and " +
			"db/, extensions/, prompts/ and embeddings/ are deliberately left alone, so recent projects, " +
			"installed extensions, saved prompts and the semantic index all survive. The cost is that the next " +
			"time you open a project in a given language, Zed re-downloads that language's server and debug " +
			"adapter — network access required, and autocomplete/diagnostics are unavailable for that language " +
			"until the download lands.",
		Commands: []string{
			`# Quit Zed first`,
			`rm -rf ~/Library/Application\ Support/Zed/languages/*`,
			`rm -rf ~/Library/Application\ Support/Zed/debug_adapters/*`,
			`rm -rf ~/Library/Application\ Support/Zed/prettier/*`,
			`rm -rf ~/Library/Application\ Support/Zed/hang_traces/*`,
		},
		CleanPaths: []string{
			"languages/*",
			"debug_adapters/*",
			"prettier/*",
			"hang_traces/*",
		},
	},
	"Sublime Text": {
		Score: Caution,
		Description: "Sublime Text 4's data directory, and unusually for a macOS app it mixes cache straight " +
			"into it. Packages/ holds every package you installed plus your own User/ subfolder with " +
			"Preferences.sublime-settings, key bindings and snippets; Installed Packages/ holds the " +
			".sublime-package archives those came from; Local/ holds session state including the contents of " +
			"unsaved buffers. Cache/ and Index/ are the disposable pair — parsed package data and the " +
			"goto-definition symbol index Sublime builds across your open folders.",
		Effects: "Quit Sublime Text first, since it writes session state on exit. Only Cache/ and Index/ are " +
			"removed. Your packages, settings, key bindings, snippets and — importantly — the unsaved buffers " +
			"in Local/ are all left exactly as they were. Sublime rebuilds both on next launch: the first " +
			"start is slower and Goto Definition is incomplete for a minute or so while it re-indexes your " +
			"open folders.",
		Commands: []string{
			`# Quit Sublime Text first`,
			`rm -rf ~/Library/Application\ Support/Sublime\ Text/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Sublime\ Text/Index/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Index/*",
		},
	},
	"Sublime Text 3": {
		Score: Caution,
		Description: "Sublime Text 3's data directory, kept separate from Sublime Text 4's by the trailing 3. " +
			"Same layout as its successor: Packages/ with your installed packages and User/ settings, " +
			"Installed Packages/ with the .sublime-package archives, Local/ with session state, and Cache/ " +
			"plus Index/ as the regenerable pair. Sublime Text 4 reuses the version 3 data directory by " +
			"default, so on a machine that upgraded in place this folder may be the live one rather than a " +
			"leftover — check which of the two is being written to before assuming it is dead.",
		Effects: "Quit Sublime Text first. Only Cache/ and Index/ go; packages, settings, key bindings and the " +
			"unsaved buffers in Local/ survive, and version 4 keeps reading this directory afterward if that " +
			"is how it is configured. Both folders rebuild on next launch at the cost of one slower start and " +
			"a brief re-index.",
		Commands: []string{
			`# Quit Sublime Text first`,
			`rm -rf ~/Library/Application\ Support/Sublime\ Text\ 3/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Sublime\ Text\ 3/Index/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Index/*",
		},
	},
	"TextMate": {
		Score: Caution,
		Description: "TextMate's support folder, split by design into bundles TextMate manages and bundles you " +
			"do. Managed/Bundles holds the language bundles TextMate downloads and updates itself from its own " +
			"repository. Bundles/ and Pristine Copy/Bundles hold bundles you installed or wrote, and TextMate " +
			"deliberately stores only your diff against a default item there so an upgrade never clobbers a " +
			"customisation — which also means those folders are not reproducible from anywhere else.",
		Effects: "Quit TextMate first. Only the managed bundle set is removed; anything you installed by hand or " +
			"customised stays put, because those live in the sibling folders this action does not touch. " +
			"TextMate re-downloads the managed bundles the next time it checks for updates, so syntax " +
			"highlighting and commands for those languages are missing until it does — which needs network " +
			"access.",
		Commands: []string{
			`# Quit TextMate first`,
			`rm -rf ~/Library/Application\ Support/TextMate/Managed/Bundles/*`,
		},
		CleanPaths: []string{"Managed/Bundles/*"},
	},
	"Nova": {
		Score: Risky,
		Description: "Support folder for Nova, Panic's native macOS code editor. Extensions/ is effectively the " +
			"whole thing, and Panic's own migration guide treats it as the unit you copy to move Nova to " +
			"another Mac: each extension gets a subfolder keyed by identifier, and that subfolder holds the " +
			"extension's settings as well as the extension itself. Nova keeps per-project settings in a hidden " +
			".nova folder inside each project instead, and servers and keys in Panic Sync.",
		Effects: "No clean action is offered. There is no cache/settings split to exploit here — deleting " +
			"Extensions/ would take every extension's configuration with it, and Nova gives you no way to " +
			"restore just the settings afterward. Remove individual extensions from Nova's own Extension " +
			"Library instead, which uninstalls them cleanly.",
	},
	"BBEdit": {
		Score: Risky,
		Description: "BBEdit's support folder, and it is all things you or someone else wrote: Scripts/ holds " +
			"AppleScripts, shell scripts and Automator workflows exposed in the Scripts menu, Text Filters/ " +
			"the programs BBEdit pipes selected text through, plus Clippings/, Language Modules/, " +
			"Color Schemes/, Packages/ and Setup/ for saved bookmarks, Grep patterns and file filters. Note " +
			"BBEdit can be configured to keep this folder in Dropbox instead, so on some machines the copy in " +
			"~/Library is the empty one.",
		Effects: "No clean action is offered. Nothing in here is a cache — it is a collection of user-authored " +
			"scripts, filters, clippings and saved search patterns that in many cases exist nowhere else, and " +
			"the folder is small. Remove individual items through BBEdit's Folders menu, which opens the " +
			"relevant subfolder so you can see what you are deleting.",
	},
	"Atom": {
		Score: Caution,
		Description: "The Electron profile of Atom, GitHub's editor — and pure dead weight on any current " +
			"machine. GitHub sunset Atom on 15 December 2022, archived every repository, and shut down the " +
			"package registry it depended on; the last release was 1.63.1 from November 2022, and nothing has " +
			"shipped since. This folder holds Chromium-side state only: Cache/, GPUCache/, blob_storage/, " +
			"Local Storage/, IndexedDB/, Session Storage/, Cookies, Preferences and storage/ (serialised " +
			"window and project state). Your actual Atom configuration is not here — config.cson, keymap.cson, " +
			"snippets, styles and every installed package live in ~/.atom, in your home directory.",
		Effects: "Quit Atom first on the off chance it is still installed. Removes the whole Electron profile, " +
			"which costs you serialised window layout and per-window session state and nothing else — " +
			"~/.atom, with all your settings and packages, is untouched and is what the Pulsar fork imports " +
			"from if you ever migrate. On a machine where Atom is long gone this folder is simply orphaned " +
			"data for an application that no longer receives updates or has a package registry to talk to.",
		Commands: []string{
			`# Quit Atom first, if it is somehow still installed`,
			`rm -rf ~/Library/Application\ Support/Atom/*`,
		},
	},
	"com.github.atom.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Squirrel's \"ShipIt\" auto-update helper for Atom, used " +
			"transiently while installing an update. Since Atom was discontinued in December 2022 and its " +
			"update endpoints no longer serve anything, this folder can never be written to again.",
		Effects: "Removes stale updater scratch files for an application that no longer publishes updates. " +
			"Nothing that still runs on this machine reads this folder.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.github.atom.ShipIt/*`},
	},
	"NetBeans": {
		Score: Caution,
		Description: "Apache NetBeans' user directory, one subfolder per major version (21, 22, ...) because " +
			"netbeans.conf points userdir at ~/Library/Application Support/NetBeans on macOS. Each version's " +
			"folder is settings, not cache: registered JDKs and application servers, editor and formatting " +
			"preferences, key bindings, and every plugin you installed through the Plugin Manager. NetBeans " +
			"keeps its actual cache strictly separate, in ~/Library/Caches/NetBeans, precisely because the two " +
			"must not share a path.",
		Effects: "Open this folder to see which NetBeans versions are taking space, but nothing inside is " +
			"offered for deletion. Removing a version's folder resets that install to a fresh-out-of-the-box " +
			"state — you lose registered JDKs and servers, editor settings, key bindings and installed plugins, " +
			"and get the setup wizard again on next launch. Your projects are unaffected wherever you saved " +
			"them. The disposable half is ~/Library/Caches/NetBeans, cleanable from the Cache tab.",
		Container: true,
	},
}

var ideCachesDB = map[string]Entry{

	// --- Shared JetBrains folders, found inside Caches/JetBrains ---
	//
	// The per-version IDE folders beside these (GoLand2026.2 and friends) are
	// handled by jetbrainsCachePattern in caches.go, not here.

	"Toolbox": {
		Score: Caution,
		Description: "The JetBrains Toolbox App's cache, and usually the largest thing in Caches/JetBrains that " +
			"is not an IDE — 420MB on this machine. download/ is nearly all of it: the staged .dmg and patch " +
			"files Toolbox fetched to install or update your IDEs, kept under content-hash names long after the " +
			"install finished, plus the Toolbox installer itself. Around it sit plugin-icons/ and icons.txt " +
			"(marketplace artwork for the plugin browser), temp/ and recovery/ scratch space, and two things " +
			"that are not cache at all: ports/ and a set of live s-* Unix domain sockets a running Toolbox uses " +
			"to talk to its daemon and to your IDEs.",
		Effects: "Quit JetBrains Toolbox first. Removes the staged installers and patches, the temp directory " +
			"and the plugin marketplace artwork — the large majority of the folder. Your installed IDEs, their " +
			"settings, licenses and your JetBrains account sign-in are all elsewhere and completely unaffected; " +
			"Toolbox simply re-downloads an installer the next time it actually needs one, and re-fetches " +
			"plugin icons as you browse. The live sockets and ports/ are deliberately left alone so a running " +
			"Toolbox is not cut off from its daemon.",
		Commands: []string{
			`# Quit JetBrains Toolbox first`,
			`rm -rf ~/Library/Caches/JetBrains/Toolbox/download/*`,
			`rm -rf ~/Library/Caches/JetBrains/Toolbox/temp/*`,
			`rm -rf ~/Library/Caches/JetBrains/Toolbox/plugin-icons/*`,
		},
		CleanPaths: []string{
			"download/*",
			"temp/*",
			"plugin-icons/*",
		},
	},
	"Daemon": {
		Score: Safe,
		Description: "Scratch space for the JetBrains Toolbox Daemon, the background helper that installs and " +
			"updates IDEs and brokers remote-development sessions. Its whole content is remDev/: " +
			"frontend-download/ holds JetBrains Client frontends downloaded so you can connect to a remote IDE " +
			"backend, and tmp/ is transfer scratch. The daemon's own binary is not here — that is under " +
			"~/Library/Application Support/JetBrains/Daemon.",
		Effects: "Quit JetBrains Toolbox first so the daemon is not mid-transfer. Removes downloaded remote-" +
			"development client frontends and transfer scratch only. Nothing local breaks; the next time you " +
			"open a remote development session, the matching client is re-downloaded automatically, which " +
			"needs network access and adds a one-time delay before that session starts.",
		Commands: []string{
			`# Quit JetBrains Toolbox first`,
			`rm -rf ~/Library/Caches/JetBrains/Daemon/*`,
		},
	},
	"acp-agents": {
		Score: Safe,
		Description: "Downloaded artwork for the Agent Client Protocol agent registry your JetBrains IDEs browse " +
			"— a .icons/ folder with one subfolder per known agent (Claude, GitHub Copilot, Junie, Codex, " +
			"Gemini, Cursor, Cline, Devin and a few dozen more), each holding one icon file per agent version " +
			"the IDE has ever listed. About 5MB here and it only grows as the registry does. The agents' actual " +
			"downloaded runtimes are per-IDE-version, in the acp-agents folder inside each " +
			"GoLand<year>.<n>-style directory beside this one, and those are the ones that reach a gigabyte.",
		Effects: "Removes cached agent icons only. Which agents you have installed, their versions and the terms " +
			"you accepted are recorded under ~/Library/Application Support/JetBrains/acp-agents and are not " +
			"touched, so nothing is uninstalled and no agent stops working. The agent picker briefly shows " +
			"placeholder icons until it re-downloads them.",
		Commands: []string{`rm -rf ~/Library/Caches/JetBrains/acp-agents/*`},
	},
	"Fleet": {
		Score: Safe,
		Description: "JetBrains Fleet's cache directory — the counterpart to its config folder under Application " +
			"Support, holding downloaded language-server backends, workspace indexes and general scratch. Fleet " +
			"was discontinued on 22 December 2025 and superseded by Air, so on most machines this is now a " +
			"cache for an editor that is no longer developed and whose server-dependent features have been " +
			"switching off since.",
		Effects: "Quit Fleet first if you somehow still run it. Removes indexes and downloaded backends only; " +
			"Fleet's settings and plugins live under ~/Library/Application Support/JetBrains/Fleet and are not " +
			"touched, so a surviving install still starts and still has your configuration — it just re-indexes " +
			"and re-downloads what it needs. On a machine where Fleet is gone this is pure leftover.",
		Commands: []string{
			`# Quit Fleet first, if it is still installed`,
			`rm -rf ~/Library/Caches/JetBrains/Fleet/*`,
		},
	},

	// --- VS Code forks ---

	"com.vscodium": {
		Score: Caution,
		Description: "VSCodium's cache: GPU shader cache, extension-host scratch data and workspace indexes " +
			"used to reopen recent folders quickly. VSCodium is the community build of VS Code's source without " +
			"Microsoft's branding, telemetry or marketplace. Its settings, workspace state and downloaded " +
			"extension archives are not here — those are in ~/Library/Application Support/VSCodium and " +
			"~/.vscode-oss/extensions.",
		Effects: "Quit VSCodium first. Rebuilt automatically on next launch; you may lose some per-workspace UI " +
			"state such as recent search history, and the first launch afterward is slightly slower while " +
			"caches rebuild. Installed extensions and settings are not affected.",
		Commands: []string{`# Quit VSCodium first`, `rm -rf ~/Library/Caches/com.vscodium/*`},
	},
	"com.vscodium.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Squirrel's \"ShipIt\" auto-update helper for VSCodium, " +
			"used transiently while installing an update and normally cleaned up afterward.",
		Effects: "Removes stale updater scratch files only; VSCodium itself, its extensions and its settings are " +
			"unaffected.",
		Commands: []string{`rm -rf ~/Library/Caches/com.vscodium.ShipIt/*`},
	},
	"com.exafunction.windsurf": {
		Score: Caution,
		Description: "Cache for the Windsurf editor (bundle id still carries Codeium's original company name, " +
			"Exafunction): GPU shader cache, extension-host scratch and workspace indexes. Windsurf's settings, " +
			"workspace history and sign-in are in ~/Library/Application Support/Windsurf, its extensions in " +
			"~/.windsurf and its AI engine in ~/.codeium — none of which is in this folder.",
		Effects: "Quit Windsurf first. Rebuilt automatically on next launch. You stay signed in and keep every " +
			"setting and extension; the only cost is a slightly slower first launch and the loss of some " +
			"per-workspace UI state such as recent search history.",
		Commands: []string{`# Quit Windsurf first`, `rm -rf ~/Library/Caches/com.exafunction.windsurf/*`},
	},
	"com.exafunction.windsurf.ShipIt": {
		Score: Safe,
		Description: "A leftover working directory from Squirrel's \"ShipIt\" auto-update helper for Windsurf, " +
			"used transiently while installing an update.",
		Effects:  "Removes stale updater scratch files only; Windsurf, its extensions and your sign-in are unaffected.",
		Commands: []string{`rm -rf ~/Library/Caches/com.exafunction.windsurf.ShipIt/*`},
	},
	"Positron": {
		Score: Caution,
		Description: "Cache for Positron, Posit's Code OSS-based data-science IDE. Holds the usual Electron " +
			"editor cache — shader cache, extension-host scratch, workspace indexes — and nothing about your R " +
			"or Python environments, which live in the projects and package libraries themselves. Positron's " +
			"settings and workspace state are under ~/Library/Application Support/Positron.",
		Effects: "Quit Positron first. Rebuilt automatically on next launch; you may lose some per-workspace UI " +
			"state and get one slower start. Registered interpreters, installed extensions and settings are not " +
			"in this folder and are unaffected.",
		Commands: []string{`# Quit Positron first`, `rm -rf ~/Library/Caches/Positron/*`},
	},

	// --- Other editors ---

	"Sublime Text": {
		Score: Safe,
		Description: "Sublime Text 4's cache directory. Sublime is unusual in splitting its throwaway data " +
			"across two places depending on version and platform — some builds keep Cache/ and Index/ inside " +
			"the data directory under Application Support instead — so this folder may be the live cache, a " +
			"partial one, or empty. Whatever is in it is parsed package data and symbol-index material, never " +
			"packages or settings.",
		Effects: "Quit Sublime Text first. Sublime rebuilds this on next launch: the first start is slower and " +
			"Goto Definition is incomplete for a short while as it re-indexes. Your packages, settings, key " +
			"bindings and unsaved buffers live in ~/Library/Application Support/Sublime Text and are not " +
			"touched.",
		Commands: []string{`# Quit Sublime Text first`, `rm -rf ~/Library/Caches/Sublime\ Text/*`},
	},
	"Sublime Text 3": {
		Score: Safe,
		Description: "Sublime Text 3's cache directory — parsed package data and the symbol index, kept apart " +
			"from version 4's by the trailing 3. Because Sublime Text 4 reuses the version 3 data directory by " +
			"default on an in-place upgrade, this folder can still be written to by a current install rather " +
			"than being a leftover.",
		Effects: "Quit Sublime Text first. Everything here is rebuilt on next launch, at the cost of one slower " +
			"start and a brief re-index. Packages, settings, key bindings and unsaved buffers are stored under " +
			"Application Support, not here, and are unaffected.",
		Commands: []string{`# Quit Sublime Text first`, `rm -rf ~/Library/Caches/Sublime\ Text\ 3/*`},
	},
	"NetBeans": {
		Score: Caution,
		Description: "Apache NetBeans' cache directory, one subfolder per major version, pointed here by " +
			"cachedir in netbeans.conf. It holds parsed class and index data used to make project loading, " +
			"syntax highlighting and code completion fast, and NetBeans keeps it strictly separate from the " +
			"user directory under Application Support — the two are not allowed to share a path. Clearing it is " +
			"NetBeans' own standard remedy for slow startup, unresponsive plugins and phantom compile errors " +
			"caused by a stale index.",
		Effects: "Quit NetBeans first, because a running instance holds these files open. Nothing but " +
			"regenerable index data is removed: registered JDKs, servers, editor settings, key bindings and " +
			"installed plugins are all in the user directory under Application Support and survive intact, and " +
			"your project sources are wherever you saved them. Expect one noticeably slow startup while " +
			"NetBeans re-scans and re-indexes your open projects.",
		Commands: []string{`# Quit NetBeans first`, `rm -rf ~/Library/Caches/NetBeans/*`},
	},
	"Atom": {
		Score: Safe,
		Description: "Atom's cache folder — chiefly the compile cache of transpiled CoffeeScript, Babel and " +
			"TypeScript output that Atom's require() hook generated as it loaded packages. GitHub discontinued " +
			"Atom on 15 December 2022 and archived every repository, so nothing on a current machine writes " +
			"here any more.",
		Effects: "Removes transpiler output that would be regenerated on the next launch if Atom were still " +
			"installed, and is simply orphaned otherwise. Your Atom settings and packages are in ~/.atom, in " +
			"your home directory, and are not touched.",
		Commands: []string{`rm -rf ~/Library/Caches/Atom/*`},
	},
	"com.github.atom": {
		Score: Safe,
		Description: "The bundle-id-named half of Atom's cache, sitting beside the plain Atom folder — " +
			"Chromium-side HTTP and shader cache written by the Electron shell rather than by Atom's own " +
			"package loader. Dead weight since Atom was discontinued in December 2022.",
		Effects: "Removes cached web assets and shader data only. Nothing that still runs reads this folder; if " +
			"Atom is somehow still installed it simply rebuilds the cache on next launch. Settings and packages " +
			"in ~/.atom are untouched.",
		Commands: []string{`rm -rf ~/Library/Caches/com.github.atom/*`},
	},
	"com.apple.dt.Xcode": {
		Score: Safe,
		Description: "Xcode's own application cache — the precompiled module cache and assorted scratch the IDE " +
			"rebuilds on demand. Note that this is the small half of Xcode's disk usage and almost never the " +
			"folder you are looking for: the tens of gigabytes are under ~/Library/Developer, in " +
			"Xcode/DerivedData (per-project build products and indexes, freely regenerable), " +
			"Xcode/iOS DeviceSupport (debug symbols per connected device OS version, 2-5GB each and " +
			"re-downloaded when you next plug the device in), CoreSimulator (simulator runtimes, often 5-8GB " +
			"per iOS version) and Xcode/Archives (which is the one exception — an archive cannot be regenerated " +
			"and holds the dSYMs you need to symbolicate crash reports from a shipped build). This app does not " +
			"scan ~/Library/Developer at all, so none of that appears anywhere in its listing.",
		Effects: "Quit Xcode first. Removes the module cache and scratch data; Xcode rebuilds it transparently, " +
			"which can make the first build after this noticeably slower and may re-download some components. " +
			"No project files, provisioning profiles, certificates, signing identities or archives are stored " +
			"in this folder. If you actually need gigabytes back, clear DerivedData and stale device-support " +
			"folders under ~/Library/Developer by hand, and prune simulators with `xcrun simctl delete " +
			"unavailable` — but leave Xcode/Archives alone unless you are certain a build will never need " +
			"symbolicating again.",
		Commands: []string{`# Quit Xcode first`, `rm -rf ~/Library/Caches/com.apple.dt.Xcode/*`},
	},
}
