package knowledge

// This file describes Apple's own folders across the three roots they
// dominate. It exists because ~/Library/Containers on a current macOS has
// around 738 entries, essentially all of them Apple's, and without these
// tables the app renders that as a wall of undescribed rows.
//
// Three facts shape everything below.
//
// First, a container's Data subtree is that sandboxed process's entire
// home directory — Data/Documents is its Documents folder, Data/Library is
// its Library. Deleting a container is not clearing a cache, it is
// factory-resetting the process. So the default here is Risky or Caution
// with no clean action at all, and the entries that do offer one name the
// exact cache subpaths and never touch the folder as a whole.
//
// Second, most of these folders are empty. macOS creates a container for
// every sandboxed extension the moment it is registered, whether or not it
// ever writes a byte, so hundreds of the rows are 4KB of
// container-manager metadata wrapped around an empty skeleton home
// directory. The families below — Quick Look's per-file-type thumbnail
// renderers, the PriML/PFL machine-learning host plugins, the Lighthouse
// SELF ingestors — are all of this shape. They are described rather than
// cleaned because there is genuinely nothing in them to reclaim, and the
// point of the entry is to answer "what is this?" not "how do I delete
// it?".
//
// Third, where an Apple folder is actually large, the size is almost
// always in one of three places: a widget extension's archived renderings
// under Data/SystemData/com.apple.chrono, a Foundation URL cache's
// Cache.db-wal, or a container's httpstorages.sqlite-wal. All three are
// SQLite write-ahead logs or derived renderings that macOS never
// checkpoints or evicts on its own, which is why folders holding no real
// data still report several megabytes.

// appleChronoClean is the CleanPaths value shared by every widget
// extension container. chronod, the per-user agent that keeps widgets up
// to date, archives each widget's rendered output here as .chrono-timeline
// files; the widget layout and per-widget configuration live in chronod's
// own group container (~/Library/Group Containers/group.com.apple.chronod)
// and are deliberately out of scope, so a clean can never make a widget
// disappear from Notification Center or lose how it was configured.
var appleChronoClean = []string{`Data/SystemData/com.apple.chrono/*`}

// appleWidgetContainer builds the entry for one widget extension's
// sandbox. There are around twenty of these on a stock system and they are
// structurally identical — only the owning app and the location of that
// app's real data differ — so the prose is generated rather than copied
// twenty times. widget completes "The sandbox of ..."; elsewhere is the
// sentence naming where the owning app's actual data lives, which the
// Effects text needs so it can say what a clean does not touch.
func appleWidgetContainer(id, widget, elsewhere string) Entry {
	return Entry{
		Score: Caution,
		Description: "The sandbox of " + widget + ". Almost nothing in it belongs to the extension itself: the " +
			"bulk sits under Data/SystemData/com.apple.chrono, where chronod — the background agent that keeps " +
			"widgets current — archives the rendered output of every widget size and configuration it has ever " +
			"drawn, as .chrono-timeline files split across placeholders/, snapshots/, timelines/ and relevance/. " +
			"Each rendering is keyed by pixel size and corner radius, so the same widget accumulates a separate " +
			"file per family and per display it has appeared on, and nothing evicts the ones for sizes you no " +
			"longer use.",
		Effects: "Removes the archived widget renderings and nothing else. Which widgets you placed and how you " +
			"configured them is stored in chronod's own group container, not here, so nothing vanishes from " +
			"Notification Center and nothing has to be re-added. " + elsewhere + " chronod re-renders each widget " +
			"on its next timeline refresh — minutes for most, immediately if you open Notification Center — and " +
			"until then a widget may briefly show its placeholder. If one stays blank, force-quitting chronod in " +
			"Activity Monitor makes it re-render at once.",
		Commands: []string{
			`rm -rf ~/Library/Containers/` + id + `/Data/SystemData/com.apple.chrono/*`,
		},
		CleanPaths: appleChronoClean,
	}
}

// appleQuickLookThumbnail builds the entry for one of Quick Look's
// per-file-type thumbnail renderers. macOS ships one sandboxed extension
// per format at /System/Library/ExtensionKit/Extensions/*ThumbnailExtension.appex,
// all on the com.apple.quicklook.thumbnail.secure extension point, and
// gives each its own container. Verified empty on a real machine: the only
// file in any of them is the container-manager metadata plist.
func appleQuickLookThumbnail(kinds string) Entry {
	return Entry{
		Score: Caution,
		Description: "One of the ten sandboxed thumbnail renderers Quick Look ships, this one responsible for " +
			kinds + ". macOS runs each file type's renderer in its own sandbox so a malformed file can only " +
			"crash the extension that parses it, which is why a stock system has a row here for every format " +
			"Finder can preview. The container is empty — the single file in it is the container-manager " +
			"metadata plist — because the rendered thumbnails are not stored per extension: they go into the " +
			"system-wide Quick Look thumbnail cache under /var/folders, which macOS sizes and purges itself.",
		Effects: "No clean action is offered, because there is nothing in the folder to remove. It appears in this " +
			"listing only because macOS creates a container for every sandboxed extension whether or not it ever " +
			"writes to it, and containermanagerd would recreate this one the next time Finder previewed a file " +
			"of this type anyway. If you are after the thumbnail cache itself, `qlmanage -r cache` is the " +
			"supported way to reset it; it lives outside your home folder and this app does not scan it.",
	}
}

// appleMLHostPlugin builds the entry for one worker plugin of macOS's
// on-device machine-learning host. Every one of these declares the
// com.apple.mlhost.worker-high extension point and ships as an .appex
// under /System/Library/ExtensionKit/Extensions — both confirmed by
// reading the bundles on a real machine, which matters because Apple
// publishes no documentation for any of them individually.
func appleMLHostPlugin(bundle, role string) Entry {
	return Entry{
		Score: Caution,
		Description: "The sandbox of " + bundle + ", one of the worker plugins macOS's on-device " +
			"machine-learning host runs. Its bundle is /System/Library/ExtensionKit/Extensions/" + bundle +
			".appex and it declares the com.apple.mlhost.worker-high extension point, so it is launched on " +
			"demand by the ML host rather than by you. " + role + " The container is empty — a " +
			"container-manager metadata plist and the usual skeleton home directory — because the work happens " +
			"in memory and the results go to the framework's own storage, not into the plugin's sandbox.",
		Effects: "No clean action is offered: there is nothing in the folder to reclaim. It is listed so the name " +
			"stops looking like something that shouldn't be on your Mac. If you want the analytics these " +
			"plugins feed switched off, the control is System Settings > Privacy & Security > Analytics & " +
			"Improvements, which disables the work rather than deleting an already-empty folder.",
	}
}

// applePriMLNote is appended to the PriML plugins' descriptions. PFL is
// Private Federated Learning, Apple's published approach to improving
// models from data that never leaves the device; the machine ships
// PriMLETL.framework and SiriMASPFLTraining.framework to support it. The
// individual plugin names are internal codenames Apple has never
// documented, and this note says so rather than inventing a purpose.
const applePriMLNote = "PriML is Apple's internal shorthand for its private machine-learning stack and PFL for " +
	"Private Federated Learning, the published technique for improving a model from on-device data by sending " +
	"back only differentially private aggregate updates, never the data itself. Apple documents the technique " +
	"but not this individual plugin, so what it specifically computes is not publicly known."

// appleSELFIngestorNote is the shared explanation for the SELF ingestor
// family. All of them declare the com.apple.lighthouse.SAOrchestratedExtension
// extension point, and the machine ships a matching set of Lighthouse
// frameworks (CoreMLFeatureStore, CoreMLModelStore, DataProcessor,
// ModelMonitoring). What "SELF" abbreviates is not documented anywhere
// public, so the note describes the observable role and stops there.
const appleSELFIngestorNote = "It is one of a family of \"SELF ingestor\" extensions — the others cover Biome " +
	"(Apple's on-device event-stream store), Siri's GMS, and Intelligence Flow's transcripts and telemetry — " +
	"all of which declare the com.apple.lighthouse.SAOrchestratedExtension extension point and are scheduled by " +
	"Lighthouse, the on-device runtime whose frameworks (LighthouseCoreMLFeatureStore, LighthouseCoreMLModelStore, " +
	"LighthouseModelMonitoring) ship alongside them. Their job is to read a signal source into that local feature " +
	"store; what the SELF acronym stands for is not documented publicly."

