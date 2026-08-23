package knowledge

// This file is the dictionary for ~/Library/Application Support and
// ~/Library/Group Containers. Both are the opposite of ~/Library/Caches:
// by Apple's convention this is where an app puts data it expects to keep
// — login sessions, licenses, message databases, game saves, settings.
// Entries here are therefore deliberately stingy. Where an app mixes
// disposable cache into the same folder as its real data (the usual
// Electron layout, where Cache/ sits next to Local Storage/), we never
// offer a whole-folder delete; CleanPaths names the disposable subfolders
// and everything else is left alone.
//
// The Chromium/Electron split used repeatedly below, confirmed against the
// actual folders on this machine:
//
//	disposable — Cache, Code Cache, GPUCache, DawnWebGPUCache,
//	             DawnGraphiteCache, GrShaderCache, Service Worker/CacheStorage
//	keep       — Local Storage, IndexedDB, Session Storage, Cookies,
//	             Preferences, Backups (unsaved editors), Service Worker/Database
//
// Local Storage in particular is where Electron apps park the auth token,
// so wiping it silently signs the user out — exactly the sort of surprise
// this dictionary exists to prevent.

// jetbrainsConfigPattern describes one JetBrains IDE version's folder under
// Application Support. Unlike its Caches counterpart it is deliberately not
// cleanable, and unlike Effective's promotion of superseded cache folders it
// never becomes cleanable either: this is where the IDE keeps settings,
// keymaps, downloaded plugins and the license file. Deciding that an old
// version's configuration is no longer worth keeping is the user's call, so
// the entry exists purely to explain what the folder holds and where the
// space actually went.
var jetbrainsConfigPattern = patternEntry{
	re: jetbrainsVersionRe,
	build: func(name string) Entry {
		return Entry{
			Score: Risky,
			Description: "The configuration directory for one specific JetBrains IDE install (" + name + "). " +
				"Despite sitting next to a lot of large files, none of this is cache: options/, keymaps/, " +
				"codestyles/, colors/ and fileTemplates/ are your IDE settings, settingsSync/ is the copy " +
				"synced to your JetBrains account, jdbc-drivers/ holds database drivers you downloaded, and " +
				"the .key and plugin_*.license files next to them are your actual license. The bulk of the " +
				"size is plugins/ — every plugin you installed or updated, unpacked; on this machine a single " +
				"version's plugins folder runs to ~2.4GB, with github-copilot-intellij, fullLine and ml-llm " +
				"accounting for most of it.",
			Effects: "Nothing here is offered for deletion, on purpose — not even for a version superseded by a " +
				"newer install. Deleting it would drop that install's settings, keymaps, database drivers and " +
				"license file, and no amount of reindexing brings those back. To reclaim the space, either " +
				"uninstall the IDE version through JetBrains Toolbox (which removes this folder as part of a " +
				"clean uninstall) or disable plugins you no longer use from inside the IDE. The genuinely " +
				"disposable half of a JetBrains install — project indexes, Local History, plugin/icon caches, " +
				"AI-agent runtimes — lives under ~/Library/Caches/JetBrains and is cleanable from the Cache tab.",
		}
	},
}

