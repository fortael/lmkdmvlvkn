package knowledge

// This file is the dictionary for the Applications tab: the .app bundles
// installed in /Applications and ~/Applications, plus the folder names you
// run into after drilling into one of them.
//
// Unlike every other root, nothing in this map is cleanable — on purpose.
// Not one entry carries Commands, so CanClean() is false for all of them
// and the UI can only ever describe what you're looking at. There are two
// separate reasons for that, and both are load-bearing:
//
//  1. Anything INSIDE a bundle is covered by that bundle's code signature.
//     Deleting a single file — an unused locale, a second architecture
//     slice, a language folder for a language you don't speak — invalidates
//     the signature, and Gatekeeper then refuses to launch the app, usually
//     with the "application is damaged and should be moved to the Trash"
//     dialog. The only fix is a full reinstall. Those entries exist purely
//     to talk people out of the "just delete the locales you don't use"
//     advice that circulates for Chromium apps; every one of them is Risky.
//  2. A whole .app is the user's own call to make in Finder. Dragging an
//     app to the Trash is a decision about software you chose to install,
//     often needs an admin password for /Applications, and frequently
//     strands far more data outside the bundle than it reclaims inside it.
//     A cleanup tool has no business doing that silently, so those entries
//     rate Caution at worst and just tell you the size, what the app is,
//     and where its data lives once the bundle is gone.
//
// Apple's own apps are absent by design: the scanner drops anything whose
// bundle identifier starts with com.apple. before it ever gets here.
//
// Container is deliberately not set on anything in this file. The flag's
// UI copy is written for JetBrains' per-version cache folders ("holds
// several version caches"), which would read as nonsense next to Slack.app.
var applicationsDB = map[string]Entry{
	// ---------------------------------------------------------------
	// Generic folders found inside .app bundles. All Risky, all
	// command-free: the entire point of these is the signature warning.
	// ---------------------------------------------------------------
	"Contents": {
		Score: Risky,
		Description: "The single top-level folder every macOS .app bundle contains. An .app is not really a file — " +
			"it's an ordinary directory that Finder draws as one icon, and Contents holds the whole thing: the " +
			"executable in MacOS/, the assets in Resources/, the bundled libraries in Frameworks/, the Info.plist " +
			"that tells macOS how to launch it, and the _CodeSignature folder that certifies all of it is unmodified.",
		Effects: "Nothing in here can be safely removed. Every file under Contents is covered by the app's code " +
			"signature, so deleting even one of them invalidates the signature; Gatekeeper then refuses to launch the " +
			"app, typically with the \"application is damaged and should be moved to the Trash\" alert, and the only " +
			"fix is reinstalling it. If you want the space back, remove the whole .app in Finder instead — look at " +
			"the entry for the bundle itself to see what data that leaves behind.",
	},
	"MacOS": {
		Score: Risky,
		Description: "The bundle's actual program: the compiled executable macOS runs when you open the app, plus " +
			"any command-line helpers shipped alongside it. It is usually small for Electron apps (a thin launcher " +
			"that boots the bundled Chromium) and enormous for natively compiled ones — Warp's single binary here is " +
			"395MB and Teleport's tsh is 229MB, which is most of what those bundles weigh.",
		Effects: "This is the program itself, so removing anything here doesn't just break the signature, it " +
			"deletes the app. Even trimming it — stripping an architecture slice out of a universal binary with " +
			"`lipo` is the usual suggestion — rewrites a signed file and leaves the bundle unlaunchable until " +
			"reinstalled. There is nothing disposable in this folder.",
	},
	"Resources": {
		Score: Risky,
		Description: "Everything the app displays but doesn't execute: icons, images, sounds, nib/storyboard UI " +
			"files, and the dozens of per-language xx.lproj folders holding its translated strings. In Electron apps " +
			"it also holds app.asar — the archive containing the app's own JavaScript — which is why VS Code keeps " +
			"673MB and Claude 308MB in here.",
		Effects: "The classic \"reclaim space\" tip for this folder is deleting the xx.lproj folders for languages " +
			"you don't read. Do not: they are signed like everything else, and removing them breaks the bundle so " +
			"macOS won't open it. That applies equally to unused icons and sample assets. The only safe way to " +
			"reclaim this space is to uninstall the whole app.",
	},
	"Frameworks": {
		Score: Risky,
		Description: "Private libraries the app ships and loads at runtime rather than borrowing from macOS. For " +
			"Electron and Chromium apps this is where the entire browser engine sits, so it dominates the bundle: " +
			"464MB in Slack, 482MB in Claude, 463MB in VS Code, 1.4GB in Chrome. Native apps use it more modestly — " +
			"Zoom keeps its media/speech-recognition bundles here, OrbStack its Sparkle updater and Sentry.",
		Effects: "These are the app's own dependencies, not shared system libraries, so nothing else on the " +
			"machine can supply them if you delete one — and they're signed, so removing anything makes Gatekeeper " +
			"block the app entirely. The bundled Chromium runtime in an Electron app is not optional and not shared " +
			"between apps: that duplication is the price of the format, and the only way out of it is uninstalling " +
			"the app.",
	},
	"PlugIns": {
		Score: Risky,
		Description: "Bundled app extensions (.appex) — separate mini-programs the app registers with macOS so " +
			"they can run outside it. This is where Safari web extensions live inside their host app, and also where " +
			"share sheets, widgets and Finder extensions ship: Telegram's share extension alone is 98MB, and every " +
			"Safari-extension app here (Tampermonkey, prune, Super Agent, Usercentrics, JSONformatter) keeps its real " +
			"payload in this folder.",
		Effects: "Deleting an extension from here doesn't cleanly \"uninstall\" it — it corrupts the host app's " +
			"signature, so macOS refuses to launch the host and the extension stops working alongside it. To turn an " +
			"extension off, use Safari Settings → Extensions (or System Settings → Extensions) and untick it; to " +
			"remove it for good, delete the whole host .app in Finder.",
	},
	"XPCServices": {
		Score: Risky,
		Description: "Small helper processes the app launches out-of-process through XPC, macOS's sandboxed " +
			"inter-process mechanism. Apps use them to isolate risky or crash-prone work — parsing untrusted files, " +
			"rendering previews, talking to the network — so a failure takes down the helper instead of the app.",
		Effects: "Each service is a signed sub-bundle of its parent. Removing one breaks the parent's signature " +
			"and blocks it from launching, and even if it did launch, the feature that service backs would fail as " +
			"soon as it was used. Nothing here is cache; leave it alone and uninstall the whole app if you want the " +
			"space.",
	},
	"SharedSupport": {
		Score: Risky,
		Description: "A catch-all for auxiliary files the app ships that aren't UI resources: bundled command-line " +
			"tools, documentation, templates, sample data. FileZilla uses it for its translations (a locales " +
			"subfolder) and its helper binaries, keeping them out of the main Resources tree.",
		Effects: "Signed like the rest of the bundle, so deleting anything here — even documentation that clearly " +
			"isn't executed — invalidates the signature and stops the app from launching until you reinstall it. " +
			"Whatever features depend on the removed tool or template would break too.",
	},
	"Helpers": {
		Score: Risky,
		Description: "Extra executables the main app spawns on demand. Claude keeps its native-messaging host, its " +
			"permission-fixer and an iOS simulator helper here; Warp ships its pprof profiling helper. Unlike PlugIns " +
			"these aren't macOS-registered extensions, just private tools the app shells out to.",
		Effects: "Removing a helper breaks the code signature and Gatekeeper stops the app from launching. Even " +
			"setting the signature aside, whichever feature calls that helper — browser integration, a permissions " +
			"repair flow, profiling — would fail. Nothing here is regenerated; it only comes back on reinstall.",
	},
	"Library": {
		Score: Risky,
		Description: "Where a bundle stages things macOS itself is meant to pick up: LoginItems (the helper that " +
			"makes an app launch at startup, used by Stats and Shottr), LaunchServices, LaunchDaemons (Teleport's tsh " +
			"registers its VNet daemon from here), and Chrome's and Slack's update helpers. Setapp keeps 233MB of " +
			"LaunchServices content in this folder.",
		Effects: "Deleting anything here breaks the bundle's signature and blocks the app from launching. It also " +
			"tends to break the OS-level integration the folder exists for: the app stops starting at login, its " +
			"background daemon fails to register, or its auto-updater stops working. To stop an app launching at " +
			"login, use System Settings → General → Login Items instead.",
	},
	"Extensions": {
		Score: Risky,
		Description: "A bundle-private extensions folder, used by apps that ship system integrations outside the " +
			"standard PlugIns layout — Setapp keeps its Finder and agent extensions here.",
		Effects: "Same rule as everywhere else inside a bundle: these files are signed, and removing one leaves " +
			"the app unlaunchable until it's reinstalled. Disable the extension through System Settings → Extensions " +
			"if you don't want it running.",
	},
	"_CodeSignature": {
		Score: Risky,
		Description: "The bundle's signature itself — a CodeResources file listing a cryptographic hash for every " +
			"single file in the bundle, plus the developer's signature over that list. macOS checks it on launch, " +
			"which is exactly how it detects that a file elsewhere in the bundle has been modified or deleted.",
		Effects: "This is the enforcement mechanism, not a workaround for it: deleting _CodeSignature makes the " +
			"app unsigned rather than unchecked, and Gatekeeper blocks unsigned apps outright. Nothing in the folder " +
			"is cache, it's a few dozen KB at most, and there is no version of removing it that ends with a working " +
			"app.",
	},
	"_MASReceipt": {
		Score: Risky,
		Description: "The Mac App Store purchase receipt — a signed proof that this copy was bought or downloaded " +
			"with your Apple Account. Present on every App Store install, which on this machine covers Slack, " +
			"Telegram, Stats, Super Agent, Usercentrics, Tampermonkey and the other Safari-extension apps. It's a few " +
			"KB.",
		Effects: "Apps validate this receipt at launch and many quit immediately (or bounce you to the App Store " +
			"to \"repurchase\") when it's missing, on top of the signature check failing and Gatekeeper blocking the " +
			"launch anyway. Deleting it saves kilobytes and costs you the app. If a receipt really is corrupt, " +
			"redownloading the app from the App Store replaces it.",
	},
	"Versions": {
		Score: Risky,
		Description: "The versioned layout inside a .framework: each subfolder is one complete copy of the " +
			"framework, with a Current symlink pointing at the live one. Chrome uses it during self-update — right " +
			"now Google Chrome Framework.framework holds both 151.0.7922.138 and 151.0.7922.173 at roughly 700MB " +
			"each, because the newly downloaded version sits beside the running one until Chrome is restarted.",
		Effects: "It is genuinely tempting to delete the old version folder and halve Chrome's size, and it is " +
			"genuinely a mistake: the framework is signed as a whole, so the app stops launching. Restarting Chrome " +
			"is the supported way to collapse this — it switches to the new version and cleans up the old one itself, " +
			"no manual deletion needed.",
	},
	"locales": {
		Score: Risky,
		Description: "Per-language translation data — .pak files for Chromium-based apps, message catalogues for " +
			"others (FileZilla keeps its translations in Contents/SharedSupport/locales). One folder holds every " +
			"language the app ships, and the app reads only the one matching your system language.",
		Effects: "Deleting the languages you don't speak is the single most common \"free up space in an app " +
			"bundle\" tip on the internet, and it breaks the app: the files are covered by the code signature, so " +
			"macOS refuses to launch the bundle afterwards and tells you it's damaged. The payoff would have been a " +
			"few tens of MB; the cost is a full reinstall.",
	},
	"app": {
		Score: Risky,
		Description: "An Electron app's own source, shipped unpacked instead of inside an app.asar archive. VS " +
			"Code does this and it's the single largest thing in the bundle at 673MB — the editor's JavaScript, its " +
			"built-in language support, and the extensions Microsoft bundles by default all live in here rather than " +
			"in the Chromium runtime next door.",
		Effects: "This is the application's actual code, so removing any of it both breaks the signature and guts " +
			"the app. Note that your own installed extensions are not here — VS Code keeps those in " +
			"~/.vscode/extensions (484MB on this machine), which is outside the bundle and manageable from the " +
			"Extensions view in the editor.",
	},
	"app.asar.unpacked": {
		Score: Risky,
		Description: "The files an Electron app had to leave outside its app.asar archive, because native modules " +
			"(.node binaries) and anything spawned as a subprocess can't be loaded from inside an archive. Claude " +
			"keeps 82MB here next to its 36MB app.asar.",
		Effects: "These are the app's native dependencies — the parts most likely to be loaded early during " +
			"startup — so removing any of them breaks the app immediately, in addition to invalidating the bundle " +
			"signature and triggering Gatekeeper. Nothing in this folder is a cache and none of it is re-downloaded.",
	},
	"Electron Framework.framework": {
		Score: Risky,
		Description: "A complete, private copy of Chromium plus Node.js — the runtime every Electron app embeds so " +
			"it doesn't depend on any browser being installed. It's the reason Electron apps all weigh several " +
			"hundred MB regardless of how small their own code is: 464MB in Slack, 482MB in Claude's Frameworks " +
			"folder, 274MB in WootonPad, and each app ships its own copy pinned to its own Electron version.",
		Effects: "Nothing here is shared, optional, or reclaimable. Apps pin specific Electron versions, so the " +
			"copies can't be deduplicated even in principle, and deleting any part of one (including the pile of " +
			"xx.lproj folders and .pak files in its Resources) breaks the signature and stops the app launching. The " +
			"only way to reclaim these hundreds of MB is to uninstall the app itself.",
	},
	"Google Chrome Framework.framework": {
		Score: Risky,
		Description: "Chrome's browser engine: Blink, V8, the sandbox, the media stack and the helper processes, " +
			"all bundled as one versioned framework. It accounts for essentially the entire 1.4GB of Chrome.app, and " +
			"right now it holds two versions side by side (151.0.7922.138 and .173, ~700MB each) because Chrome has " +
			"downloaded an update it hasn't restarted into yet.",
		Effects: "Deleting the superseded version folder to halve Chrome's size does not work — the framework is " +
			"signed as a unit and Chrome won't launch afterwards. Quit and reopen Chrome instead: it activates the " +
			"new version and removes the old one on its own, which is the supported way to get that ~700MB back. None " +
			"of your browsing data is in here; that's in ~/Library/Application Support/Google/Chrome.",
	},
	"Squirrel.framework": {
		Score: Risky,
		Description: "The auto-updater used by most Electron apps on macOS (Claude, VS Code and WootonPad all ship " +
			"it, usually alongside Mantle.framework and ReactiveObjC.framework, which it depends on). It downloads " +
			"new versions in the background and swaps them in on the next launch.",
		Effects: "Deleting it to stop an app updating itself doesn't work — it breaks the signature, so the app " +
			"stops launching at all. It's also small, a few hundred KB. If you want to control updates, use the app's " +
			"own preferences. Its scratch/staging files live in ~/Library/Caches under a *.ShipIt name, outside the " +
			"bundle, and those genuinely are disposable.",
	},
	"Sparkle.framework": {
		Score: Risky,
		Description: "The other widely used third-party updater for Mac apps — the one non-App-Store native apps " +
			"typically use to check for, download and install their own updates. OrbStack ships it here alongside " +
			"Sentry.framework for crash reporting.",
		Effects: "Signed like everything else in the bundle, so removing it leaves the app unlaunchable rather " +
			"than just un-updatable, and it's only a couple of MB. Turn off automatic updates in the app's own " +
			"settings if that's the goal.",
	},

	// JetBrains lays its IDE bundles out like a Linux install tree rather
	// than a typical Mac app, so drilling into GoLand.app or PhpStorm.app
	// turns up these lowercase folders instead of Frameworks/PlugIns. They
	// are still inside a signed bundle and still untouchable.
	"jbr": {
		Score: Risky,
		Description: "The JetBrains Runtime — a full private JDK (a patched OpenJDK build with JetBrains' own font " +
			"rendering and HiDPI fixes) that every JetBrains IDE bundles so it never depends on whatever Java you do " +
			"or don't have installed. It's 173MB inside GoLand.app.",
		Effects: "This is the JVM the IDE runs on; deleting it means the IDE cannot start, and it invalidates the " +
			"bundle signature on the way. Pointing the IDE at a system JDK to save the space is not supported and " +
			"breaks rendering. Reclaim it by uninstalling the IDE from JetBrains Toolbox instead.",
	},
	"plugins": {
		Score: Risky,
		Description: "The plugins bundled with a JetBrains IDE — the biggest single item in the bundle at 2.3GB in " +
			"GoLand.app. Most of it is language and framework support that ships enabled by default (databases, " +
			"version control, web technologies, the AI assistant), not anything you installed yourself.",
		Effects: "Deleting unused bundled plugins looks like the obvious win in a 3.3GB bundle and isn't one: they " +
			"are signed with the rest of the app, so the IDE stops launching. Disabling a plugin in Settings → " +
			"Plugins is the supported way to switch one off, though that only saves memory and startup time, not " +
			"disk. Plugins you installed yourself live under ~/Library/Application Support/JetBrains, not here.",
	},
	"lib": {
		Score: Risky,
		Description: "The IDE's own compiled code — the JAR files that are IntelliJ Platform and the product built " +
			"on top of it. 918MB in GoLand.app, second only to the bundled plugins.",
		Effects: "This is the application itself, so there is nothing here to trim: removing a JAR breaks the " +
			"signature and, signature aside, the IDE simply won't start. Uninstalling the IDE through JetBrains " +
			"Toolbox is the only way to get this space back.",
	},
	"bin": {
		Score: Risky,
		Description: "Launcher scripts and small native helpers for a JetBrains IDE: the startup script, the VM " +
			"options files, the fsnotifier file-watcher, restarter and printenv helpers. A couple of MB in total.",
		Effects: "Small, entirely non-disposable, and signed — deleting anything here breaks both the launcher and " +
			"the bundle signature. If you're editing VM options, do it through Help → Edit Custom VM Options, which " +
			"writes to your config directory outside the bundle instead of modifying these files.",
	},
	"modules": {
		Score: Risky,
		Description: "Java module descriptors and module-path artifacts used by the JetBrains Runtime when it " +
			"boots the IDE. Under a megabyte.",
		Effects: "Nothing reclaimable — it's under a megabyte, the IDE needs it at startup, and deleting it breaks " +
			"the bundle signature like any other in-bundle file.",
	},
	"license": {
		Score: Risky,
		Description: "The open-source license texts for the third-party libraries a JetBrains IDE bundles, which " +
			"the IDE is legally required to ship. Not your JetBrains license or account — that's stored under " +
			"~/Library/Application Support/JetBrains, outside the bundle entirely.",
		Effects: "Half a megabyte of text files that still count toward the code signature, so deleting them stops " +
			"the IDE from launching. Nothing here affects your subscription or activation either way.",
	},
	"licenses": {
		Score: Risky,
		Description: "The same idea in JetBrains Toolbox's bundle: bundled third-party license texts, about 1.2MB. " +
			"Not related to your JetBrains subscription or any IDE activation.",
		Effects: "Signed along with the rest of Toolbox, so removing these text files makes the app refuse to " +
			"launch, in exchange for about a megabyte. Leave them.",
	},
	"jre": {
		Score: Risky,
		Description: "JetBrains Toolbox's own bundled Java runtime, 54MB, separate from the jbr folder inside each " +
			"IDE it installs. Toolbox is itself a JVM app and ships the runtime it needs.",
		Effects: "Toolbox cannot start without it, and deleting it breaks the bundle signature. Note this is not " +
			"shared with the IDEs — each IDE carries its own JetBrains Runtime — so removing it saves 54MB and costs " +
			"you Toolbox.",
	},
	"jetbrainsd": {
		Score: Risky,
		Description: "The background service half of JetBrains Toolbox (28MB): the daemon that checks for IDE " +
			"updates, handles the toolbox:// links used by JetBrains' web integrations, and keeps installed IDEs in " +
			"sync with your account.",
		Effects: "Deleting it breaks Toolbox's signature so the app won't launch, and even if it did, automatic " +
			"update checks and one-click clone/open links would stop working. If you just don't want the daemon " +
			"running at startup, turn that off in Toolbox's own settings.",
	},

	// ---------------------------------------------------------------
	// Whole .app bundles. Descriptive only: what it is, why it's the
	// size it is, and what stays behind if you drag it to the Trash.
	// ---------------------------------------------------------------
	"Google Chrome.app": {
		Score: Caution,
		Description: "Google's browser, and the largest app on this machine at 1.4GB — effectively all of it the " +
			"Chromium engine in Contents/Frameworks. It self-updates in the background, and the bundle currently " +
			"carries two full copies of that engine (~700MB each) because an update has been downloaded but not " +
			"restarted into. Quitting and reopening Chrome collapses that back to one.",
		Effects: "Deleting the app leaves all of your Chrome data on disk: ~/Library/Application " +
			"Support/Google/Chrome is 4.4GB here and holds your profiles, history, cookies, saved passwords and " +
			"extensions, with more in ~/Library/Caches/Google. That's deliberate — reinstalling Chrome picks the " +
			"profile straight back up — but it means removing the app alone reclaims far less than you'd expect. " +
			"Chrome also leaves the Google Software Update helper behind to keep other Google apps current.",
	},
	"Visual Studio Code.app": {
		Score: Caution,
		Description: "Microsoft's editor, 1.1GB, built on Electron. It splits almost evenly between the bundled " +
			"Chromium runtime in Frameworks (463MB) and the editor's own code in Resources/app (673MB), the latter " +
			"including all the language support and extensions Microsoft ships by default. It updates itself through " +
			"the Squirrel framework it bundles.",
		Effects: "Removing the app leaves the parts you actually accumulated: ~/.vscode/extensions (484MB here) " +
			"for everything you installed from the marketplace, and ~/Library/Application Support/Code (674MB) for " +
			"your settings, keybindings, per-workspace state and the extension host's storage. Reinstalling picks all " +
			"of it back up. If it's disk you're after, pruning unused extensions from inside VS Code frees more than " +
			"deleting the app does.",
	},
	"Claude.app": {
		Score: Caution,
		Description: "Anthropic's Claude desktop app, 801MB, an Electron app. 482MB of that is the bundled " +
			"Chromium runtime; the remaining 308MB in Resources is the app's own code plus some sizeable extras — a " +
			"138MB ion-dist folder and a pair of ~23MB smol-bin disk images, one per architecture. Contents/Helpers " +
			"also ships a native-messaging host for browser integration.",
		Effects: "Deleting the app leaves ~/Library/Application Support/Claude behind, which on this machine is " +
			"9.8GB — more than twelve times the size of the bundle itself, and by far the bigger prize if you're " +
			"reclaiming space. There's also ~/Library/Caches/com.anthropic.claudefordesktop. Your conversation " +
			"history is tied to your account server-side, so removing the app doesn't lose it, but it also means " +
			"deleting the bundle on its own barely moves the needle.",
	},
	"OrbStack.app": {
		Score: Risky,
		Description: "A fast, low-overhead replacement for Docker Desktop that runs Linux containers and full " +
			"Linux VMs on macOS. 713MB, most of it Contents/Resources/assets (440MB) — the Linux kernel and guest " +
			"images it boots machines from — plus a 176MB native binary. It ships under the bundle ID " +
			"dev.kdrag0n.MacVirt, which is why its cache folders don't say \"orbstack\".",
		Effects: "The bundle is the small half of OrbStack's footprint: ~/Library/Group " +
			"Containers/HUAQ24HBR6.dev.orbstack is 13GB on this machine and holds the actual VM disk with every " +
			"image, container and volume you've built. Deleting the app strands that data rather than freeing it, and " +
			"leaves you without the tool needed to open it. Stop your containers and use OrbStack's own uninstall " +
			"(Settings, or `orb delete all` first) if you really want it gone.",
	},
	"Slack.app": {
		Score: Caution,
		Description: "Slack's desktop client, 497MB, installed from the Mac App Store (hence the _MASReceipt in " +
			"the bundle). It's an Electron app, and 464MB of that total is the bundled Chromium runtime — the Slack " +
			"code itself is about 32MB. Because it's a sandboxed App Store build, its data lives under Containers " +
			"rather than loose in Application Support.",
		Effects: "Deleting the app leaves ~/Library/Containers/com.tinyspeck.slackmacgap behind, which holds your " +
			"logged-in workspaces, local message cache and downloaded files, along with a Group Containers folder " +
			"shared with its extensions. You'd need to sign back into every workspace after reinstalling if you clear " +
			"that too. Nothing in the bundle itself is trimmable — the Chromium runtime is the whole size.",
	},
	"Warp.app": {
		Score: Caution,
		Description: "A modern terminal emulator, and a useful contrast to the Electron apps around it: at 451MB " +
			"it's a similar size, but it's natively compiled — 395MB of it is one Rust binary at " +
			"Contents/MacOS/stable, with no browser engine involved. It also ships a dock tile plugin and a pprof " +
			"profiling helper.",
		Effects: "Removing the app leaves its state behind in several places: ~/Library/Application " +
			"Support/dev.warp.Warp-Stable (617MB here, including block history and settings), " +
			"~/Library/Caches/dev.warp.Warp-Stable, ~/Library/Group Containers/2BBY89MBSN.dev.warp, and ~/.warp. Your " +
			"actual shell history is your shell's (~/.zsh_history) and is unaffected either way.",
	},
	"zoom.us.app": {
		Score: Caution,
		Description: "The Zoom meeting client, 450MB, of which 419MB is Contents/Frameworks — and unusually for an " +
			"app this size, that's not a browser engine but Zoom's own media stack: video codecs, an audio processing " +
			"bundle, an on-device speech-recognition framework, a SIP SDK, and separate bundles for its calendar and " +
			"mail features.",
		Effects: "Deleting the app leaves ~/Library/Application Support/zoom.us (settings, virtual backgrounds), " +
			"~/Library/Caches/us.zoom.xos and a Group Containers folder for its calendar/contacts integration. Worth " +
			"checking ~/Documents/Zoom separately before or after — that's where local meeting recordings go, it " +
			"isn't touched by removing the app, and it's usually the largest Zoom-related folder on a machine.",
	},
	"Setapp.app": {
		Score: Caution,
		Description: "MacPaw's Setapp client — a subscription service that installs and updates apps from its own " +
			"catalogue rather than the App Store. 329MB, most of it (233MB) in Contents/Library/LaunchServices, plus " +
			"a Finder extension and a background agent. It installs the apps you subscribe to into " +
			"/Applications/Setapp/ as ordinary bundles.",
		Effects: "Deleting the client does not remove the apps it installed — those stay in /Applications/Setapp/ " +
			"and simply stop updating and, for most of them, stop validating your subscription. ~/Library/Application " +
			"Support/Setapp (450MB here) and each installed app's own support folders stay too. Setapp ships a proper " +
			"uninstaller in its own menu; using that removes the agent and extensions cleanly, which dragging the " +
			"bundle to the Trash does not.",
	},
	"Telegram.app": {
		Score: Caution,
		Description: "The official Telegram desktop client for macOS, 310MB, a native Swift/Objective-C app from " +
			"the Mac App Store rather than an Electron wrapper. Its 185MB executable and a surprisingly large 98MB " +
			"share extension make up almost all of it.",
		Effects: "Because it's a sandboxed App Store build, everything you'd care about is in " +
			"~/Library/Containers/ru.keepcoder.Telegram (138MB here) and the matching Group Containers folder: your " +
			"session, local message cache, and downloaded media. Deleting the app leaves that intact, so a reinstall " +
			"picks up where you left off — but it also means removing the bundle alone frees only part of Telegram's " +
			"footprint. Your message history itself lives on Telegram's servers.",
	},
	"WootonPad.app": {
		Score: Caution,
		Description: "A locally installed Electron app (bundle ID ai.doctly.switchboard) that runs scheduled " +
			"tasks. 291MB, of which 274MB is the bundled Chromium runtime — its own code, in Resources/app.asar, is a " +
			"small fraction of that. It ships Squirrel and an app-update.yml, so it updates itself in place.",
		Effects: "As with any Electron app, the bundle size is the runtime and can't be reduced. Deleting the app " +
			"leaves whatever schedules and configuration it has written outside the bundle in place, and stops any " +
			"scheduled tasks it runs from firing. If you rely on those tasks, export or note them before removing the " +
			"app.",
	},
	"OpenVPN Connect.app": {
		Score: Caution,
		Description: "OpenVPN's official VPN client. Note the layout: /Applications/OpenVPN Connect.app is a " +
			"symlink into /Applications/OpenVPN Connect/, where the real 288MB bundle lives — 252MB of it bundled Qt " +
			"frameworks for the UI. It also installs a privileged helper so it can configure network routes.",
		Effects: "Deleting the bundle leaves ~/Library/Application Support/OpenVPN Connect behind, which holds the " +
			"connection profiles you imported and their saved settings — losing those means re-importing .ovpn files " +
			"from whoever issued them. It also leaves the privileged helper and its launch daemon installed; OpenVPN " +
			"ships an uninstaller precisely because dragging the app to the Trash doesn't remove those.",
	},
	"tsh.app": {
		Score: Caution,
		Description: "Teleport's `tsh` client — a command-line tool for logging into Teleport-protected SSH " +
			"servers, Kubernetes clusters and databases, shipped as a .app bundle rather than a bare binary. The " +
			"bundle exists so macOS will register its VNet launch daemon (in Contents/Library/LaunchDaemons); the " +
			"actual content is a single 229MB Go binary at Contents/MacOS/tsh.",
		Effects: "You almost certainly invoke this as `tsh` from a terminal, not by double-clicking it, so it's " +
			"easy to mistake for a stray app and delete the CLI you depend on. Removing it also unregisters the VNet " +
			"daemon. Your Teleport session certificates are in ~/.tsh, outside the bundle, and stay behind — they're " +
			"only a few dozen KB and expire on their own.",
	},
	"tctl.app": {
		Score: Caution,
		Description: "Teleport's `tctl` admin CLI, packaged as an app bundle for the same reason tsh.app is: a " +
			"single 195MB Go binary in Contents/MacOS. It's the cluster-administration counterpart to tsh — managing " +
			"users, roles, tokens and cluster resources — so it's only useful if you administer a Teleport cluster " +
			"rather than merely connect to one.",
		Effects: "Nothing inside is removable, and the binary is the whole bundle. If you only ever use `tsh` to " +
			"connect to things, this is the one Teleport bundle you might genuinely not need — but check with whoever " +
			"manages your cluster first, and reinstall it from the same Teleport package if you're unsure. It stores " +
			"no state of its own; it reads the credentials tsh writes to ~/.tsh.",
	},
	"JetBrains Toolbox.app": {
		Score: Risky,
		Description: "JetBrains' installer and updater for their IDEs. At 199MB the app itself is modest — a " +
			"bundled Java runtime, its own libraries, and the jetbrainsd background daemon — but it is the manager " +
			"for a much larger footprint: it installs IDEs into ~/Applications (GoLand and PhpStorm here, 6.2GB " +
			"between them) and their configuration and caches into ~/Library/Application Support/JetBrains (12GB) and " +
			"~/Library/Caches/JetBrains.",
		Effects: "Deleting Toolbox does not uninstall the IDEs it manages — they keep working but stop updating, " +
			"and you lose the only interface that can cleanly remove them or roll back a version. If you want to " +
			"reclaim IDE space, do it from inside Toolbox (uninstall an IDE, or clear old versions) rather than by " +
			"deleting bundles by hand, which leaves Toolbox's records pointing at installs that no longer exist.",
	},
	"FileZilla.app": {
		Score: Caution,
		Description: "The open-source FTP/FTPS/SFTP client, 47MB — a wxWidgets application, so the bulk is its " +
			"bundled UI frameworks plus helper binaries and translations under Contents/SharedSupport. It's " +
			"sandboxed, and also registers a File Provider extension so remote sites can be mounted in Finder.",
		Effects: "Deleting the app leaves its sandbox containers behind (org.filezilla-project.filezilla.sandbox " +
			"and the org.filezilla-project.remotedrive family), and those hold the Site Manager: every saved host, " +
			"username, and — depending on your settings — saved passwords, along with your transfer queue. Export " +
			"your sites from File → Export before removing anything if you want them back later.",
	},
	"Super Agent.app": {
		Score: Caution,
		Description: "A Safari extension host app. Safari extensions on macOS can't be installed on their own — " +
			"each one has to be shipped inside a regular .app that registers it with the system, which is why an app " +
			"you never open appears in /Applications. This one carries Super Agent, which answers website " +
			"cookie-consent banners automatically according to preferences you set once. 43MB, built with Mac " +
			"Catalyst (hence the Catalyst.bundle), with the extension itself in Contents/PlugIns.",
		Effects: "The host app is the extension's delivery mechanism, so deleting it uninstalls the extension from " +
			"Safari and cookie banners start appearing normally again. If you just want it off for now, untick it in " +
			"Safari → Settings → Extensions and leave the app in place. Its settings live in " +
			"~/Library/Containers/com.agent.super.extension.safari and stay behind either way.",
	},
	"Stats.app": {
		Score: Caution,
		Description: "An open-source menu-bar system monitor (CPU, memory, disk, network, battery, sensors) by " +
			"Serhiy Mytrovtsiy. 36MB, including a widgets extension for Notification Centre and a login-item helper " +
			"in Contents/Library so it can start with the system.",
		Effects: "Deleting the app leaves ~/Library/Application Support/Stats (11MB of settings and collected " +
			"history) and its widget container behind. Because it registers a login item, dragging it to the Trash " +
			"while it's running can leave a stale entry in System Settings → General → Login Items — quit Stats " +
			"first. Nothing it monitors is affected; it only reads system metrics.",
	},
	"Usercentrics.app": {
		Score: Caution,
		Description: "Another Safari extension host app, this one from Usercentrics — a consent-management company " +
			"— shipping their Data Shield extension, which handles cookie-consent banners on your behalf according to " +
			"a privacy level you choose. 31MB, with the extension in Contents/PlugIns. You never launch it directly; " +
			"it exists so Safari has something to install the extension from.",
		Effects: "Removing the app removes the extension from Safari, and consent banners go back to appearing " +
			"manually. Toggling it off in Safari → Settings → Extensions has the same day-to-day effect without " +
			"uninstalling anything. Its containers (com.usercentrics.consentagent and the matching extension " +
			"container, plus a Group Container) stay on disk after the app is deleted.",
	},
	"Hand Mirror.app": {
		Score: Caution,
		Description: "A menu-bar utility by Rafael Conde that puts a one-click camera preview in the menu bar, so " +
			"you can check your framing before joining a call without opening Photo Booth or the meeting app itself. " +
			"27MB, App Store distributed, with a paid \"Plus\" tier unlocking extra features.",
		Effects: "Deleting the app removes the menu-bar item and its container in " +
			"~/Library/Containers/net.rafaelconde.Hand-Mirror stays behind with its preferences. It's small and " +
			"self-contained, so there's little to reclaim either way. If you bought Hand Mirror Plus, that purchase " +
			"is tied to your Apple Account and can be restored after reinstalling.",
	},
	"WiFi Explorer.app": {
		Score: Caution,
		Description: "A Wi-Fi scanner and analyser by Adrian Granados / Intuitibits: it lists nearby networks with " +
			"their SSID, BSSID, vendor, channel, band, security and signal strength, which is how you find out that " +
			"your neighbours are all sitting on the same 2.4GHz channel you are. 8.3MB, a compact native app.",
		Effects: "Small enough that removing it reclaims almost nothing. Its preferences and any saved scans live " +
			"in ~/Library/Containers/wifiexplorer and a Group Container shared with Intuitibits' other tools, both of " +
			"which stay behind. Note this is the standard edition, distinct from the more expensive WiFi Explorer " +
			"Pro.",
	},
	"Shottr.app": {
		Score: Caution,
		Description: "A screenshot tool aimed at designers and front-end developers: scrolling full-page captures, " +
			"OCR text recognition, a pixel ruler and colour picker, annotation and pixelation. Notably tiny at 6.6MB " +
			"— a native Apple Silicon app with no bundled runtime, which is the entire reason it starts and captures " +
			"faster than macOS's built-in screenshot UI.",
		Effects: "There is essentially nothing to reclaim here. Deleting it leaves " +
			"~/Library/Containers/cc.ffitch.shottr with your preferences and pinned screenshots, and, since it " +
			"installs a login item, it's worth quitting the app before removing it so the entry in System Settings → " +
			"Login Items goes away cleanly. Screenshots you already saved to disk are unaffected.",
	},
	"No Duplicate Tabs.app": {
		Score: Caution,
		Description: "A Safari extension host app (from tentativeknowledge.com) whose extension watches for tabs " +
			"pointing at the same URL and closes the duplicates. 4.8MB, App Store distributed, with the actual " +
			"extension in Contents/PlugIns — the app itself does nothing when opened beyond explaining how to enable " +
			"it in Safari.",
		Effects: "Deleting the app uninstalls the extension from Safari and duplicate tabs stop being closed " +
			"automatically. Disabling it in Safari → Settings → Extensions achieves the same thing reversibly. Its " +
			"container in ~/Library/Containers/com.tentativeknowledge.No-Duplicate-Tabs remains after deletion and is " +
			"negligible in size.",
	},
	"prune.app": {
		Score: Caution,
		Description: "A Safari extension host app for `prune`, an open-source tab manager that closes stale tabs " +
			"and focuses an existing tab instead of opening a duplicate. 4.5MB. Its Info.plist records " +
			"SFSafariWebExtensionConverterVersion, meaning it's a cross-browser WebExtension repackaged for Safari " +
			"with Apple's converter — the same code that ships as a Chrome or Firefox add-on, wrapped in an app.",
		Effects: "Removing the app removes the extension from Safari; your tabs are unaffected but nothing will " +
			"prune them any more. Toggling it off in Safari → Settings → Extensions is the reversible option. Its " +
			"containers (com.github.prune and .Extension) hold the extension's settings and stay behind.",
	},
	"JSONformatter.app": {
		Score: Caution,
		Description: "A Safari extension host app for JSON Formatter, which turns a raw JSON response into a " +
			"collapsible, syntax-highlighted tree when you open one in a browser tab. 3.8MB, App Store distributed. " +
			"Like every Safari extension it has to ship inside an app, which is why it sits in /Applications despite " +
			"having no real interface of its own.",
		Effects: "Deleting it removes the extension, and JSON URLs go back to rendering as a wall of unformatted " +
			"text in Safari. Disabling it in Safari → Settings → Extensions does the same reversibly. Its containers " +
			"stay on disk and are a few hundred KB at most.",
	},
	"Tampermonkey.app": {
		Score: Risky,
		Description: "The Safari host app for Tampermonkey, the userscript manager — it runs custom JavaScript you " +
			"(or someone else) wrote against specific sites. Only 2.9MB, because the interesting part isn't the " +
			"bundle: your installed userscripts and their stored data live in the extension's container, not here.",
		Effects: "Rated more carefully than the other extension hosts because deleting the app takes your " +
			"installed userscripts with it — they're stored in ~/Library/Containers/net.tampermonkey.SafariWebExt and " +
			"its extension container, and any script you wrote yourself or configured by hand is not recoverable from " +
			"the App Store. Export your scripts from Tampermonkey's dashboard first. Disabling the extension in " +
			"Safari → Settings is the safe way to switch it off.",
	},
	"Ultra Wifi.app": {
		Score: Caution,
		Description: "A Wi-Fi analyser and monitor from Z9Apps: signal strength, noise, channel usage across 2.4 " +
			"and 5GHz, plus a speed test. 2.8MB, App Store distributed with in-app purchases. It overlaps almost " +
			"entirely with WiFi Explorer, also installed here — worth knowing if you're deciding which to keep.",
		Effects: "Too small for its removal to reclaim meaningful space. Its container in " +
			"~/Library/Containers/com.z9apps.ultraWifi keeps preferences and stays behind. Any in-app purchase is " +
			"tied to your Apple Account, so reinstalling and restoring purchases gets it back.",
	},

	// ---------------------------------------------------------------
	// ~/Applications. JetBrains Toolbox installs IDEs here rather than
	// into /Applications, which is why the user-level folder is the
	// bigger of the two on this machine.
	// ---------------------------------------------------------------
	"GoLand.app": {
		Score: Risky,
		Description: "JetBrains' Go IDE, installed into ~/Applications by JetBrains Toolbox rather than by you " +
			"dragging it anywhere. At 3.3GB it's the largest single bundle on the machine: 2.3GB of bundled plugins, " +
			"918MB of platform JARs, and a 173MB private Java runtime. The layout inside is a Linux-style tree (bin, " +
			"lib, plugins, jbr) rather than the usual Mac Frameworks/Resources split.",
		Effects: "Delete this through JetBrains Toolbox, not Finder — Toolbox tracks what it installed, and " +
			"removing the bundle by hand leaves it convinced GoLand is still there. Either way your settings, " +
			"keymaps, plugins and license stay in ~/Library/Application Support/JetBrains, and your project indexes " +
			"and Local History stay in ~/Library/Caches/JetBrains (together about 12GB here), so removing the IDE " +
			"reclaims less than the bundle size suggests unless you clean those too.",
	},
	"PhpStorm.app": {
		Score: Risky,
		Description: "JetBrains' PHP IDE, 2.9GB, installed into ~/Applications by JetBrains Toolbox alongside " +
			"GoLand. Same internal layout and the same reason for the size: bundled plugins and platform JARs plus " +
			"its own copy of the JetBrains Runtime — each IDE ships a complete stack, nothing is shared between them.",
		Effects: "Uninstall from JetBrains Toolbox rather than dragging the bundle to the Trash, so Toolbox's " +
			"records stay accurate. Your configuration lives under ~/Library/Application Support/JetBrains and its " +
			"indexes under ~/Library/Caches/JetBrains; both survive removing the app, and the caches for a version " +
			"you no longer have installed are exactly the kind of thing worth cleaning afterwards.",
	},
	"Claude Code URL Handler.app": {
		Score: Caution,
		Description: "A 4KB stub bundle created by Claude Code so macOS has something to register the claude:// " +
			"URL scheme against — clicking a claude:// link (from a browser or an IDE integration) opens this, which " +
			"immediately hands off to the real CLI. It contains a single small executable and an Info.plist, and no " +
			"application of its own.",
		Effects: "Effectively nothing to reclaim — it's 4KB. Deleting it breaks claude:// links until Claude Code " +
			"recreates it, which it does on its own; it does not affect the `claude` command in your terminal, your " +
			"session, or anything under ~/.claude.",
	},
}