var appleContainersDB = map[string]Entry{
	// --- Widget extensions -------------------------------------------------
	// The one genuinely large family in Containers after the media daemons.
	// Ordered here roughly by measured size on a real machine.
	"com.apple.iCal.CalendarWidgetExtension": appleWidgetContainer(
		"com.apple.iCal.CalendarWidgetExtension",
		"Calendar's widget extension, which draws the Up Next, Month and List widgets, and at around 4MB is "+
			"reliably the largest widget container on a Mac",
		"Your calendars, events and subscriptions are stored by the Calendar database in ~/Library/Calendars and "+
			"in iCloud, none of it in this container.",
	),
	"com.apple.stocks.widget": appleWidgetContainer(
		"com.apple.stocks.widget",
		"the Stocks widget extension",
		"Your watchlist is synced through your Apple Account and kept by the Stocks app, not here.",
	),
	"com.apple.weather.widget": appleWidgetContainer(
		"com.apple.weather.widget",
		"the Weather widget extension",
		"Your saved locations belong to the Weather app and sync through iCloud; they are not in this container.",
	),
	"com.apple.podcasts.widget": appleWidgetContainer(
		"com.apple.podcasts.widget",
		"the Podcasts widget extension",
		"Your subscriptions, play position and any downloaded episodes belong to the Podcasts app and are "+
			"untouched.",
	),
	"com.apple.clock.WorldClockWidget": appleWidgetContainer(
		"com.apple.clock.WorldClockWidget",
		"the Clock app's World Clock widget extension",
		"The cities you added, and any alarms or timers, are the Clock app's own settings and survive.",
	),
	"com.apple.PeopleViewService.PeopleWidget-macOS": appleWidgetContainer(
		"com.apple.PeopleViewService.PeopleWidget-macOS",
		"the People widget extension, which shows the faces Photos has recognised",
		"The people themselves, and the names you gave them, are stored inside your Photos library and are not "+
			"affected.",
	),
	"com.apple.Notes.WidgetExtension": appleWidgetContainer(
		"com.apple.Notes.WidgetExtension",
		"the Notes widget extension",
		"Your notes live in the Notes database and in iCloud; not one of them is stored in this container.",
	),
	"com.apple.Safari.SafariWidgetExtension": appleWidgetContainer(
		"com.apple.Safari.SafariWidgetExtension",
		"Safari's widget extension, which renders the Reading List and Shared with You widgets",
		"Your Reading List, bookmarks and history belong to Safari and are stored well outside this container.",
	),
	"com.apple.reminders.WidgetExtension": appleWidgetContainer(
		"com.apple.reminders.WidgetExtension",
		"the Reminders widget extension",
		"Your lists and reminders are in the Reminders store and in iCloud, not in this folder.",
	),
	"com.apple.Photos.PhotosReliveWidget": appleWidgetContainer(
		"com.apple.Photos.PhotosReliveWidget",
		"the Photos widget extension that surfaces Memories and featured photos",
		"The photos are in your Photos library and only rendered copies of them were ever here.",
	),
	"com.apple.findmy.FindMyWidgetPeople": appleWidgetContainer(
		"com.apple.findmy.FindMyWidgetPeople",
		"the Find My widget extension for people",
		"Location sharing is an account-level setting and live locations come from the network on demand, so "+
			"nothing about who you share with is stored here.",
	),
	"com.apple.findmy.FindMyWidgetItems": appleWidgetContainer(
		"com.apple.findmy.FindMyWidgetItems",
		"the Find My widget extension for items — AirTags and Find My-network accessories",
		"Your registered items belong to your Apple Account, so none of them can be lost by clearing this cache.",
	),
	"com.apple.Home.HomeEnergyWidgets": appleWidgetContainer(
		"com.apple.Home.HomeEnergyWidgets",
		"the Home app's energy widget extension",
		"Your home configuration, accessories and automations are stored by HomeKit and synced through iCloud, "+
			"not in this container.",
	),
	"com.apple.ScreenTimeWidgetApplication.ScreenTimeWidgetExtension": appleWidgetContainer(
		"com.apple.ScreenTimeWidgetApplication.ScreenTimeWidgetExtension",
		"the Screen Time widget extension",
		"The underlying usage history is kept by the Screen Time agent in its own container and by iCloud if "+
			"you sync it across devices; only the drawn charts were here.",
	),
	"com.apple.journal.widgets": appleWidgetContainer(
		"com.apple.journal.widgets",
		"the Journal widget extension",
		"Your journal entries are stored by the Journal app, and this container never held their text.",
	),
	"com.apple.tips.Widget": appleWidgetContainer(
		"com.apple.tips.Widget",
		"the Tips widget extension",
		"Nothing personal is involved at all — the content is Apple's tip catalogue, re-downloaded as needed.",
	),
	"com.apple.shortcuts.ShortcutsWidget": appleWidgetContainer(
		"com.apple.shortcuts.ShortcutsWidget",
		"the Shortcuts widget extension, which renders the buttons for shortcuts you pinned to a widget",
		"The shortcuts themselves are stored by the Shortcuts app and synced through iCloud, so none of your "+
			"automations are at risk.",
	),
	"com.apple.AccessibilitySettingsWidgetExtension": appleWidgetContainer(
		"com.apple.AccessibilitySettingsWidgetExtension",
		"the Accessibility Settings widget extension",
		"Every accessibility setting it displays is a system preference and is read live, never stored here.",
	),

	// --- Quick Look thumbnail renderers ------------------------------------
	// One per file type, all empty, all in the screenshot's wall of unknowns.
	"com.apple.quicklook.thumbnail.ImageExtension": appleQuickLookThumbnail(
		"bitmap and vector images — JPEG, PNG, HEIC, TIFF, GIF and the raw formats Image I/O understands",
	),
	"com.apple.quicklook.thumbnail.TextExtension": appleQuickLookThumbnail(
		"plain text, rich text and source files, which it renders by drawing the first page of the document",
	),
	"com.apple.quicklook.thumbnail.CalendarExtension": appleQuickLookThumbnail(
		".ics calendar files and .vcs invitations",
	),
	"com.apple.quicklook.thumbnail.ContactExtension": appleQuickLookThumbnail(
		".vcf contact cards, drawing the contact's photo or monogram",
	),
	"com.apple.quicklook.thumbnail.FontExtension": appleQuickLookThumbnail(
		"font files — .ttf, .otf, .ttc and font suitcases — by typesetting a sample of the typeface",
	),
	"com.apple.quicklook.thumbnail.OfficeExtension": appleQuickLookThumbnail(
		"Microsoft Office documents, so that .docx, .xlsx and .pptx files get a first-page preview in Finder " +
			"even with no Office installed",
	),
	"com.apple.quicklook.thumbnail.PackageExtension": appleQuickLookThumbnail(
		".pkg and .mpkg installer packages, which it draws as the installer box icon",
	),
	"com.apple.quicklook.thumbnail.WebExtension": appleQuickLookThumbnail(
		"web content — .html, .webarchive and .url files — by rendering the page off-screen",
	),
	"com.apple.quicklook.thumbnail.ClippingsExtension": appleQuickLookThumbnail(
		"the .textClipping and .pictClipping files Finder creates when you drag a selection to the desktop",
	),
	"com.apple.quicklook.thumbnail.AudiovisualExtension": appleQuickLookThumbnail(
		"audio and video files, drawing embedded cover art for audio and a frame from the film for video",
	),

	// --- PriML / PFL machine-learning host plugins -------------------------
	"com.apple.priml.pfl.Vermillion": appleMLHostPlugin(
		"Vermillion",
		applePriMLNote+" Vermillion in particular is a bare codename with no public documentation whatsoever; "+
			"all that can honestly be said is that it is an Apple-signed private-ML worker, not something a "+
			"third party installed.",
	),
	"com.apple.priml.PFLMLHostPlugins.PrivateEvolutionPlugin": appleMLHostPlugin(
		"PrivateEvolutionPlugin",
		applePriMLNote+" The name matches Private Evolution, a published method for generating differentially "+
			"private synthetic data without training a model, which would fit the surrounding machinery — but "+
			"Apple has not confirmed the connection, so treat that as a reading of the name and nothing more.",
	),
	"com.apple.priml.PFLMLHostPlugins.SiriAutoEvalPlugin": appleMLHostPlugin(
		"SiriAutoEvalPlugin",
		applePriMLNote+" The name points at automated evaluation of Siri models on device — scoring how a "+
			"model performed without a human reading what you said — which is consistent with the "+
			"SiriMASPFLTraining framework shipped next to it.",
	),
	"com.apple.priml.PFLMLHostPlugins.SiriMASPFLCK": appleMLHostPlugin(
		"SiriMASPFLCK",
		applePriMLNote+" MAS and CK are undocumented internal abbreviations; the surrounding framework is "+
			"named SiriMASPFLTraining, so this sits on Siri's federated-training path rather than anywhere "+
			"near your recordings.",
	),
	"com.apple.priml.PFLMLHostPlugins.SiriMASPFLPush": appleMLHostPlugin(
		"SiriMASPFLPush",
		applePriMLNote+" Its sibling SiriMASPFLCK covers the same Siri federated-training path; the \"Push\" "+
			"half of the pair is the one that submits an aggregate update when the device is idle and charging.",
	),
	"com.apple.priml.PFLMLHostPlugins.AVCPlugin": appleMLHostPlugin(
		"AVCPlugin",
		applePriMLNote+" What AVC abbreviates here is not documented and it is not the video codec of the same "+
			"name; the bundle is an ordinary ML host worker like the rest of the family.",
	),
	"com.apple.priml.PFLMLHostPlugins.FedAutoEvalPlugin": appleMLHostPlugin(
		"FedAutoEvalPlugin",
		applePriMLNote+" \"Fed\" is federated and \"AutoEval\" automated evaluation, so this is the "+
			"model-scoring counterpart to the training plugins rather than a data collector of its own.",
	),

	// --- Lighthouse / proactive SELF ingestors ------------------------------
	"com.apple.proactive.AppleIntelligenceReportingSELFIngestor": {
		Score: Caution,
		Description: "The sandbox of AppleIntelligenceReportingSELFIngestor, an Apple-signed extension at " +
			"/System/Library/ExtensionKit/Extensions/AppleIntelligenceReportingSELFIngestor.appex. " +
			appleSELFIngestorNote + " This one reads Apple Intelligence's own reporting signals. On a real " +
			"machine the container is empty apart from its container-manager metadata plist.",
		Effects: "No clean action is offered, because the folder holds nothing to reclaim. Apple Intelligence is " +
			"opt-in and its switch is in System Settings; turning it off stops the work these extensions do, " +
			"which is a far more useful lever than deleting a 4KB folder macOS would recreate.",
	},
	"com.apple.siri.GMSSELFIngestor": {
		Score: Caution,
		Description: "The sandbox of GMSSELFIngestor, shipped at " +
			"/System/Library/ExtensionKit/Extensions/GMSSELFIngestor.appex. " + appleSELFIngestorNote +
			" This one covers Siri's GMS signals. Like the rest of the family the container is empty on a real " +
			"machine — the extension's output goes to Lighthouse's feature store, not into its own sandbox.",
		Effects: "No clean action is offered; there is nothing in the folder. Siri's analytics contribution is " +
			"controlled in System Settings > Privacy & Security > Analytics & Improvements, and the recordings " +
			"and transcripts people usually worry about are governed separately under Siri & Spotlight, not by " +
			"anything stored here.",
	},
	"com.apple.lighthouse.SiriCoreMetricsWorker": {
		Score: Caution,
		Description: "A Lighthouse-scheduled worker that computes Siri's core on-device metrics. Lighthouse is " +
			"the on-device machine-learning runtime whose frameworks (LighthouseCoreMLFeatureStore, " +
			"LighthouseCoreMLModelStore, LighthouseModelMonitoring, LighthouseDataProcessor) ship in " +
			"/System/Library/PrivateFrameworks, and it runs a dozen or so of these workers on its own schedule. " +
			"The container is an empty skeleton home directory.",
		Effects: "No clean action is offered — the folder is empty and containermanagerd recreates it the moment " +
			"Lighthouse next schedules the worker. It is described here only so the name reads as a normal part " +
			"of macOS rather than something unexplained.",
	},
	"com.apple.lighthouse.SiriTurnRestatementExtension": {
		Score: Caution,
		Description: "Another Lighthouse worker, this one on Siri's \"turn restatement\" path — the step that " +
			"rephrases what Siri understood from a conversational turn so the result can be evaluated on " +
			"device. It has a near-identical twin registered under com.apple.SiriMetrics with the same bundle " +
			"name, which is why the same words appear twice in this listing. Both containers are empty.",
		Effects: "No clean action is offered; neither container holds anything. Siri's own request history and " +
			"the settings that govern it are elsewhere entirely, so there is nothing to gain or lose here.",
	},

	// --- Shortcuts / WorkflowKit -------------------------------------------
	"com.apple.WorkflowKit.ShortcutsIntents": {
		Score: Caution,
		Description: "The sandbox of ShortcutsIntents, the extension that lets other apps and Siri run your " +
			"shortcuts as App Intents. It ships inside the WorkflowKit framework at " +
			"/System/Library/PrivateFrameworks/WorkflowKit.framework/PlugIns/ShortcutsIntents.appex and gets a " +
			"container of its own like any sandboxed extension. Your actual shortcuts are not here: the " +
			"Shortcuts app keeps them in its own storage and syncs them through iCloud.",
		Effects: "No clean action is offered — the container holds only its container-manager metadata plist, so " +
			"there is nothing to reclaim. Deleting it could not remove a shortcut even if you wanted it to; " +
			"shortcuts are managed from inside the Shortcuts app.",
	},
	"com.apple.WorkflowKit.ShortcutsViewService": {
		Score: Caution,
		Description: "The sandbox of ShortcutsViewService, the out-of-process view service that draws the " +
			"Shortcuts UI other apps embed — the action picker and shortcut editor sheets you see without " +
			"leaving the host app. It is the sibling of ShortcutsIntents in the same WorkflowKit framework. " +
			"Nothing of yours is stored here; the shortcuts it displays are read from the Shortcuts app.",
		Effects: "No clean action is offered. The folder contains no data — just the container skeleton macOS " +
			"creates for every sandboxed extension — and your shortcuts, their configuration and their iCloud " +
			"sync are entirely unaffected either way.",
	},

	// --- Siri and Apple Intelligence ---------------------------------------
	"com.apple.siri.media-indexer": {
		Score: Risky,
		Description: "The sandbox of Siri's media indexer, and one of the few Siri containers with real content " +
			"in it — around 5MB of .tdb files sitting directly in Data (albumtitlesedgeTable.tdb, " +
			"artistnamesedgeTable.tdb, composernamesedgeTable.tdb and their dataTable counterparts). They are a " +
			"phonetic index of your music library's titles, artists and composers, built so \"play <album>\" " +
			"can be matched against what you actually own rather than against a generic vocabulary.",
		Effects: "No clean action is offered. The index is derived from your Music library and would rebuild " +
			"itself, but it lives loose in Data rather than in a cache directory, so there is no way to clear " +
			"it that this app can guarantee is scoped correctly — and 5MB is not worth being wrong about. " +
			"Nothing here is your music: the library, playlists and play counts are the Music app's, and " +
			"deleting these files would only cost Siri its ability to recognise your album and artist names " +
			"until it reindexed.",
	},
	"com.apple.Siri": {
		Score: Risky,
		Description: "The sandbox of the Siri interface itself — the process behind the Siri window and its " +
			"result views. It is a few dozen kilobytes: preferences and view-service scratch state. None of " +
			"Siri's substance is here; requests are handled by separate daemons, the settings live in System " +
			"Settings, and any audio Apple retains is governed by the Siri & Dictation privacy controls rather " +
			"than by a file in this folder.",
		Effects: "No clean action is offered. There is nothing meaningful to reclaim, and the Siri path is a bad " +
			"place to be wrong for the sake of a few dozen kilobytes. If Siri is misbehaving, toggling it off " +
			"and on in System Settings resets far more of its state than emptying this container would.",
	},
	"com.apple.GenerativePlaygroundApp": {
		Score: Risky,
		Description: "The sandbox of Image Playground, which ships under the internal name GenerativePlayground. " +
			"On this machine it holds only a preferences plist, because generated images are saved out to your " +
			"Photos library or wherever you exported them rather than kept inside the container. Its three " +
			"sibling containers — the App Intents, Messages and remote-UI extensions — are the same shape.",
		Effects: "No clean action is offered. It is currently a few dozen kilobytes, but this is a container for " +
			"a document-producing app: a sandboxed app's Data/Documents is its Documents folder, and on a Mac " +
			"where Image Playground has been used more heavily this is exactly the kind of folder that can end " +
			"up holding generated images that exist nowhere else. Delete images from inside the app instead, " +
			"where they go through the Trash.",
	},
	"com.apple.aiml.mlpt.FedStats.MLHostPlugin": {
		Score: Caution,
		Description: "One of the FedStats plugins in the com.apple.aiml namespace — Apple's AI/ML federated " +
			"statistics workers, which compute aggregate statistics on device so that only the aggregate, never " +
			"the underlying data, is ever eligible to leave. There are half a dozen registered variants " +
			"(MLHostPlugin, MLHostPluginClassA, MLHostPluginClassB, and PriML.FedStats.Plugin and friends), " +
			"differing by the data class they are permitted to read.",
		Effects: "No clean action is offered: these containers are tens of kilobytes of empty skeleton at most. " +
			"The switch that actually matters is System Settings > Privacy & Security > Analytics & " +
			"Improvements, which stops the work; deleting an empty folder macOS recreates does not.",
	},
	"com.apple.intelligenceflow.IntelligenceFlowAppIntentsExtension": {
		Score: Caution,
		Description: "One of the Intelligence Flow containers. Intelligence Flow is the framework family behind " +
			"Apple Intelligence's planning and tool-calling — /System/Library/PrivateFrameworks holds " +
			"IntelligenceFlow, IntelligenceFlowPlannerRuntime, IntelligenceFlowContext and a dozen more — and " +
			"this extension is the bridge that lets it invoke App Intents that apps have published. The " +
			"container is empty; the models and the planner's state are framework-side, not here.",
		Effects: "No clean action is offered, because there is nothing in the folder. Apple Intelligence is " +
			"opt-in and can be switched off in System Settings, which is the only meaningful way to stop this " +
			"machinery; the empty container it leaves behind costs 4KB.",
	},
	"com.apple.intelligenceplatform.IntelligencePlatform.DiagnosticExtension": {
		Score: Caution,
		Description: "The diagnostic extension of Intelligence Platform, the sibling framework family to " +
			"Intelligence Flow (IntelligencePlatformCompute, IntelligencePlatformQuery, " +
			"IntelligencePlatformDataActions and others ship alongside it). Diagnostic extensions exist so that " +
			"a sysdiagnose can collect a subsystem's state without that subsystem having to be running; they " +
			"produce output on demand and store nothing between runs.",
		Effects: "No clean action is offered — the container is empty and stays empty except during a " +
			"sysdiagnose. Nothing about Apple Intelligence's behaviour, models or settings is stored here.",
	},

	// --- Credential-adjacent containers: refuse every action ---------------
	// The main Passwords app and its menu-bar extra are already covered in
	// containersDB; these are the two remaining pieces of the same feature,
	// and they get the same answer for the same reason.
	"com.apple.Passwords-Settings.extension": {
		Score: Risky,
		Description: "The sandbox of the Passwords pane inside System Settings, which runs as its own extension " +
			"and so gets its own container. No password is in it: credentials live in the iCloud Keychain " +
			"behind the system keychain services. What is here is about 2MB, and nearly all of that is an empty " +
			"httpstorages.sqlite whose write-ahead log has grown to 2MB and never been checkpointed, plus a " +
			"small WebKit store backing the pane's views.",
		Effects: "This app refuses every action on this folder, including the manual delete override, because " +
			"the entire payoff is a stale SQLite log and the folder sits on the credential path. Being wrong " +
			"here is expensive and being right is worth two megabytes. If the pane misbehaves, signing out of " +
			"and back into iCloud is the supported fix.",
		Protected: true,
	},
	"com.apple.PasswordManagerBrowserExtensionHelper": {
		Score: Risky,
		Description: "The sandbox of the helper that lets Safari's password AutoFill talk to the system " +
			"credential store — the process behind the suggestion sheet when a site asks for a login. It stores " +
			"no credentials of its own; as with the Passwords settings extension, the ~2MB it reports is almost " +
			"entirely an un-checkpointed httpstorages.sqlite-wal wrapped around an empty database.",
		Effects: "This app refuses every action on this folder, including the manual delete override. Two " +
			"megabytes of stale write-ahead log is not worth any risk at all on the path that fills in your " +
			"saved passwords, and nothing in the folder regrows enough to be worth revisiting.",
		Protected: true,
	},
	"com.apple.UsageTrackingAgent": {
		Score: Risky,
		Description: "The sandbox of the Screen Time usage agent. " +
			"Data/Library/Application Support/UsageTrackingAgent/UsageTracking.sqlite is the local record of " +
			"which apps and websites you used and for how long — the data behind every Screen Time report and " +
			"every app limit you have set. It is small, a couple of hundred kilobytes, because it stores " +
			"aggregates rather than a raw event log.",
		Effects: "This app refuses every action on this folder, including the manual delete override. Screen " +
			"Time history cannot be regenerated: unless you have Screen Time syncing across devices through " +
			"iCloud, this file is the only copy, and deleting it silently resets your usage history and can " +
			"leave configured limits pointing at nothing. Screen Time data is cleared from System Settings, " +
			"where it is a deliberate choice rather than a side effect of a cleanup.",
		Protected: true,
	},
	"com.apple.mediastream.mstreamd": {
		Score: Risky,
		Description: "The sandbox of mstreamd, the daemon behind Shared Albums and iCloud photo streams. " +
			"Data/Library/MediaStream holds its bookkeeping about which shared albums exist, what has been " +
			"uploaded and what has been pulled down. The photos themselves are in your Photos library and in " +
			"iCloud, not in this container.",
		Effects: "No clean action is offered. The folder is a couple of hundred kilobytes, and clearing sync " +
			"bookkeeping out from under a running photo-sync daemon is exactly the kind of change that produces " +
			"duplicate uploads or an album that will not resync — a poor trade for the space involved.",
	},
	"com.apple.systempreferences.AppleIDSettings": {
		Score: Risky,
		Description: "The sandbox of the Apple Account pane in System Settings — the page listing your devices, " +
			"iCloud storage and subscriptions. Most of its 1.4MB is an HTTPStorages database and a WebKit store " +
			"backing the account pages, which Apple serves as web views; Data/Library/Application Support holds " +
			"a small amount of account-flow state. Your Apple Account password is in the system keychain, not " +
			"here.",
		Effects: "No clean action is offered. There is barely a megabyte of real content, and the state in it is " +
			"tied to an authenticated session — clearing it can produce a re-authentication prompt, or a pane " +
			"that renders blank until it re-fetches, for no benefit. Quitting and reopening System Settings " +
			"fixes a misdrawn pane.",
	},
	"com.apple.voicebankingd": {
		Score: Risky,
		Description: "The sandbox of voicebankingd, the daemon behind Personal Voice and Live Speech — the " +
			"accessibility features that let someone record their own voice and have the Mac speak in it. The " +
			"name is alarming for a cleanup tool and the reality is not: on a real machine the container holds " +
			"a preferences plist, a key-value sync token for com.apple.accessibility.livespeech, and an empty " +
			"httpstorages.sqlite whose 2MB write-ahead log accounts for essentially the whole folder.",
		Effects: "No clean action is offered. The recordings and the trained voice model are not in this " +
			"container — they are held by the accessibility subsystem outside your home folder — but a Personal " +
			"Voice takes fifteen minutes of a person's speech to create and cannot be regenerated from " +
			"anything, so this app stays well clear of every folder on that path for the sake of two megabytes " +
			"of stale log.",
	},
	"com.apple.iWork.Pages": {
		Score: Risky,
		Description: "A second Pages container, alongside com.apple.Pages, registered under the iWork bundle " +
			"identifier. Data/Library/Application Support is nearly all of its ~4MB and holds the same class of " +
			"content as its twin: autosave state and any templates you made yourself. As with any sandboxed " +
			"document app, Data/Documents is that app's Documents folder and can hold real work.",
		Effects: "No clean action is offered, for the same reason the com.apple.Pages container gets none: there " +
			"is nothing cache-shaped in it and the downside is losing autosaved documents or your own " +
			"templates. To reclaim space from Pages, delete documents from inside the app, where they go " +
			"through the Trash and stay recoverable.",
	},
	"com.apple.FontBook": {
		Score: Risky,
		Description: "Font Book's sandbox. Data/Library/Application Support holds Identifiers.json and Font " +
			"Book's own bookkeeping about which fonts are activated and which collections you created. The font " +
			"files themselves are in ~/Library/Fonts and /System/Library/Fonts, entirely outside this container.",
		Effects: "No clean action is offered. At under 200KB there is nothing to reclaim, and font collections " +
			"are something you built by hand that nothing else has a copy of. No installed font could be lost " +
			"by clearing this folder, but the organisation you imposed on them could be.",
	},
	"com.apple.reminders": {
		Score: Risky,
		Description: "The Reminders app's sandbox. Its ~700KB is mostly Data/Library/Application Support, " +
			"holding UI state, a TipKit database and CrashReporter scratch rather than reminder content — the " +
			"actual lists live in the Reminders store and sync through iCloud. The rule for sandboxed apps " +
			"still applies though: this Data folder is the app's home directory, not a cache.",
		Effects: "No clean action is offered. There is under a megabyte here and none of it is cache-shaped, so " +
			"the only thing a delete could achieve is resetting the app's window state — while carrying the " +
			"risk any container delete carries. Reminders are removed from inside the app, where they go to " +
			"Recently Deleted first.",
	},
	"com.apple.Maps": {
		Score: Risky,
		Description: "The Maps app's own sandbox, which is separate from geod's — geod holds the tile cache and " +
			"the abandoned temp files that make up most of Maps' disk usage, and that container is described " +
			"separately. This one is small and includes a Data/Maps subdirectory that macOS protects from " +
			"reads, so even measuring it accurately requires Full Disk Access.",
		Effects: "No clean action is offered. There are barely a hundred kilobytes on offer, part of the folder " +
			"is deliberately unreadable, and your Guides, Favourites and Home/Work addresses sync through your " +
			"Apple Account rather than living here. The reclaimable space in Maps is in the geod container, " +
			"which this app does describe and does offer to clean.",
	},

	// --- Small but genuinely cleanable service containers -------------------
	"com.apple.SafariPlatformSupport.Helper": {
		Score: Caution,
		Description: "The sandbox of a Safari helper process that renders parts of Safari's UI in a separate " +
			"sandbox from the browser itself. Its megabyte splits between Data/Library/HTTPStorages, a WebKit " +
			"store and Data/Library/Caches, all of it fetched content backing those views. None of your " +
			"browsing history, cookies or saved passwords is in this container — those belong to Safari and to " +
			"the keychain.",
		Effects: "Quit Safari first. Removes only the helper's own cache directory, so the WebKit store and " +
			"HTTPStorages database are left alone and no session in the helper's views is dropped. The content " +
			"re-fetches the next time one of those views is shown, which needs a network connection but " +
			"nothing else.",
		Commands: []string{
			`# Quit Safari first`,
			`rm -rf ~/Library/Containers/com.apple.SafariPlatformSupport.Helper/Data/Library/Caches/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/*`},
	},
	"com.apple.LookupViewService": {
		Score: Caution,
		Description: "The sandbox of the Look Up panel — the popover you get from a three-finger tap or " +
			"Ctrl-Cmd-D, showing dictionary definitions, Siri Knowledge and web suggestions for a selected " +
			"word. Data/Library/Caches holds the fetched results for words looked up recently; the dictionaries " +
			"themselves ship with macOS in /Library/Dictionaries and are not in this container.",
		Effects: "Removes the cached lookup results. The panel keeps working identically — offline dictionary " +
			"definitions come from the installed dictionaries and are not affected at all — and the online " +
			"portion re-fetches on the next lookup of a given word. The cost is a fraction of a second on that " +
			"first lookup and nothing else.",
		Commands: []string{
			`rm -rf ~/Library/Containers/com.apple.LookupViewService/Data/Library/Caches/*`,
		},
		CleanPaths: []string{`Data/Library/Caches/*`},
	},
	"com.apple.CalendarWeatherKitService": {
		Score: Caution,
		Description: "The sandbox of the small service that fetches weather for Calendar, so an event's location " +
			"can show a forecast. Essentially all of its ~850KB is Data/Library/HTTPStorages: an " +
			"httpstorages.sqlite whose write-ahead log has grown and never been checkpointed, wrapping cached " +
			"WeatherKit responses. Nothing about your calendars or events is stored here.",
		Effects: "Removes the cached forecast responses and the oversized log around them. Your events, " +
			"calendars and their locations belong to Calendar and are untouched; the service re-fetches a " +
			"forecast the next time you look at an event that has one, which takes a network round trip and no " +
			"longer.",
		Commands: []string{
			`rm -rf ~/Library/Containers/com.apple.CalendarWeatherKitService/Data/Library/HTTPStorages/*`,
		},
		CleanPaths: []string{`Data/Library/HTTPStorages/*`},
	},
	"com.apple.helpviewer": {
		Score: Caution,
		Description: "Help Viewer's sandbox — the window that opens from a Help menu. Its ~600KB is a WebKit " +
			"store and an HTTPStorages database, because help books are rendered as web content and Apple " +
			"serves updated pages online. The much larger side of the same feature is helpd's cache in " +
			"~/Library/Caches/com.apple.helpd, which holds the generated help indexes and typically runs to " +
			"tens of megabytes.",
		Effects: "Quit Help Viewer first. Removes the cached help pages the viewer had fetched. Nothing of yours " +
			"is involved and no help book is uninstalled — the next time you open Help, pages are fetched again " +
			"or read from the local help books, needing a network connection only for the online portions.",
		Commands: []string{
			`# Quit Help Viewer first`,
			`rm -rf ~/Library/Containers/com.apple.helpviewer/Data/Library/WebKit/*`,
			`rm -rf ~/Library/Containers/com.apple.helpviewer/Data/Library/HTTPStorages/*`,
		},
		CleanPaths: []string{`Data/Library/WebKit/*`, `Data/Library/HTTPStorages/*`},
	},
	"com.apple.Safari.CacheDeleteExtension": {
		Score: Caution,
		Description: "The sandbox of the extension that lets macOS's cache-delete machinery reclaim Safari's " +
			"caches when the disk runs low — that is, the thing whose whole job is deleting caches has a cache " +
			"of its own. Its ~400KB is almost entirely a WebKit store. It carries none of your browsing data; " +
			"it exists so that cache_delete can ask Safari to free space without launching the browser.",
		Effects: "Quit Safari first. Removes the extension's WebKit store, which it recreates on demand. Nothing " +
			"about Safari, your history, cookies or the caches this extension manages on macOS's behalf is " +
			"changed — the extension is a mechanism, and clearing its scratch space does not disable it.",
		Commands: []string{
			`# Quit Safari first`,
			`rm -rf ~/Library/Containers/com.apple.Safari.CacheDeleteExtension/Data/Library/WebKit/*`,
		},
		CleanPaths: []string{`Data/Library/WebKit/*`},
	},
	"com.apple.weather": {
		Score: Caution,
		Description: "The Weather app's sandbox. Of its ~4MB, Data/Library/Application Support is the larger " +
			"half and holds the app's real state — the Weather folder with your saved locations, an iCloud sync " +
			"folder, a TipKit database and queued app-analytics payloads — while Data/Library/HTTPStorages is " +
			"cached forecast responses. Only the second of those is disposable.",
		Effects: "Quit Weather first. This removes the cached forecast responses only and deliberately leaves " +
			"Data/Library/Application Support alone, so your saved cities and their order survive exactly as " +
			"they were. The app re-fetches current conditions the next time you open it, which needs a network " +
			"connection and takes a moment on first launch.",
		Commands: []string{
			`# Quit Weather first`,
			`rm -rf ~/Library/Containers/com.apple.weather/Data/Library/HTTPStorages/*`,
		},
		CleanPaths: []string{`Data/Library/HTTPStorages/*`},
	},
}

