// SPDX-License-Identifier: MIT

package engine

// opExamples are each operation's examples for its help, the man page
// and docs/COMMANDS.md: real command lines, as typed after "bmdctl",
// the plainest first, by path.
var opExamples = map[string][]string{
	"audio meters": {
		"audio meters",
		"audio meters --for 3",
	},
	"classic-audio meters": {
		"classic-audio meters",
		"classic-audio meters --for 3",
	},
	"connect": {
		"connect 192.168.10.240",
		"connect",
		"connect --pin off",
	},
	"disconnect": {
		"disconnect",
	},
	"dsk toggle": {
		"dsk toggle",
		"dsk toggle --dsk 2",
	},
	"info": {
		"info",
	},
	"inputs": {
		"inputs",
		"inputs --all",
	},
	"macro decode": {
		"macro decode open.bin",
		"macro decode open.bin --json",
	},
	"macro delete": {
		"macro delete 4",
		`macro delete "Lower third in"`,
	},
	"macro download": {
		"macro download 1",
		"macro download 1 open.xml",
		"macro download 1 - --format json",
	},
	"macro encode": {
		"macro encode open.xml open.bin",
	},
	"macro list": {
		"macro list",
		"macro list --all",
	},
	"macro run": {
		"macro run 1",
		`macro run "Lower third in"`,
	},
	"macro stop": {
		"macro stop",
	},
	"macro upload": {
		"macro upload 5 open.xml",
		"macro upload 6 open.bin --name Open",
	},
	"media clear": {
		"media clear 5",
	},
	"media download": {
		"media download 3",
		"media download 3 logo.png",
	},
	"media list": {
		"media list",
		"media list --all",
	},
	"media upload": {
		"media upload 3 logo.png",
		`media upload 4 sponsor.png --name "Main Sponsor"`,
		"media upload 5 lower-third.png --premultiply off",
	},
	"profile images": {
		"profile images",
		"profile images --dir stills/",
	},
	"profile restore": {
		"profile restore show.xml",
		"profile restore show.xml --program --preview",
		"profile restore show.xml --macros-only",
	},
	"profile save": {
		"profile save show.xml",
		"profile save show.xml --media-pool-images off",
	},
	"state": {
		"state",
		"state --path mes.0.program",
		"state --path fairlight.master",
	},
	"status": {
		"status",
	},
	"time": {
		"time",
	},
	"transition next": {
		"transition next --next background,key1",
		"transition next --toggle key1",
	},
	"transition rate": {
		"transition rate 0:20",
		"transition rate 30 --me 2",
	},
	"transition style": {
		"transition style mix",
		"transition style stinger --me 2",
	},
	"usk toggle": {
		"usk 1 toggle",
		"usk 2 toggle --me 2",
	},
	"video-modes": {
		"video-modes",
	},
	"watch": {
		"watch",
		"watch --filter PrgI,PrvI",
		"watch --filter PrgI --count 1",
	},
	"show": {
		"show keys",
		"show transition",
		"show audio",
	},
	"undo": {
		"undo",
	},
}
