// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// examples are each wire command's examples for its help, the man page
// and docs/COMMANDS.md: real command lines, as typed after "bmdctl",
// the plainest first, by path. Not bmdwire's: atemwire has no CLI.
var examples = map[string][]string{
	"audio compressor": {
		"audio compressor 1301 --enabled --threshold -20 --ratio 4",
		"audio compressor cam1 --attack 2 --release 200",
	},
	"audio eq": {
		"audio eq 1301 1 --shape HighPass --frequency 80",
		"audio eq cam2 3 --frequency 2500 --gain -3 --q 2",
		"audio eq cam2 3 --enabled off",
	},
	"audio expander": {
		"audio expander 1301 --enabled --mode gate --threshold -45",
		"audio expander cam1 --range 20 --ratio 2",
	},
	"audio levels": {
		"audio levels on",
	},
	"audio limiter": {
		"audio limiter 1301 --enabled --threshold -6",
		"audio limiter cam1 --attack 1 --release 100",
	},
	"audio master": {
		"audio master --volume -6",
		"audio master --volume -inf",
		"audio master --eq --eq-gain 2",
	},
	"audio master-compressor": {
		"audio master-compressor --enabled --threshold -10 --ratio 3",
	},
	"audio master-eq": {
		"audio master-eq 1 --shape LowShelf --gain -2",
		"audio master-eq 6 --shape HighShelf --gain 1.5",
	},
	"audio master-limiter": {
		"audio master-limiter --enabled --threshold -1",
	},
	"audio strip": {
		"audio strip cam1 --volume -6",
		"audio strip 1301 --gain 12 --mix On",
		"audio strip cam2 --mix AudioFollowVideo",
	},
	"auto": {
		"auto",
		"auto --me 2",
	},
	"auto-video-mode": {
		"auto-video-mode on",
	},
	"aux": {
		"aux 1 pgm",
		"aux 2 cam4",
		"aux 3 cf1",
	},
	"camera": {
		"camera 1 0 3 --type fixed16 --data 0.5",
		"camera 2 1 2 --type int16 --data 5600,0",
		"camera 1 0 0 --relative --type fixed16 --data 0.05",
	},
	"classic-audio input": {
		"classic-audio input cam1 --afv",
		"classic-audio input cam2 --on --volume 32768",
		"classic-audio input cam3 --balance -5000",
	},
	"classic-audio levels": {
		"classic-audio levels on",
	},
	"classic-audio master": {
		"classic-audio master --volume 32768",
		"classic-audio master --afv",
	},
	"classic-audio monitor": {
		"classic-audio monitor --enabled --volume 32768",
		"classic-audio monitor --solo --solo-source cam2",
		"classic-audio monitor --dim",
	},
	"color": {
		"color 1 --hue 210 --saturation 80 --luma 40",
		"color 2 --sat 0 --luma 100",
	},
	"cut": {
		"cut",
		"cut --me 2",
	},
	"dsk auto": {
		"dsk auto",
		"dsk auto --dsk 2",
	},
	"dsk fill": {
		"dsk fill mp1",
		"dsk fill mp2 --dsk 2",
	},
	"dsk gain": {
		"dsk gain --premultiplied",
		"dsk gain --premultiplied off --clip 40 --gain 70",
		"dsk gain --invert --dsk 2",
	},
	"dsk key": {
		"dsk key mp1k",
		"dsk key mp2k --dsk 2",
	},
	"dsk mask": {
		"dsk mask --enabled --top -4",
		"dsk mask --enabled off --dsk 2",
	},
	"dsk on-air": {
		"dsk on-air on",
		"dsk on-air off --dsk 2",
	},
	"dsk rate": {
		"dsk rate 1:00",
		"dsk rate 0:15 --dsk 2",
	},
	"dsk tie": {
		"dsk tie on",
		"dsk tie off --dsk 2",
	},
	"ftb enable": {
		"ftb enable on",
	},
	"ftb rate": {
		"ftb rate 1:00",
	},
	"ftb toggle": {
		"ftb toggle",
	},
	"hyperdeck-slot": {
		"hyperdeck-slot 1 --address 192.168.10.50 --input cam4",
		"hyperdeck-slot 2 --address 0.0.0.0",
	},
	"input rename": {
		`input rename cam1 --long-name "Stage Left" --short-name STGL`,
		"input rename 2 --short-name WIDE",
	},
	"macro action": {
		"macro action stop-record",
		"macro action insert-user-wait",
		"macro action continue",
	},
	"macro record": {
		`macro record 5 "Lower third in"`,
		`macro record 6 Open --description "titles and music"`,
	},
	"macro sleep": {
		"macro sleep 25",
	},
	"media capture": {
		"media capture",
	},
	"media clear-unlocked": {
		"media clear-unlocked 5",
	},
	"media player": {
		"media player 1 --still 3",
		"media player 2 --clip 1",
	},
	"multiviewer input": {
		"multiviewer input 3 cam5",
		"multiviewer input 10 aux1 --multiviewer 2",
	},
	"multiviewer properties": {
		"multiviewer properties --swap",
		"multiviewer properties --layout 12 --multiviewer 2",
	},
	"preview": {
		"preview cam3",
		"preview col1 --me 2",
	},
	"program": {
		"program cam2",
		`program "Stage Left"`,
		"program mp1 --me 2",
	},
	"recording settings": {
		"recording settings --filename service-2026-10-11",
		"recording settings --in-camera",
	},
	"recording state": {
		"recording state on",
	},
	"startup clear": {
		"startup clear",
	},
	"startup save": {
		"startup save",
	},
	"streaming audio-bitrate": {
		"streaming audio-bitrate 128000 192000",
	},
	"streaming service": {
		"streaming service --name YouTube --key abcd-1234-efgh-5678",
		"streaming service --url rtmp://a.rtmp.youtube.com/live2",
		"streaming service --bitrate-min 4500000 --bitrate-max 6000000",
	},
	"streaming state": {
		"streaming state on",
	},
	"supersource box": {
		"supersource box 1 --source cam1 --enabled",
		"supersource box 2 --source cam2 --x 800 --size 500",
		"supersource box 2 --masked --left 4000 --right 4000",
	},
	"supersource properties": {
		"supersource properties --fill-source mp1 --key-source mp1k",
		"supersource properties --layer foreground --premultiplied",
	},
	"tbar": {
		"tbar 5000",
		"tbar 10000 --me 2",
	},
	"time-of-day": {
		"time-of-day",
		"time-of-day --time 2026-10-11T19:30:00Z",
	},
	"time-request": {
		"time-request",
	},
	"transition dip": {
		"transition dip --source col1 --rate 0:15",
		"transition dip --source black --me 2",
	},
	"transition dve": {
		"transition dve --style PushLeft --rate 0:20",
		"transition dve --style SqueezeTopLeft --reverse --flip-flop",
		"transition dve --fill-source mp1 --key-source mp1k --key-enable",
	},
	"transition mix": {
		"transition mix 1:00",
		"transition mix 0:12 --me 2",
	},
	"transition preview": {
		"transition preview on",
		"transition preview on --me 2",
	},
	"transition settings": {
		"transition settings --style mix --next background",
		"transition settings --style wipe --me 2",
	},
	"transition stinger": {
		"transition stinger --player 1 --duration 2:00 --trigger 0:12",
		"transition stinger --pre-roll 0:10 --rate 0:08",
		"transition stinger --premultiplied off --clip 50 --gain 70",
	},
	"transition wipe": {
		"transition wipe --pattern CircleIris --rate 1:00",
		"transition wipe --width 5 --source col1 --softness 20",
		"transition wipe --reverse --flip-flop",
	},
	"usk chroma": {
		"usk 1 chroma --foreground 0.6 --background 0.4",
		"usk 1 chroma --spill 0.5 --flare 0.3",
		"usk 2 chroma --key-edge 0.5 --me 2",
	},
	"usk chroma-sample": {
		"usk 1 chroma-sample --cursor --x 0.2 --y 0.5",
		"usk 1 chroma-sample --preview",
		"usk 1 chroma-sample --cursor off --preview off",
	},
	"usk dve": {
		"usk 1 dve --size-x 0.4 --size-y 0.4",
		"usk 1 dve --position-x 8 --position-y 4.5",
		"usk 1 dve --border --shadow --rate 0:20",
	},
	"usk fill": {
		"usk 1 fill cam3",
		"usk 2 fill mp1 --me 2",
	},
	"usk key": {
		"usk 1 key mp1k",
		"usk 2 key cam4 --me 2",
	},
	"usk keyframe-run": {
		"usk 1 keyframe-run a",
		"usk 1 keyframe-run full",
		"usk 1 keyframe-run runToInfinite --direction MiddleCentre",
	},
	"usk keyframe-set": {
		"usk 1 keyframe-set a",
		"usk 1 keyframe-set b --me 2",
	},
	"usk luma": {
		"usk 1 luma --clip 40 --gain 60",
		"usk 1 luma --premultiplied",
		"usk 2 luma --invert --me 2",
	},
	"usk mask": {
		"usk 1 mask --enabled --left -8 --right 8",
		"usk 1 mask --enabled off",
	},
	"usk on-air": {
		"usk 1 on-air on",
		"usk 2 on-air off --me 2",
	},
	"usk pattern": {
		"usk 1 pattern --pattern CircleIris --size 40",
		"usk 1 pattern --softness 20 --position-x 0.75",
	},
	"usk type": {
		"usk 1 type --type chroma",
		"usk 1 type --type dve --fly",
	},
	"video-mode": {
		"video-mode 1080p50",
		"video-mode 1080i5994",
	},
}