// appleURLCacheShape and appleURLCacheEffects are the halves of a
// Foundation URL cache entry that are identical for every daemon that
// writes one. The layout is always the same — Cache.db plus its -shm/-wal
// companions and an fsCachedData directory for bodies too large to inline
// — and so is the consequence of clearing it. Measured on a real machine,
// the -wal file is routinely 95%+ of the folder: macOS almost never
// checkpoints these, so a "15MB" cache is usually a 140KB database behind a
// stale log.
const appleURLCacheShape = "On disk it is the standard Foundation URL cache: Cache.db with its -shm and -wal " +
	"companions, plus an fsCachedData folder for response bodies too large to store inline. The write-ahead log " +
	"is usually almost all of the size — macOS rarely checkpoints these — so most of what this folder reports is " +
	"a stale log wrapped around a nearly empty database."

const appleURLCacheEffects = "Everything in a URL cache is a copy of a server response, so the only cost is one " +
	"extra network round trip the next time the same data is wanted. No account, credential, preference or " +
	"history is stored in one, and the daemon recreates the database on its next request without any " +
	"intervention."

// appleURLCache builds an entry for one of the many ~/Library/Caches
// folders that are nothing but a Foundation URL cache. Only the prose that
// genuinely differs per daemon — who writes it, and what re-fetching costs
// — is passed in; the shared half about layout and consequences is
// generated, the same way logsWarpRotated generates five near-identical
// rotated-log entries.
func appleURLCache(name, description, effects string) Entry {
	return Entry{
		Score:       Safe,
		Description: description + " " + appleURLCacheShape,
		Effects:     effects + " " + appleURLCacheEffects,
		Commands:    []string{`rm -rf ~/Library/Caches/` + name + `/*`},
	}
}