var appSupportDB = map[string]Entry{
	"JetBrains": {
		Score: Caution,
		Description: "A container of per-version configuration directories, one per installed JetBrains IDE " +
			"(GoLand2026.1, PhpStorm2026.2, RustRover2026.1 and so on), alongside small shared folders for " +
			"Toolbox, Fleet, Air and the Daemon helper. Unlike the same-named folder under ~/Library/Caches, " +
			"nothing in here is a cache: it holds IDE settings, keymaps, code styles, downloaded database " +
			"drivers, license files, and — by far the largest part — every plugin you have downloaded, which " +
			"is why one version folder alone can pass 2GB.",
		Effects: "Open this folder to see which IDE version is using what, but expect nothing inside to be " +
			"offered for deletion. Even a version fully superseded by a newer install keeps its settings and " +
			"license here, and discarding those is your decision rather than something this app will make for " +
			"you; JetBrains Toolbox removes them properly when you uninstall a version. The disposable half of " +
			"a JetBrains install lives under ~/Library/Caches/JetBrains and is cleanable from the Cache tab.",
		Container: true,
	},
	// Claude's vm_bundles/ is the largest thing this file still offers to
	// clean, so the reasoning behind the split is written down. Claude.app
	// embeds a manifest of VM releases — each one a 40-hex sha with a
	// compressed artifact per file and published binary deltas from the
	// previous few releases — and records which release every extracted file
	// came from in a sibling .<name>.origin marker. A missing file is rebuilt
	// in a fixed order: promote a prefetched copy out of warm/, decompress the
	// local <name>.zst, download a delta against that .zst, or download the
	// full .zst. That order is exactly why rootfs.img is offered here and
	// rootfs.img.zst is not — keeping the 1.2GB compressed copy is what makes
	// dropping the 5.6GB live image cost either nothing or a few hundred MB.
	// sessiondata.img is spared because it is the sandbox's own saved state,
	// which is also what Claude's own delete-VM-bundle command chooses to keep.
	"Claude": {
		Score: Caution,
		Description: "The Claude desktop app's profile, and two unrelated things sharing one folder — 9.8GB " +
			"of them on this machine. The Electron half is ordinary: Cache/ and Code Cache/ are Chromium's " +
			"disk caches (about 950MB together), while Local Storage/, Cookies/, config.json and " +
			"local-agent-mode-sessions/ hold your signed-in session, your MCP server configuration and the " +
			"history of past local-agent runs. The other 8.2GB is vm_bundles/, the Linux microVM that local " +
			"agent mode and Cowork execute inside. claudevm.bundle/rootfs.img is the live sandbox disk, " +
			"created 10GB but sparse — which is why `ls -l` claims 10GB and `du` reports the 5.6GB it really " +
			"occupies — and it only ever grows, because space freed inside the guest is not handed back to " +
			"the host file; next to it, rootfs.img.zst is the 1.2GB compressed copy of that same image which " +
			"Claude rebuilds it from, vmlinuz and initrd are the guest kernel and boot image (225MB, kept " +
			"both compressed and unpacked), and sessiondata.img is the sandbox's own saved state. " +
			"vm_bundles/warm/ is a pre-fetched 1.2GB compressed copy of the next VM release, and claude-code/ " +
			"plus claude-code-vm/ are the Claude Code runtime for the Mac and for the sandbox (302MB and " +
			"295MB), one directory per version, with superseded versions pruned by Claude itself.",
		Effects: "Quit Claude Desktop first — a running sandbox holds rootfs.img open. Removes the Chromium " +
			"caches, the pre-fetched warm image and the live sandbox disk: about 7.7GB of the 9.8GB here, " +
			"nearly all of it rootfs.img. Nothing holding work goes with it, so you stay signed in, " +
			"config.json keeps your MCP servers, and sessiondata.img, local-agent-mode-sessions/, Local " +
			"Storage/, Cookies/ and IndexedDB/ are deliberately left alone — as is the 1.2GB rootfs.img.zst, " +
			"and sparing that one is the point, because Claude rebuilds rootfs.img by decompressing it " +
			"locally: no download at all when it matches the VM release Claude currently wants, and a ~340MB " +
			"delta download when it is one release behind. (Delete rootfs.img.zst by hand as well if you want " +
			"that 1.2GB too, and the next VM release costs a full ~1.2GB download instead.) What you do give " +
			"up is the sandbox's installed state: the next local-agent or Cowork run boots the factory image " +
			"again and reinstalls whatever a session had added inside the VM, while your project files, which " +
			"live on the Mac rather than in the image, are untouched. Claude ships the same action itself as " +
			"\"Delete Cowork VM Bundle and Restart…\", which also keeps sessiondata.img; its \"Free Up Cowork " +
			"Disk Space…\" command is a different thing that only tidies up inside the running guest and does " +
			"not shrink rootfs.img on this side.",
		Commands: []string{
			`# Quit Claude Desktop first — a running sandbox holds rootfs.img open`,
			`rm -rf ~/Library/Application\ Support/Claude/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Claude/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Claude/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Claude/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Claude/DawnGraphiteCache/*`,
			`rm -rf ~/Library/Application\ Support/Claude/vm_bundles/warm/*`,
			`rm -f ~/Library/Application\ Support/Claude/vm_bundles/claudevm.bundle/rootfs.img`,
			`# rootfs.img.zst is kept on purpose: it is what rebuilds rootfs.img without a download`,
			`# sessiondata.img and local-agent-mode-sessions/ are never touched`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
			"vm_bundles/warm/*",
			"vm_bundles/claudevm.bundle/rootfs.img",
		},
	},
	"Google": {
		Score: Caution,
		Description: "Chrome's profile root — the folder holding your browsing history, cookies, saved " +
			"passwords, extensions and site data, one subfolder per profile (Default, Profile 2, ...). What " +
			"makes it enormous is not any of that: OptGuideOnDeviceModel/ is Chrome's on-device Gemini Nano " +
			"model, a single weights.bin of about 4GB that Chrome downloads on eligible machines to power " +
			"local AI features like page summaries, \"help me write\" and on-device scam detection. Next to " +
			"it sit smaller downloaded models (OptGuideOnDeviceClassifierModel, optimization_guide_model_store), " +
			"the component/extension CRX download cache, and the GPU shader caches.",
		Effects: "Quit Chrome first. Only the downloaded models and shader/CRX caches are removed — around " +
			"4.2GB here. Your history, cookies, saved passwords, extensions, bookmarks and per-site data all " +
			"live in the profile subfolders and are not touched, so you stay logged in everywhere. Chrome's " +
			"local AI features fall back to being unavailable until the model is fetched again, and it will " +
			"quietly re-download the full 4GB unless you turn it off first: open chrome://on-device-internals " +
			"and use Uninstall, or set chrome://flags#optimization-guide-on-device-model to Disabled. Expect a " +
			"brief shader recompile on the first page load afterward.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/OptGuideOnDeviceModel/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/OptGuideOnDeviceClassifierModel/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/optimization_guide_model_store/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/component_crx_cache/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/GrShaderCache/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/GraphiteDawnCache/*`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/ShaderCache/*`,
			`# Chrome re-downloads the 4GB model unless you disable it in chrome://on-device-internals first`,
		},
		CleanPaths: []string{
			"Chrome/OptGuideOnDeviceModel/*",
			"Chrome/OptGuideOnDeviceClassifierModel/*",
			"Chrome/optimization_guide_model_store/*",
			"Chrome/component_crx_cache/*",
			"Chrome/GrShaderCache/*",
			"Chrome/GraphiteDawnCache/*",
			"Chrome/ShaderCache/*",
		},
	},
	"Steam": {
		Score: Caution,
		Description: "The Steam client's own data folder — note that this is not where games are installed " +
			"(those live in steamapps under a Steam library folder, usually on another volume). What is here " +
			"is the client itself (Steam.AppBundle, ~1.1GB), your login and library settings in config/*.vdf, " +
			"cloud saves and screenshots in userdata/, and two large caches: appcache/librarycache/, the " +
			"downloaded capsule/hero/logo artwork for every game in your library, and config/htmlcache/, the " +
			"embedded Chromium cache backing the store, community and overlay pages.",
		Effects: "Quit Steam completely first. Only the artwork cache, the HTTP metadata cache and the embedded " +
			"browser's Cache/Code Cache/shader caches are removed — around 690MB here. Your installed games, " +
			"cloud saves, screenshots, controller configs and login (config.vdf / loginusers.vdf) are all left " +
			"alone, and htmlcache's own Cookies and Local Storage are deliberately skipped so store pages stay " +
			"signed in. Library artwork shows grey placeholders until it re-downloads, and store/community " +
			"pages load from the network once. Steam's own Settings has a \"Delete Web Browser Data\" button " +
			"that clears the same htmlcache folder.",
		Commands: []string{
			`# Quit Steam first`,
			`rm -rf ~/Library/Application\ Support/Steam/appcache/librarycache/*`,
			`rm -rf ~/Library/Application\ Support/Steam/appcache/httpcache/*`,
			`rm -rf ~/Library/Application\ Support/Steam/config/htmlcache/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Steam/config/htmlcache/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Steam/config/htmlcache/GrShaderCache/*`,
			`rm -rf ~/Library/Application\ Support/Steam/config/htmlcache/GPUCache/*`,
		},
		CleanPaths: []string{
			"appcache/librarycache/*",
			"appcache/httpcache/*",
			"config/htmlcache/Cache/*",
			"config/htmlcache/Code Cache/*",
			"config/htmlcache/GrShaderCache/*",
			"config/htmlcache/GPUCache/*",
		},
	},
	"Code": {
		Score: Caution,
		Description: "VS Code's profile folder. User/ is the part you cannot lose: settings.json, keybindings, " +
			"snippets, the per-workspace state in workspaceStorage/ (which extensions use to remember things " +
			"like your chat history) and User/History/, VS Code's own local file history. The rest is " +
			"regenerable — CachedExtensionVSIXs/ keeps the downloaded .vsix installer of every extension long " +
			"after it was installed, CachedData/ holds V8 compile caches keyed by VS Code commit, and Cache/, " +
			"Code Cache/ and the GPU caches are ordinary Chromium caches. Installed extensions themselves live " +
			"in ~/.vscode/extensions, outside this folder entirely.",
		Effects: "Quit VS Code first. Removes the leftover extension installers, the V8 compile caches and the " +
			"Chromium caches — roughly 500MB here. Your settings, keybindings, snippets, per-workspace state, " +
			"local file history and installed extensions all survive, and you stay signed in to Settings Sync " +
			"and any extension accounts. The only visible cost is a slower first launch while CachedData " +
			"rebuilds; extensions are not reinstalled, only their stale download archives go away.",
		Commands: []string{
			`# Quit VS Code first`,
			`rm -rf ~/Library/Application\ Support/Code/CachedExtensionVSIXs/*`,
			`rm -rf ~/Library/Application\ Support/Code/CachedData/*`,
			`rm -rf ~/Library/Application\ Support/Code/CachedProfilesData/*`,
			`rm -rf ~/Library/Application\ Support/Code/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Code/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Code/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Code/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Code/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"CachedExtensionVSIXs/*",
			"CachedData/*",
			"CachedProfilesData/*",
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"Cursor": {
		Score: Caution,
		Description: "Cursor's profile folder, laid out exactly like VS Code's since Cursor is a fork of it: " +
			"User/ holds settings, keybindings and per-workspace state, Backups/ holds unsaved editor " +
			"contents, and CachedData/, CachedExtensionVSIXs/, Cache/, Code Cache/ and the GPU caches are the " +
			"regenerable half.",
		Effects: "Quit Cursor first. Removes only the compile caches, leftover extension installers and " +
			"Chromium caches. Settings, keybindings, workspace state, unsaved-editor backups and your Cursor " +
			"sign-in are untouched; the first launch afterward is slightly slower while CachedData rebuilds.",
		Commands: []string{
			`# Quit Cursor first`,
			`rm -rf ~/Library/Application\ Support/Cursor/CachedExtensionVSIXs/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/CachedData/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/CachedProfilesData/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Cursor/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"CachedExtensionVSIXs/*",
			"CachedData/*",
			"CachedProfilesData/*",
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"Kiro": {
		Score: Caution,
		Description: "Profile folder for Kiro, AWS's VS Code-derived agentic IDE, with the same layout as VS " +
			"Code: User/ for settings and per-workspace state, Backups/ for unsaved editors, and CachedData/, " +
			"Cache/, Code Cache/ and the GPU caches as the disposable half.",
		Effects: "Quit Kiro first. Removes only the V8 compile caches and Chromium caches; settings, workspace " +
			"state, unsaved-editor backups and your AWS/Kiro sign-in (kept in Local Storage and Cookies) are " +
			"untouched. Expect one slightly slower launch while the caches rebuild.",
		Commands: []string{
			`# Quit Kiro first`,
			`rm -rf ~/Library/Application\ Support/Kiro/CachedData/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/CachedProfilesData/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Kiro/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"CachedData/*",
			"CachedProfilesData/*",
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"discord": {
		Score: Caution,
		Description: "The Discord desktop app's Electron profile. Cache/ and Code Cache/ are the image, emoji " +
			"and script caches and account for most of the size; the numbered folder next to them (0.0.391 " +
			"here) is the currently installed client module set that Discord downloads and updates itself. " +
			"Local Storage/ is small but critical — it is where the Discord auth token lives — and " +
			"settings.json holds your client settings.",
		Effects: "Quit Discord first. Clears the image/emoji and script caches only; avatars, emoji and inline " +
			"images reload from the network the first time you see them again. You stay logged in, keep every " +
			"setting, and the installed client modules are left in place so Discord starts normally instead of " +
			"re-downloading itself.",
		Commands: []string{
			`# Quit Discord first`,
			`rm -rf ~/Library/Application\ Support/discord/Cache/*`,
			`rm -rf ~/Library/Application\ Support/discord/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/discord/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/discord/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/discord/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"dev.warp.Warp-Stable": {
		Score: Caution,
		Description: "Support data for the Warp terminal, almost all of which is autoupdate/ — a downloaded " +
			".dmg of a Warp release plus the .app extracted from it, staged there by the built-in updater and " +
			"left behind afterward. The genuinely valuable Warp state (command history, workflows, AI " +
			"conversations) is not here; it lives in warp.sqlite under ~/Library/Group " +
			"Containers/2BBY89MBSN.dev.warp.",
		Effects: "Quit Warp first so you don't delete an update mid-install. Removes the staged installer and " +
			"extracted app bundle only — about 615MB here. Your history, workflows, settings, themes and MCP " +
			"configuration are untouched, and the already-installed Warp in /Applications keeps running; the " +
			"updater simply re-downloads the next release when one appears.",
		Commands: []string{
			`# Quit Warp first`,
			`rm -rf ~/Library/Application\ Support/dev.warp.Warp-Stable/autoupdate/*`,
		},
		CleanPaths: []string{"autoupdate/*"},
	},
	"Setapp": {
		Score: Caution,
		Description: "Support folder for the Setapp subscription client. LaunchAgents/ is not a cache at all — " +
			"it contains the actual Setapp, SetappAssistant, SetappLauncher and SetappUpdater app bundles that " +
			"the service runs from. Default/ is the disposable half: SetappIcons/ holds app icons for the " +
			"catalog UI and MediaCache/ holds the .mp4 promo videos Setapp plays on app detail pages, with " +
			"Default/Databases/ storing the catalog index itself.",
		Effects: "Quit Setapp first. Only the downloaded icons and promo videos are removed — about 195MB here. " +
			"Your subscription, sign-in, and every app you installed through Setapp are unaffected, and the " +
			"helper bundles in LaunchAgents/ are deliberately left alone so the service keeps working. The " +
			"catalog just re-fetches artwork and videos as you browse it again.",
		Commands: []string{
			`# Quit Setapp first`,
			`rm -rf ~/Library/Application\ Support/Setapp/Default/SetappIcons/*`,
			`rm -rf ~/Library/Application\ Support/Setapp/Default/MediaCache/*`,
		},
		CleanPaths: []string{
			"Default/SetappIcons/*",
			"Default/MediaCache/*",
		},
	},
	"osu": {
		Score: Caution,
		Description: "osu!'s entire user data directory, and the game says so itself in the IMPORTANT READ ME " +
			"file it drops in there: files/ holds the raw pieces of every beatmap, skin and replay you have, " +
			"client.realm is the database tying them together with your local scores and settings, and " +
			"online.db is the downloaded beatmap listing. Only cache/ is throwaway — a compiled shader cache, " +
			"a rasterised font cache and a small Sentry crash-reporting buffer.",
		Effects: "Quit osu! first. Only cache/ is removed, about 62MB here. Every beatmap, skin, replay, local " +
			"score and setting is left exactly as it was — this app will not touch anything else in this " +
			"folder, because osu! stores it as an inseparable unit and losing part of it loses data " +
			"permanently. The game recompiles shaders and re-rasterises fonts on the next launch, which makes " +
			"that one launch a little slower.",
		Commands: []string{
			`# Quit osu! first`,
			`rm -rf ~/Library/Application\ Support/osu/cache/*`,
		},
		CleanPaths: []string{"cache/*"},
	},
	"TIDAL": {
		Score: Caution,
		Description: "The TIDAL desktop app's Electron profile: Cache/, Code Cache/ and Service " +
			"Worker/CacheStorage/ hold cached album artwork and web assets, component_crx_cache/ holds " +
			"downloaded Chromium components, and WidevineCdm/ is the DRM module required to play protected " +
			"streams. Your sign-in and offline library metadata are in Local Storage/ and IndexedDB/.",
		Effects: "Quit TIDAL first. Removes cached artwork, web assets and shader caches — album art and UI " +
			"resources reload from the network once. You stay signed in, and the Widevine DRM module is " +
			"deliberately left in place so playback is not interrupted by a re-download the next time you " +
			"press play.",
		Commands: []string{
			`# Quit TIDAL first`,
			`rm -rf ~/Library/Application\ Support/TIDAL/Cache/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/Service\ Worker/CacheStorage/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/component_crx_cache/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/TIDAL/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Code Cache/*",
			"Service Worker/CacheStorage/*",
			"component_crx_cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"zoom.us": {
		Score: Caution,
		Description: "The Zoom client's support folder, nearly all of which is asr/ — a code-signed bundle of " +
			"ONNX models (a quantised encoder, decoder and joint network, plus voice-activity detection, " +
			"language identification and a vocabulary) that Zoom downloads to run English speech recognition " +
			"on device for live captions and transcription. data/ next to it holds the client's own small " +
			"database; your account sign-in and meeting settings are kept in preferences and the Keychain, not " +
			"here.",
		Effects: "Quit Zoom first. Removes the downloaded speech-recognition model, about 169MB. You stay " +
			"signed in and every meeting setting survives; the only effect is that on-device live captions are " +
			"unavailable until Zoom re-downloads the model, which it does automatically the next time you " +
			"switch captions on with a network connection.",
		Commands: []string{
			`# Quit Zoom first`,
			`rm -rf ~/Library/Application\ Support/zoom.us/asr/*`,
		},
		CleanPaths: []string{"asr/*"},
	},
	"Raindrop.io": {
		Score: Caution,
		Description: "Electron profile for the Raindrop.io bookmark manager. Cache/ and Code Cache/ are " +
			"Chromium's page and script caches for the app's web UI, while IndexedDB/ and Local Storage/ hold " +
			"the offline copy of your collections and your signed-in session.",
		Effects: "Quit Raindrop first. Clears the web asset and script caches only — about 35MB. Your " +
			"bookmarks, collections and sign-in are stored in IndexedDB and Local Storage and are not touched; " +
			"the app re-fetches page assets and thumbnails from the network on next launch.",
		Commands: []string{
			`# Quit Raindrop.io first`,
			`rm -rf ~/Library/Application\ Support/Raindrop.io/Cache/*`,
			`rm -rf ~/Library/Application\ Support/Raindrop.io/Code\ Cache/*`,
			`rm -rf ~/Library/Application\ Support/Raindrop.io/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Raindrop.io/DawnWebGPUCache/*`,
			`rm -rf ~/Library/Application\ Support/Raindrop.io/DawnGraphiteCache/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"Code Cache/*",
			"GPUCache/*",
			"DawnWebGPUCache/*",
			"DawnGraphiteCache/*",
		},
	},
	"apidog": {
		Score: Caution,
		Description: "Electron profile for the Apidog API client. Collections/ and the data-storage-*.json " +
			"files next to it are your saved API collections and request history, and ApidogAppAgent.app is a " +
			"helper bundle the app launches; Cache/, GPUCache/ and DawnCache/ are ordinary Chromium caches.",
		Effects: "Quit Apidog first. Only the Chromium caches are removed. Your API collections, environments, " +
			"request history and sign-in stay exactly where they are, and the bundled helper app is left " +
			"alone so Apidog starts normally.",
		Commands: []string{
			`# Quit Apidog first`,
			`rm -rf ~/Library/Application\ Support/apidog/Cache/*`,
			`rm -rf ~/Library/Application\ Support/apidog/GPUCache/*`,
			`rm -rf ~/Library/Application\ Support/apidog/DawnCache/*`,
		},
		CleanPaths: []string{
			"Cache/*",
			"GPUCache/*",
			"DawnCache/*",
		},
	},
	"DEVSENSE": {
		Score: Caution,
		Description: "Support folder for DEVSENSE's PHP tooling (PHP Tools for VS Code, and the PHP plugin " +
			"they ship for other editors). Its only content is packages-cache/, a downloaded index of PHP " +
			"package metadata used for Composer autocompletion and dependency lookups.",
		Effects: "Removes the cached package index, around 9MB. No project files, licenses or editor settings " +
			"live here; the tooling rebuilds the index the next time it needs to resolve a Composer package, " +
			"which requires network access and briefly makes package autocompletion less complete.",
		Commands: []string{
			`rm -rf ~/Library/Application\ Support/DEVSENSE/packages-cache/*`,
		},
		CleanPaths: []string{"packages-cache/*"},
	},
	"com.apple.wallpaper": {
		Score: Caution,
		Description: "Where macOS stores the aerial wallpapers and screensavers it downloads on demand: " +
			"aerials/videos/ holds the full-resolution .mov files (each one runs to hundreds of MB), " +
			"aerials/thumbnails/ holds the small previews the wallpaper picker shows, and aerials/manifest " +
			"describes the catalog. Which wallpaper you actually selected is recorded separately in " +
			"Store/Index.plist.",
		Effects: "Removes the downloaded aerial videos only — 416MB here. Your wallpaper choice is not reset " +
			"and the picker keeps its previews, because thumbnails and the manifest are left alone. If your " +
			"current wallpaper is an aerial, macOS shows its still frame and re-downloads the video in the " +
			"background the next time it wants to play it, which needs a network connection.",
		Commands: []string{
			`rm -rf ~/Library/Application\ Support/com.apple.wallpaper/aerials/videos/*`,
		},
		CleanPaths: []string{"aerials/videos/*"},
	},
	"com.apple.mobileAssetDesktop": {
		Score: Caution,
		Description: "The high-resolution .heic desktop pictures macOS downloads on demand — the dynamic and " +
			"still wallpapers that ship with the system (The Cliffs, Ventura Graphic, Tree, Motion Blue and " +
			"friends), each stored as a single multi-megabyte file containing every time-of-day variant.",
		Effects: "Removes the downloaded wallpaper images. Nothing about your settings changes and your " +
			"wallpaper selection is remembered; if the picture you are currently using was one of these, macOS " +
			"falls back to a low-resolution preview and re-downloads the full file in the background, so a " +
			"network connection is needed before it looks right again.",
		Commands: []string{
			`rm -rf ~/Library/Application\ Support/com.apple.mobileAssetDesktop/*`,
		},
	},

	// The remaining entries are description-only on purpose: they carry real,
	// non-regenerable user data mixed too finely to carve up safely, so
	// CanClean is false for all of them (Score set, no Commands). Leaving them
	// out entirely would make them Unknown, which is equally uncleanable but
	// tells the user nothing about the space they're looking at.

	"iMazing": {
		Score: Risky,
		Description: "iMazing's own library, not its device backups (those go wherever you pointed iMazing, " +
			"by default ~/Library/Application Support/MobileSync). Almost all of the size is Library/Apps/, " +
			"the .ipa archives of iOS apps iMazing pulled off a connected device — on this machine two " +
			"archives account for over 560MB. Prefs/ holds iMazing's settings and Library/Apps/Icons/ the app " +
			"artwork it shows alongside them.",
		Effects: "No clean action is offered here. Those .ipa archives are frequently impossible to obtain " +
			"again: Apple does not let you re-download arbitrary app versions, and if an app has since been " +
			"delisted or updated, the copy in this folder is the only one you have. If you know you no longer " +
			"need them, remove individual archives from inside iMazing's own Library manager, which updates " +
			"its index at the same time instead of leaving it pointing at missing files.",
	},
	"factorio": {
		Score: Risky,
		Description: "Factorio's user data directory. saves/ holds your save games, mods/ the mods you " +
			"installed, config/ your key bindings and graphics settings, and player-data.json your login " +
			"token and per-player state. The only genuinely disposable item is crop-cache.dat, a few MB of " +
			"pre-cropped sprite atlas the game regenerates on launch.",
		Effects: "No clean action is offered. Save games are irreplaceable and there is not enough disposable " +
			"data here to be worth the risk of a mistargeted delete. If you want the space back, delete " +
			"specific old saves from inside the game's load-game screen, where you can see what each one is " +
			"before removing it.",
	},
	"com.openai.chat": {
		Score: Risky,
		Description: "Local storage for the ChatGPT desktop app. The conversations-v3-<account-id> folder is a " +
			"local copy of your conversation history and the project-g-p-* folders mirror your ChatGPT " +
			"projects, all keyed to the signed-in account. Note that the app's throwaway UI cache is a " +
			"separate folder under ~/Library/Caches, not this one.",
		Effects: "No clean action is offered. Although conversations are also stored on your ChatGPT account, " +
			"this local copy is what the app reads offline and what makes search instant, and there is no " +
			"reliable way to tell from the filenames which parts have been synced. Clear the Caches entry " +
			"instead if you want to reclaim space from this app.",
	},
	"CEF": {
		Score: Caution,
		Description: "A shared profile folder written by apps embedding the Chromium Embedded Framework " +
			"without setting their own path, so the owning app is not identifiable from the folder name. On " +
			"this machine it contains only User Data/WidevineCdm — the Widevine DRM module used to play " +
			"protected audio and video — plus an empty dictionary folder.",
		Effects: "No clean action is offered, because deleting the Widevine module breaks protected media " +
			"playback in whichever app owns this folder until it re-downloads the component, and there is no " +
			"way to tell from here which app that is. The folder is small enough that it is not worth finding " +
			"out.",
	},
}

