package knowledge

// Chrome's profile internals, for the folders you land on after opening
// ~/Library/Application Support/Google.
//
// The "Google" entry in appsupport.go already offers the cleanup; these
// exist so that drilling in explains what you are looking at rather than
// showing an unrated 4 GB folder full of version numbers. That folder —
// OptGuideOnDeviceModel — is the single largest thing in a Chrome profile
// on a machine where the on-device model has downloaded, and nothing
// about its name says so.

var chromeAppSupportDB = map[string]Entry{
	"OptGuideOnDeviceModel": {
		Score: Safe,
		Description: "Chrome's on-device foundation model, Gemini Nano. One versioned subdirectory per model " +
			"release, each holding a weights.bin of roughly 4GB, downloaded in the background on machines Chrome " +
			"considers eligible. It powers local AI features — page summaries, \"help me write\", on-device scam " +
			"detection — and is not needed for anything else Chrome does. This is normally the largest single " +
			"thing in a Chrome profile, and none of it is your data.",
		Effects: "Quit Chrome first. Removes the downloaded model. Chrome keeps working exactly as before, minus " +
			"the local AI features, which report themselves unavailable until the model is fetched again. It will " +
			"quietly re-download the full ~4GB unless you turn it off first: open chrome://on-device-internals and " +
			"use Uninstall, or set chrome://flags#optimization-guide-on-device-model to Disabled. No history, " +
			"cookies, passwords or extensions live here.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/OptGuideOnDeviceModel/*`,
			`# it comes back unless you disable it in chrome://on-device-internals`,
		},
	},
	"OptGuideOnDeviceClassifierModel": {
		Score: Safe,
		Description: "A much smaller companion to the on-device model: the classifier Chrome runs first to decide " +
			"whether a given page or request is worth handing to the full model. Typically a few hundred MB.",
		Effects: "Quit Chrome first. Re-downloaded automatically alongside the main on-device model. Nothing but " +
			"local AI features is affected, and no browsing data is stored here.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/OptGuideOnDeviceClassifierModel/*`,
		},
	},
	"optimization_guide_model_store": {
		Score: Safe,
		Description: "Downloaded models for Chrome's Optimization Guide — the service behind page-load hints, " +
			"password-change detection, phishing classification and similar on-device predictions. Distinct from " +
			"Gemini Nano next door: these are many small task-specific models rather than one large one.",
		Effects: "Quit Chrome first. Chrome re-downloads whichever models it still wants, in the background, over " +
			"the following days. In the meantime the affected predictions fall back to their default behaviour. " +
			"No browsing data is stored here.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/optimization_guide_model_store/*`,
		},
	},
	"component_crx_cache": {
		Score: Safe,
		Description: "Downloaded .crx archives for Chrome's own components — Widevine, the certificate revocation " +
			"list, Safe Browsing lists, the recovery component. This is the download cache, not the installed " +
			"components themselves, which are unpacked elsewhere in the profile.",
		Effects: "Quit Chrome first. Removes the archives only; every component stays installed and working. " +
			"Chrome re-fetches an archive if it ever needs to reinstall or roll back a component.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/component_crx_cache/*`,
		},
	},
	"GrShaderCache": {
		Score: Safe,
		Description: "Compiled GPU shader programs for Chrome's Skia/Ganesh renderer, cached so pages that use " +
			"the same drawing operations don't pay the compile cost twice.",
		Effects: "Quit Chrome first. Rebuilt transparently; the only cost is a brief shader recompile the first " +
			"time each page is drawn afterwards. Nothing about your profile is affected.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/GrShaderCache/*`,
		},
	},
	"GraphiteDawnCache": {
		Score: Safe,
		Description: "The same idea as GrShaderCache, for Chrome's newer Graphite/Dawn graphics backend: compiled " +
			"pipelines for the WebGPU-based renderer.",
		Effects: "Quit Chrome first. Rebuilt transparently, with a brief recompile on the next draw. No profile " +
			"data lives here.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/GraphiteDawnCache/*`,
		},
	},
	"ShaderCache": {
		Score:       Safe,
		Description: "Chrome's general GPU program cache, shared across profiles.",
		Effects: "Quit Chrome first. Rebuilt transparently; expect one brief recompile pause afterwards and " +
			"nothing else.",
		Commands: []string{
			`# Quit Chrome first`,
			`rm -rf ~/Library/Application\ Support/Google/Chrome/ShaderCache/*`,
		},
	},
	"Chrome": {
		Score: Risky,
		Description: "The Chrome profile root. Your browsing history, cookies, saved passwords, extensions, " +
			"bookmarks and per-site data all live in the profile subfolders here (Default, Profile 1, ...). What " +
			"makes this folder large is none of that: it is the downloaded on-device models (Gemini Nano alone is " +
			"about 4GB) and the shader caches beside them.",
		Effects: "Nothing is offered at this level, because the disposable parts and the irreplaceable parts sit " +
			"side by side. Open this folder and clean the model and cache directories individually — " +
			"OptGuideOnDeviceModel is almost always the big one — or use the clean action on the parent Google " +
			"folder, which targets exactly those and leaves every profile alone.",
	},
}