var appleCachesDB = map[string]Entry{
	"com.apple.helpd": {
		Score: Safe,
		Description: "The cache of helpd, the daemon behind every Help menu in macOS. Generated/ is the bulk of " +
			"it — around 18MB of search indexes helpd builds from the help books installed by macOS and by " +
			"third-party apps — with a Cache.db URL cache and CSHelpIndex.plist alongside for the pages Apple " +
			"serves online. Every byte of it is derived from help books that are still installed elsewhere.",
		Effects: "Quit Help Viewer first. Deletes the generated search indexes and the cached online pages. No " +
			"help book is uninstalled and no app loses its documentation — helpd rebuilds the index the next " +
			"time you search Help, which takes a few seconds and some CPU on that first search. Typically the " +
			"largest single Apple cache on a Mac.",
		Commands: []string{`# Quit Help Viewer first`, `rm -rf ~/Library/Caches/com.apple.helpd/*`},
	},
	"com.apple.donotdisturbd": appleURLCache(
		"com.apple.donotdisturbd",
		"The cache of donotdisturbd, the daemon that runs Focus modes — deciding which notifications are "+
			"delivered, syncing Focus state to your other devices and driving Focus filters. On a real machine "+
			"this folder is routinely 15MB and consists of a 140KB database behind a 15MB write-ahead log, "+
			"making it one of the largest and most misleading Apple caches on the system.",
		"Your Focus modes, their schedules, their allow-lists and the filters you configured are preferences "+
			"synced through your Apple Account, not entries in this cache, so no Focus is altered or lost.",
	),
	"com.apple.CloudTelemetry": {
		Score: Safe,
		Description: "A staging area for telemetry from Apple's cloud-facing daemons, one subfolder per service " +
			"under XPCService/ — com.apple.cloudd for iCloud transfers, com.apple.identityservicesd for " +
			"iMessage/FaceTime identity, com.apple.swtransparencyd for software transparency verification, and " +
			"com.apple.privatecloudcomputed for Private Cloud Compute. Each holds configurations, throttles, " +
			"storebags and an eventcache of not-yet-uploaded events.",
		Effects: "Removes the queued telemetry events and the cached service configuration around them. Nothing " +
			"reads these back as state: the daemons keep working, iCloud sync and iMessage are unaffected, and " +
			"each service re-fetches its configuration on the next run. Anything that had not been uploaded " +
			"never will be, which for some people is the point.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.CloudTelemetry/*`},
	},
	"com.apple.appstoreagent": {
		Score: Caution,
		Description: "The App Store agent's cache. Beyond the usual Cache.db URL cache it holds a set of " +
			"SubscriptionEntitlements_v2 plists — a local snapshot of which subscriptions your Apple Account " +
			"currently entitles you to, covering the App Store, Music, Podcasts and hardware bundles. These are " +
			"a cached copy of server-side truth, not the entitlement itself.",
		Effects: "Quit the App Store first. Removes the cached storefront responses and the entitlement " +
			"snapshot. Nothing you have bought or subscribed to is affected — purchases and subscriptions are " +
			"held by your Apple Account, and the agent re-fetches the entitlement list on its next check. Until " +
			"it does, an app that gates a feature on a subscription may briefly behave as if you do not have " +
			"one, so this is worth doing while online rather than on a plane.",
		Commands: []string{`# Quit the App Store first`, `rm -rf ~/Library/Caches/com.apple.appstoreagent/*`},
	},
	"com.apple.AppleMediaServices": {
		Score: Caution,
		Description: "The shared cache for Apple Media Services — the layer underneath the App Store, Music, TV, " +
			"Podcasts and Books. Its subfolders are named after what they hold: DynamicUI and PaymentSheetsUI " +
			"cache the server-driven interface descriptions Apple sends for storefront pages and purchase " +
			"sheets, Engagement and Metrics hold promotional and analytics state, and Storage/fpdi hold " +
			"service scratch data.",
		Effects: "Quit the App Store, Music and TV first. Removes cached interface descriptions and promotional " +
			"content, all of which is re-fetched from Apple on next use. No purchase, subscription, library or " +
			"sign-in lives here — those are account-side or in the individual apps' own storage. The first " +
			"storefront page you open afterwards will take longer to draw and needs a network connection.",
		Commands: []string{
			`# Quit the App Store, Music and TV first`,
			`rm -rf ~/Library/Caches/com.apple.AppleMediaServices/*`,
		},
	},
	"com.apple.amsengagementd": appleURLCache(
		"com.apple.amsengagementd",
		"The cache of amsengagementd, the Apple Media Services daemon that handles \"engagement\" content — the "+
			"promotional sheets, offers and upsell cards the App Store, Music and TV apps display, along with "+
			"the artwork they use.",
		"Nothing you own is involved: no purchase, subscription or library entry is stored here, and the "+
			"promotional content simply re-downloads the next time one of those apps shows a storefront.",
	),
	"com.apple.passd": {
		Score: Risky,
		Description: "The cache of passd, the daemon behind Wallet and Apple Pay. On disk it is an ordinary " +
			"Foundation URL cache — Cache.db and fsCachedData — holding fetched pass artwork and merchant " +
			"assets, and none of it is card data: payment credentials live in the Secure Enclave and the passes " +
			"themselves are stored by passd outside this cache.",
		Effects: "No clean action is offered. The content really is only cached artwork, but this app does not " +
			"put a delete button on the Apple Pay path for the sake of a few megabytes that regrow on their " +
			"own. If Wallet is misbehaving, removing and re-adding the individual pass is the supported fix.",
	},
	"com.apple.ctcategories.service": appleURLCache(
		"com.apple.ctcategories.service",
		"The cache of the content-categories service, which classifies websites by category so Screen Time's "+
			"web-content limits and app-category reports can work. It caches Apple's category lookups for "+
			"domains you have visited.",
		"Your Screen Time settings, limits and usage history are held by the Screen Time agent, not here, so no "+
			"limit is lifted and no report is emptied; category lookups are simply re-fetched as sites are "+
			"visited again.",
	),
	"com.apple.storekitagent": appleURLCache(
		"com.apple.storekitagent",
		"The cache of the StoreKit agent, which handles in-app purchases and subscriptions on behalf of every "+
			"app that sells something. On a real machine its 4.5MB is a 384KB database behind a 4.1MB "+
			"write-ahead log.",
		"Purchases and subscriptions belong to your Apple Account and are re-validated against Apple's servers, "+
			"so nothing you paid for can be lost here; an app may take a moment longer to confirm an "+
			"entitlement the first time it asks.",
	),
	"com.apple.amsaccountsd": {
		Score: Risky,
		Description: "The cache of amsaccountsd, the Apple Media Services account daemon — the piece that keeps " +
			"the App Store, Music and TV signed in to your Apple Account. It is a plain Foundation URL cache " +
			"(Cache.db plus fsCachedData plus an HSTS list) and its 4.5MB is almost entirely an " +
			"un-checkpointed write-ahead log. Your Apple Account password is in the system keychain, never here.",
		Effects: "Quit the App Store, Music and TV first. Clearing this can make media services re-authenticate, " +
			"which may prompt you for your Apple Account password or a two-factor code — the same trade-off as " +
			"the akd cache next to it. Nothing you have purchased or subscribed to is affected, since that is " +
			"account-side. Worth doing only if you are troubleshooting a media sign-in problem, not as routine " +
			"cleanup.",
		Commands: []string{
			`# Quit the App Store, Music and TV first`,
			`rm -rf ~/Library/Caches/com.apple.amsaccountsd/*`,
		},
	},
	"com.apple.translationd": appleURLCache(
		"com.apple.translationd",
		"The cache of translationd, the daemon behind the Translate app and the Translate option in the "+
			"right-click menu. What it caches is service responses and metadata, not the offline language "+
			"models — those are downloaded assets and are stored outside ~/Library/Caches entirely.",
		"No downloaded language stops working and nothing has to be re-downloaded, because the models are not "+
			"in this folder; only cached service responses are lost.",
	),
	"com.apple.tipsd": appleURLCache(
		"com.apple.tipsd",
		"The cache of tipsd, the daemon that fetches the content shown by the Tips app and by the tip cards "+
			"macOS occasionally surfaces. fsCachedData here is the downloaded tip artwork.",
		"Nothing personal is involved at any point — the content is Apple's tip catalogue, re-downloaded the "+
			"next time Tips has something to show.",
	),
	"com.apple.iCloudNotificationAgent": appleURLCache(
		"com.apple.iCloudNotificationAgent",
		"The cache of the agent that delivers iCloud's own notifications — shared-album invitations, "+
			"storage-full warnings, Family Sharing requests and the like.",
		"No notification you have already received is deleted from Notification Center and no iCloud setting "+
			"changes; the agent simply re-fetches whatever it needs on its next check.",
	),
	"com.apple.appstorecomponentsd": appleURLCache(
		"com.apple.appstorecomponentsd",
		"The cache of appstorecomponentsd, which renders the reusable App Store UI components other apps embed "+
			"— the in-app App Store sheets and product pages you see without leaving the host app.",
		"No installed app, purchase or subscription is involved; an embedded App Store sheet simply takes a "+
			"moment longer to draw the first time it is shown again.",
	),
	"com.apple.FeatureAccessAgent": appleURLCache(
		"com.apple.FeatureAccessAgent",
		"The cache of the feature-access agent, which checks which macOS features your account and region are "+
			"entitled to — the mechanism behind features that appear only in some countries or only on some "+
			"account types.",
		"No feature is disabled by clearing it: the agent re-checks entitlement on its next run, and the answer "+
			"is determined by your account and region rather than by anything in this folder.",
	),
	"com.apple.geoanalyticsd": {
		Score: Safe,
		Description: "The cache of geoanalyticsd, the daemon that collects analytics about Maps and location " +
			"services usage. Unlike its neighbours it holds an APDB.db rather than a Cache.db, and as with the " +
			"rest, the write-ahead log is essentially all of the 3MB: a 24KB database behind a 3MB log.",
		Effects: "Removes the queued location-analytics records. Maps itself is unaffected — tiles, saved " +
			"places and Guides are nowhere near this folder — and location services keep working exactly as " +
			"before. Anything not yet uploaded never will be, which is a feature rather than a cost for most " +
			"people.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.geoanalyticsd/*`},
	},
	"com.apple.itunescloudd": appleURLCache(
		"com.apple.itunescloudd",
		"The cache of itunescloudd, the daemon that syncs your Music library and its cloud state — matched "+
			"tracks, playlist membership, and the artwork metadata that goes with them.",
		"Your music library, playlists, play counts and any downloaded tracks belong to the Music app and to "+
			"your Apple Account, so none of them are touched; the daemon re-fetches cloud state on its next sync.",
	),
	"com.apple.Family-Settings.extension": appleURLCache(
		"com.apple.Family-Settings.extension",
		"The cache of the Family Sharing pane in System Settings, which runs as its own extension and so gets "+
			"its own cache folder. It caches the family-member list and the profile pictures shown in the pane.",
		"Your Family Sharing group, its members and everything shared through it are account-side, so nothing "+
			"about the family changes — the pane just re-fetches names and pictures the next time you open it.",
	),
	"com.apple.sirittsd": {
		Score: Safe,
		Description: "The cache of sirittsd, Siri's text-to-speech daemon. SynthesisCache/ holds audio it has " +
			"already synthesised, so a phrase Siri says often does not have to be generated from the voice " +
			"model every time. The voice models themselves are downloaded assets stored outside " +
			"~/Library/Caches and are not part of this folder.",
		Effects: "Removes the synthesised audio only. No downloaded Siri voice is removed and nothing has to be " +
			"re-downloaded — the daemon regenerates a phrase from the model it already has, costing a barely " +
			"perceptible delay the first time Siri says something it had cached.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.sirittsd/*`},
	},
	"com.apple.proactive.eventtracker": {
		Score: Safe,
		Description: "The cache of the proactive event tracker, part of the \"proactive\" subsystem behind Siri " +
			"Suggestions — the same family as the ProactiveEventTracker and ProactiveHarvesting frameworks in " +
			"/System/Library/PrivateFrameworks. It holds aggregation state (aggr_state), a per-user analytics " +
			"UUID, and log_stores of events waiting to be aggregated.",
		Effects: "Removes the queued events and the tracker's aggregation state. Siri Suggestions keeps working " +
			"— what it suggests is driven by the on-device knowledge store, not by this staging folder — and " +
			"the tracker rebuilds its state as you keep using the Mac. Events that had not been aggregated yet " +
			"are simply dropped.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.proactive.eventtracker/*`},
	},
	"com.apple.duetexpertd": {
		Score: Safe,
		Description: "The cache of duetexpertd, the daemon behind Siri Suggestions and proactive predictions — " +
			"the process that decides which app to suggest in Spotlight or the Dock. All this folder holds is " +
			"com.apple.e5rt.e5bundlecache, a compiled-model cache for the Apple Neural Engine runtime: the " +
			"machine-specific compiled form of models that ship with macOS.",
		Effects: "Removes the compiled model cache. Nothing is learned or forgotten — the behavioural data " +
			"driving suggestions lives in the on-device knowledge store, not here — and the runtime recompiles " +
			"the models the first time it needs them, costing a small one-time CPU spike and nothing else.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.duetexpertd/*`},
	},
	"com.apple.cache_delete": {
		Score: Safe,
		Description: "The bookkeeping of cache_delete, the macOS daemon that reclaims purgeable space when the " +
			"disk fills up — the mechanism that makes deleted-but-still-recoverable caches count as free space " +
			"in Finder. It holds CDPurgeStats, CacheDeletePurgeHistory.txt, breadcrumbs and analytics: a record " +
			"of what it has purged and when, not a cache of anything itself.",
		Effects: "Removes the purge history. The daemon keeps reclaiming space exactly as before, because its " +
			"policy comes from macOS rather than from this folder — the only loss is the log of what it has " +
			"cleaned up before, which is of interest to nobody outside a diagnostic session.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.cache_delete/*`},
	},
	"com.apple.appleaccountd": {
		Score: Risky,
		Description: "The cache of appleaccountd, which handles Apple Account details — the device list, account " +
			"settings and the data behind the Apple Account pane. It is an ordinary Foundation URL cache " +
			"holding responses from Apple's account endpoints; your password and tokens are in the system " +
			"keychain, not in this folder.",
		Effects: "Clearing this can make account-backed services re-authenticate and may prompt for your Apple " +
			"Account password or a two-factor code, the same as clearing the akd cache next to it. Nothing is " +
			"lost from your account — only the cached copy of what Apple's servers already know. Worth doing " +
			"only when troubleshooting an Apple Account problem, not as routine cleanup.",
		Commands: []string{`rm -rf ~/Library/Caches/com.apple.appleaccountd/*`},
	},
	"com.apple.icloudwebd": appleURLCache(
		"com.apple.icloudwebd",
		"The cache of icloudwebd, the daemon behind iCloud's web-backed surfaces — the parts of iCloud settings "+
			"and services macOS renders as web content rather than native views.",
		"No iCloud data is stored here and nothing syncs differently afterwards; the affected views simply "+
			"re-fetch their content the next time one is opened.",
	),
	"com.apple.watchlistd": appleURLCache(
		"com.apple.watchlistd",
		"The cache of watchlistd, which maintains the Up Next list in the TV app across your devices, caching "+
			"artwork and show metadata for the titles in it.",
		"Your Up Next list, watch history and purchases belong to your Apple Account and to the TV app, so "+
			"nothing disappears from the list — artwork just re-downloads as you scroll it.",
	),
	"com.apple.SoftwareUpdateNotificationManager": appleURLCache(
		"com.apple.SoftwareUpdateNotificationManager",
		"The cache of the component that shows software-update notifications, holding the release-note text and "+
			"artwork it displays in those alerts.",
		"No update is installed, cancelled or deferred by clearing it, and no update setting changes — the "+
			"notification content is re-fetched when there is next an update to announce.",
	),
	"com.apple.managedappdistributionagent": appleURLCache(
		"com.apple.managedappdistributionagent",
		"The cache of the managed app distribution agent, which handles apps pushed to the Mac by an "+
			"organisation's device management. On an unmanaged personal Mac it exists but does essentially "+
			"nothing.",
		"No managed app is uninstalled and no management enrolment is affected — the agent's actual state is "+
			"held by the device-management subsystem, and only cached server responses are here.",
	),
	"com.apple.betaenrollmentagent": appleURLCache(
		"com.apple.betaenrollmentagent",
		"The cache of the beta enrolment agent, which handles enrolling the Mac in Apple's public or developer "+
			"beta software programmes and caches the seed catalogue it checks against.",
		"Your beta enrolment status is not stored here, so clearing this cannot un-enrol the Mac or change "+
			"which updates it is offered; the agent re-fetches the seed catalogue on its next check.",
	),
	"com.apple.nbagent": appleURLCache(
		"com.apple.nbagent",
		"The cache of nbagent, the agent behind the News app's background fetching, holding article metadata "+
			"and artwork for stories it has pre-fetched.",
		"Your News subscriptions, saved stories and reading history belong to the News app and your Apple "+
			"Account, so nothing saved is lost — pre-fetched articles are simply downloaded again when needed.",
	),
	"com.apple.FileProviderDaemon.AppStoreService": appleURLCache(
		"com.apple.FileProviderDaemon.AppStoreService",
		"The cache of the File Provider daemon's App Store service — the piece that lets the App Store surface "+
			"content through the File Provider mechanism that also backs iCloud Drive and third-party cloud "+
			"storage in Finder.",
		"No file in iCloud Drive or any other File Provider is touched: this is the App Store's own scratch "+
			"space within that mechanism, and clearing it changes nothing you can see in Finder.",
	),
	"com.apple.AuthenticationServicesCore.AuthenticationServicesAgent": {
		Score: Risky,
		Description: "The cache of the Authentication Services agent — the system component behind \"Sign in " +
			"with Apple\", passkeys and the credential-provider API that password managers plug into. What it " +
			"caches is presentation data for the sheets it shows: relying-party names, icons and the like.",
		Effects: "No clean action is offered. No credential, passkey or token is stored in this folder — those " +
			"are in the keychain and the Secure Enclave — but this app does not offer a delete button anywhere " +
			"on the authentication path for a few hundred kilobytes of cached icons that regrow by themselves.",
	},
	"com.apple.shazamd": {
		Score: Risky,
		Description: "The cache of shazamd, the daemon behind Shazam music recognition and the Music " +
			"Recognition control. It holds a rematch folder of audio signatures awaiting a second matching " +
			"attempt — recognitions that could not be resolved immediately, typically because the Mac was " +
			"offline at the time.",
		Effects: "No clean action is offered. Everything already recognised is in shazamd's Application Support " +
			"library rather than here, but the pending signatures in this folder are the only record of a " +
			"recognition that has not completed yet, and discarding them loses those songs silently for the " +
			"sake of a few hundred kilobytes.",
	},
}