var groupContainersDB = map[string]Entry{
	"HUAQ24HBR6.dev.orbstack": {
		Score: Risky,
		Description: "OrbStack's entire data store, and typically the single largest folder in Group " +
			"Containers. data/data.img.raw is one sparse disk image holding every Docker image, container, " +
			"volume and Linux machine you have — it advertises a huge apparent size (228GB here) while only " +
			"occupying what is actually used (about 12.6GB on disk), which is why `ls -l` and `du` disagree so " +
			"wildly; OrbStack even ships a README.txt in that folder explaining it. data/swap.img is the VM's " +
			"1GB swap file.",
		Effects: "No clean action is offered here, and deliberately so: deleting data.img.raw destroys every " +
			"image, container, volume and Linux machine in one go, with no undo. Reclaiming space is done from " +
			"inside the VM instead — this app has a separate Docker tab for pruning images, volumes and build " +
			"cache, which is the safe way to do it. Be aware that pruning inside the VM does not always shrink " +
			"the host file straight away: the freed blocks are returned to the sparse image lazily, so " +
			"data.img.raw can keep reporting its old on-disk size for a while after a prune.",
	},
	"6N38VWS5BX.ru.keepcoder.Telegram": {
		Score: Caution,
		Description: "Telegram's shared container, holding one appstore/account-<id> folder per signed-in " +
			"account. Inside it, postbox/db/db_sqlite is the message database — your entire chat history, over " +
			"1GB here — and postbox/media/ is the media store, tens of thousands of hashed folders of photos, " +
			"videos, voice notes and documents. Within that store, postbox/media/cache/ is specifically the " +
			"size-limited cache Telegram manages and evicts on its own, and appstore/temp/ plus " +
			"appstore/trlottie-animations/ hold in-flight downloads and rendered animated stickers.",
		Effects: "Quit Telegram first. Only the managed media cache, the temp folder and the rendered sticker " +
			"animations are removed — around 2.2GB here. Your message history, saved logins in " +
			"accounts-metadata/, chat wallpapers and call history all survive, and nothing is deleted from " +
			"Telegram's servers, so cached photos and videos simply re-download the next time you scroll back " +
			"to them. For a deeper clean with a proper preview of what goes, use Telegram's own Settings → " +
			"Data and Storage → Storage Usage, which can also drop the permanently downloaded media this " +
			"action leaves in place.",
		Commands: []string{
			`# Quit Telegram first`,
			`rm -rf ~/Library/Group\ Containers/6N38VWS5BX.ru.keepcoder.Telegram/appstore/account-*/postbox/media/cache`,
			`rm -rf ~/Library/Group\ Containers/6N38VWS5BX.ru.keepcoder.Telegram/appstore/temp/*`,
			`rm -rf ~/Library/Group\ Containers/6N38VWS5BX.ru.keepcoder.Telegram/appstore/trlottie-animations/*`,
		},
		CleanPaths: []string{
			"appstore/account-*/postbox/media/cache",
			"appstore/temp/*",
			"appstore/trlottie-animations/*",
		},
	},
	"2BBY89MBSN.dev.warp": {
		Score: Risky,
		Description: "Warp's shared container, which despite the small size is the valuable half of Warp's " +
			"data: warp.sqlite holds your command history, saved workflows, notebooks and AI conversation " +
			"history, with the usual -wal and -shm files beside it. codebase_index_snapshots/ is where Warp " +
			"caches indexes of repositories you have opened, though it is empty on this machine. The bulky, " +
			"disposable part of Warp — staged auto-update downloads — is under ~/Library/Application " +
			"Support/dev.warp.Warp-Stable instead.",
		Effects: "No clean action is offered. Deleting warp.sqlite wipes your command history, workflows and " +
			"saved AI conversations with no way to recover them, and the folder is far too small to justify " +
			"that. Clean the Application Support entry instead, which is where Warp's hundreds of megabytes " +
			"actually are.",
	},
	"group.net.whatsapp.WhatsApp.shared": {
		Score: Risky,
		Description: "WhatsApp's shared container and the only place your desktop WhatsApp data exists. " +
			"ChatStorage.sqlite is the message database, Message/ and Media/ hold message payloads and " +
			"attachments, and Axolotl.sqlite holds the Signal-protocol encryption keys that identify this " +
			"device to your account. ContactsV2.sqlite, CallHistory.sqlite and the various key-value stores " +
			"round out the rest.",
		Effects: "No clean action is offered, and this folder should be treated as off-limits. Deleting any of " +
			"it — the encryption keys especially — unlinks this Mac from your WhatsApp account and loses any " +
			"message history that was not backed up elsewhere, since WhatsApp's desktop history is not " +
			"restored from the server. Use WhatsApp's own Settings → Storage to remove downloaded media if you " +
			"need the space.",
	},
	"S8EX82NJP6.com.macpaw.CleanMyMac-setapp": {
		Score: Caution,
		Description: "The shared container for CleanMyMac (Setapp edition), used to pass state between the " +
			"main app and its helper/monitoring extensions — scan bookkeeping, the health-monitor's history, " +
			"and the list of items it has been told to ignore. CleanMyMac's much larger scan-result cache " +
			"lives under ~/Library/Caches/com.macpaw.CleanMyMac-setapp instead.",
		Effects: "No clean action is offered. The folder is only a few MB, and the ignore-list it holds is a " +
			"user decision — dropping it would quietly re-arm CleanMyMac to offer files you previously told it " +
			"to leave alone. Clean the Caches entry instead if you want to reclaim CleanMyMac's space.",
	},
}