var appleAppSupportDB = map[string]Entry{
	"com.apple.ProtectedCloudStorage": {
		Score: Risky,
		Description: "The local store for Protected Cloud Storage — the key material that lets this Mac decrypt " +
			"end-to-end encrypted iCloud data. The KeysSyncingVersion3-*-ProtectedCloudStorage.db files hold " +
			"the per-service key hierarchies behind iCloud Keychain, Messages in iCloud, Health, Safari data " +
			"and every other service Apple encrypts so that even Apple cannot read it. This is not a cache: it " +
			"is the device's half of that encryption.",
		Effects: "This app refuses every action on this folder, including the manual delete override, and it is " +
			"the clearest case in the whole dictionary. Removing key material that syncs end-to-end encrypted " +
			"data can leave this Mac unable to decrypt its own iCloud content until the account is fully " +
			"re-established, and in the worst case can require re-verifying the account from another trusted " +
			"device. Five megabytes is not a reason to go near it. If something is genuinely wrong, sign out " +
			"of and back into iCloud from System Settings and let macOS rebuild this itself.",
		Protected: true,
	},
	"com.apple.RemoteManagementAgent": {
		Score: Risky,
		Description: "The state of the remote management agent — the component that implements declarative " +
			"device management, the modern MDM mechanism an employer or school uses to configure a Mac. " +
			"Database/ holds the declarations currently in force, Status/ the agent's report of which ones it " +
			"has applied, and MigrationStatus.plist tracks the move from the older MDM protocol.",
		Effects: "This app refuses every action on this folder, including the manual delete override. On a " +
			"managed Mac, deleting the agent's declaration database can leave the device out of sync with its " +
			"management server — configuration profiles, certificates or restrictions may stop being enforced " +
			"or stop being removable — and re-enrolment is not something you can do yourself. On an unmanaged " +
			"Mac the folder is a few hundred kilobytes and equally not worth touching.",
		Protected: true,
	},
	"com.apple.shazamd": {
		Score: Risky,
		Description: "Shazam's on-device library: ShazamLibrary.sqlite is the list of every song this Mac has " +
			"recognised through the Music Recognition control or Siri, with the timestamp and the match for " +
			"each one. It is a couple of hundred kilobytes and it is a history you built by using the feature.",
		Effects: "This app refuses every action on this folder, including the manual delete override. Unless you " +
			"are signed in to a Shazam account that syncs the library, this file is the only copy of that " +
			"recognition history — nothing regenerates it and nothing else on the Mac has it. The place to " +
			"clear it is the Music Recognition history itself, where removing an entry is a deliberate choice.",
		Protected: true,
	},
	"com.apple.MediaPlaybackCore.PlaybackEventStreams": {
		Score: Risky,
		Description: "A staging area for playback events — the Music-*.sqlpkg here is a local stream of what was " +
			"played, for how long, and how it ended, queued for the Music app and Apple's services to consume. " +
			"It is what feeds play counts, listening history and the recommendations built from them.",
		Effects: "No clean action is offered. The 3MB it occupies is not worth the one thing that can go wrong: " +
			"events that have not been consumed yet are the only record of recent listening, and dropping them " +
			"quietly loses play counts and history for that period. Your library, playlists and downloads are " +
			"elsewhere and would be fine — this is purely about the pending events.",
	},
	"com.apple.control-center.tips": {
		Score: Safe,
		Description: "A TipKit database — the framework Apple ships for the small onboarding tips that appear " +
			"once and then never again. The whole folder is a hidden .tipkit directory recording which Control " +
			"Center tips have been displayed and dismissed, so macOS knows not to show them a second time. It " +
			"contains nothing else at all.",
		Effects: "Removes the record of which tips you have already seen. The only visible consequence is that " +
			"Control Center may show an onboarding tip again that you had dismissed. No Control Center setting, " +
			"module or layout is stored here and none of them change.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.control-center.tips/*`},
	},
	"com.apple.controlcenter": {
		Score: Safe,
		Description: "The same shape as com.apple.control-center.tips next to it: a hidden .tipkit directory and " +
			"nothing else, tracking which Control Center tips have been shown. macOS registers the two " +
			"identifiers separately, which is why the same feature occupies two rows in this listing.",
		Effects: "Removes the tip-display record only. Your Control Center layout — which modules are pinned to " +
			"the menu bar, which are in the panel — is a preference stored elsewhere and is not affected; at " +
			"worst a tip you already dismissed appears once more.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.controlcenter/*`},
	},
	"com.apple.games": {
		Score: Safe,
		Description: "The TipKit database for the Games app introduced in recent macOS releases — a hidden " +
			".tipkit directory recording which of its onboarding tips have been shown. Nothing about your " +
			"games, Game Center account, achievements or saves is stored in this folder.",
		Effects: "Removes the tip-display record. Your Game Center profile, achievements, friends and any game " +
			"saves are held by Game Center and by the games themselves, so none of them are affected — the only " +
			"consequence is a re-shown onboarding tip.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.games/*`},
	},
	"com.apple.ap.promotedcontentd": {
		Score: Caution,
		Description: "The state of promotedcontentd, the daemon behind Apple's own advertising — the promoted " +
			"results in App Store search and the ads in News and Stocks. Its subfolders (APDB, APPL, SFS, adsc, " +
			"shared) hold the local targeting and frequency-capping data that decides which promotions to show " +
			"and how often, computed on device.",
		Effects: "Removes the local ad state, which the daemon rebuilds. Nothing about your Apple Account or any " +
			"app changes, and no setting is altered: if you want to turn this off rather than reset it, the " +
			"control is System Settings > Privacy & Security > Apple Advertising > Personalised Ads, which is " +
			"the durable version of what clearing this folder does briefly.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.ap.promotedcontentd/*`},
	},
	"com.apple.siri.inference": {
		Score: Safe,
		Description: "Reference data Siri's on-device inference uses to interpret what you say. On a real " +
			"machine it is a single file — holidays.sqlite3, the holiday calendar for every supported region — " +
			"which is how Siri resolves \"the day before Thanksgiving\" to a date without asking a server. It " +
			"is downloaded reference data, identical on every Mac in the same region.",
		Effects: "Removes the downloaded holiday database. Nothing personal is in it and nothing you said is " +
			"recorded in it; Siri re-downloads it in the background, and until it does, date phrases that " +
			"depend on holidays may not resolve. Requires a network connection to come back.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.siri.inference/*`},
	},
	"com.apple.MediaPlayer": {
		Score: Caution,
		Description: "ServerObjectDatabases/ is the local mirror of media catalogue objects — albums, shows and " +
			"their metadata — that the Music and TV apps have fetched from Apple's servers, kept so the apps " +
			"can draw a library view without a round trip per item. Everything in it came from Apple and can " +
			"come from Apple again.",
		Effects: "Quit Music and TV first. Removes the cached catalogue metadata, which the apps re-fetch as you " +
			"browse. Your library, playlists, play counts, purchases and downloaded tracks are stored " +
			"elsewhere and are not affected; the visible cost is slower browsing and some blank metadata until " +
			"it refills, which needs a network connection.",
		Commands: []string{
			`# Quit Music and TV first`,
			`rm -rf ~/Library/Application\ Support/com.apple.MediaPlayer/*`,
		},
	},
	"com.apple.iTunesCloud": {
		Score: Safe,
		Description: "URLBags/ holds \"bags\" — the signed configuration documents Apple's media services hand " +
			"out listing which endpoint to use for which operation, refreshed periodically. It is pure service " +
			"configuration: no account data, no library data, no purchase history, just a cached copy of where " +
			"to send the next request.",
		Effects: "Removes the cached endpoint configuration. The Music, TV and App Store apps re-fetch a bag on " +
			"their next request, which needs a network connection and adds one round trip. Nothing about your " +
			"account, library or purchases is stored here or affected in any way.",
		Commands: []string{`rm -rf ~/Library/Application\ Support/com.apple.iTunesCloud/*`},
	},
	"com.apple.appleintelligencereporting.processing": {
		Score: Safe,
		Description: "The processing scratch directory for Apple Intelligence reporting — the counterpart to the " +
			"AppleIntelligenceReportingSELFIngestor extension over in ~/Library/Containers. It is where that " +
			"reporting path stages work in progress, and on a machine where Apple Intelligence sees light use " +
			"it stays at a few kilobytes.",
		Effects: "Removes staged, not-yet-processed reporting data. No Apple Intelligence feature, setting or " +
			"model is affected — this is a work queue, not state — and the directory refills as the feature is " +
			"used. Apple Intelligence itself is opt-in and switched off in System Settings, which is the real " +
			"control if that is what you are after.",
		Commands: []string{
			`rm -rf ~/Library/Application\ Support/com.apple.appleintelligencereporting.processing/*`,
		},
	},
}
