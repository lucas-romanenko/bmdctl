# bmdctl commands

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

A command's required arguments follow its words, in the order they're
said (`<name>`; `[name]` may be left out); everything optional is a
`--name value` flag, which may be shortened to any unambiguous prefix.
**Unverified** commands have a pinned wire format that nobody has confirmed on a
switcher; **destructive** ones are hard to undo or drop connections. The
echo is the state the device reports back once it has applied the
command; a command without one can't be confirmed. Its key names the
state as the device's library does, so an ATEM's indexes in it count
from 0.

## The command line

```
bmdctl [HOST] [--host H] [--json|--quiet] [--yes] [atem] <command> [arguments] [--flag value ...]
```

### Flags, accepted anywhere on the line

- `--host H`: The device: a name or alias from `bmdctl hosts` in any case, any unambiguous start of one, or host[:port] (UDP 9910 by default). Without it: $BMDCTL_HOST, else the host the last --host named, else the default host.
- `--json`: Print the engine's JSON response as it is: {ok, confirmed, result, took_ms} or {ok:false, code, error}, one per line.
- `--quiet, -q`: Print nothing on stdout; the exit status tells. Errors still go to stderr.
- `--yes, -y`: Allow a destructive command.
- `--dry-run`: Print a wire command's bytes, header included, and send nothing.
- `--fps N`: The frame rate for rates given as seconds:frames (default: the switcher's display rate; 25 with --dry-run).
- `--timeout D`: How long to wait for the switcher's echo (default 5s), or for a transfer to finish (default: the library's).
- `--no-color`: No colour, even on a terminal.
- `--timestamps, -t`: At a terminal, start each result line with the time of day (HH:MM:SS), dim.
- `--help, -h`: Help for the command line so far.

### Built-in commands

- `bmdctl`: Show the engine (running or not, its sessions, the git commit it runs) and a line per host: its alias, its model, video mode, how long its session has been open and idle (or that a watch or a subscription is using it), whether it is pinned or stale (idle past its timeout, not yet closed), and which is the default, where a command that names no host goes now. At a terminal: bmdctl's version, then a line per switcher, the default's marked *. Never starts the engine.
- `bmdctl help`: Print the help.
- `bmdctl version`: Print bmdctl's version; at a terminal also the commit it was built from, the hosts file and the engine.
- `bmdctl engine [--log-level L]`: Run the background engine in the foreground, for systemd or Docker, logging to stderr and its log file. Any command starts one in the background when none is running, and restarts one it started from another build of bmdctl; one run with bmdctl engine answers every build.
- `bmdctl engine --serve [--listen ADDRESS]`: Run the engine in the foreground and serve every command over HTTP/JSON from its process, as bmdctl serve does (127.0.0.1:8880 unless --listen says otherwise): a container's command.
- `bmdctl engine stop`: Stop the running engine; its sessions say goodbye to their switchers.
- `bmdctl engine status`: Show the engine's version, build, pid, socket, log, how long a pinned session may go idle, and its sessions: each one's age, idle time, whether pinned, and when it closes, or stale.
- `bmdctl hosts [list]`: List the hosts.
- `bmdctl hosts add ADDRESS [--as NAME] [--device D]`: Add the device at ADDRESS by the name it gives itself, as the device that answers there (an ATEM, HyperDeck, Videohub or Ultimatte; --device says it instead): an ATEM's name as set in ATEM Setup, else its model; the others' as they report it. --as gives it another, for two devices with one name. An address added again is asked again.
- `bmdctl hosts discover [--for D]`: Add every switcher that announces itself on the network (Bonjour, _switcher_ctrl._udp) within D (2s), by its own name.
- `bmdctl hosts rm NAME`: Forget a host.
- `bmdctl hosts alias NAME SHORT`: Give a host a short name it goes by as well as its own, wherever a name is taken: one word, not a command's, not another host's name or alias. Another alias replaces it; --rm instead of SHORT takes it away.
- `bmdctl hosts default [NAME]`: Show the default host, or make NAME the default and the host commands go to now.
- `bmdctl use HOST`: Make HOST the default, the host commands that name none go to from now on, without opening a session (`connect HOST` pins one too): `hosts default HOST` in two words. HOST is a host's name or alias in any case, the start of either, or its address. $BMDCTL_HOST, when set, still wins, and it says so.
- `bmdctl exec FILE|- [--continue]`: Run one command per line, in order, to one host. Every line is checked before the first is sent; it stops at the first failure unless --continue.
- `bmdctl completion bash|zsh|fish`: Print a shell completion script: commands, flags, host names and, while the engine runs, the switcher's input names and the indexes it has, counted from 1.
- `bmdctl man [--dir DIR]`: Print the bmdctl(1) manual page, or write it to DIR/bmdctl.1.
- `bmdctl doctor`: Check the engine, its socket and log, the hosts file and that each host answers, and say what is misconfigured. Exit 1 for a misconfiguration, 2 when a host only didn't answer.
- `bmdctl mcp`: Run a Model Context Protocol server on stdin and stdout for an AI agent: a tool per command group (atem_switching, atem_media, hyperdeck_transport, ...) whose subcommand argument is the command and whose other arguments are its, plus list_hosts, wait_for_change (a long poll for the next change, with a cursor so none is missed), and each host's state as a resource to subscribe to. Results say confirmed, sent, already or rejected, with what changed. A destructive subcommand does nothing without confirm: true and says what it would affect. docs/AGENTS.md says how to add it to Claude Code and Claude Desktop.
- `bmdctl serve [--listen ADDRESS]`: Serve every command over HTTP/JSON until interrupted, through the engine: GET /hosts, GET /hosts/{host}/state, POST /hosts/{host}/{command} (the command's words as the path, its arguments a JSON object, multipart/form-data for a file; ?yes=true for a destructive one), GET /hosts/{host}/events (server-sent events: watch's frames) and GET /openapi.json. The answer is the engine's, as --json prints it. It listens on 127.0.0.1:8880 unless --listen says otherwise, and has no authentication: whoever can reach it controls the devices.

### Conventions

- Host: A first word that isn't a command names the host, for that command alone: `bmdctl bc cut` is `bmdctl --host bc cut`, except that it doesn't become the host commands go to after (`bmdctl use HOST` does that). It is a host's name or alias in any case, the start of either, the name it had before a rename, its address, or any IP address; a word that is a command (a built-in, a device's word, any device's command) is always the command, and --host, given too, wins. A first word that is neither is refused, listing the hosts. After a device's word (`bmdctl atem bc cut`, `hdctl deck play`) the next word is taken the same way.
- Arguments: A command's required arguments follow its words in the order they're said: `program 5`, `usk 1 fill cam3`, `media upload 10 logo.png`. Optional ones are --name flags, which may be shortened to any unambiguous prefix (--sat for --saturation); `[file]` in a syntax may be left out.
- Devices: A command without a device word goes to the device type of its host, an ATEM for an address no host has; a command that device doesn't have says what the host is (`DECK 1 is a HyperDeck; bmdctl hyperdeck --help`). `bmdctl atem ...` and `bmdctl hyperdeck ...` name the device. `connect ADDRESS` asks what answers there, an ATEM (UDP 9910), a HyperDeck (TCP 9993), a Videohub (TCP 9990) or an Ultimatte (TCP 9998), all at once for 2 s, and saves it by its own name; --type says it instead. `bmdctl help` lists the device groups hosts.yml has a host of.
- Aliases: Run as atemctl, hdctl, vhctl or ultctl, bmdctl is that device's command: `hdctl play` is `bmdctl hyperdeck play`, `atemctl cut` is `bmdctl atem cut`, and help lists that device's commands alone. Built-ins (version, hosts, exec, doctor, ...) are bmdctl's. Releases ship them as symlinks to bmdctl (copies on Windows), and install.sh links them.
- Hosts: A host goes by the name its switcher gives itself, unless --as gave it one, and by its alias as well, if it has one. When a switcher is renamed in ATEM Setup, the next command to it renames its host in hosts.yml and says so; --host still takes the name it had.
- Output: A command that changes something prints one line: the switcher as `name (address)`, the command and where (ME1, `usk 2`), what changed as before → after, `confirmed` (the switcher reported it back) or `sent` (it has no such report), and the time. A command that reads prints a line naming the switcher, its model and video mode, then what it read. Opening a session prints `connecting to ...` and then the model and video mode on stderr. --json, --quiet and a pipe get exactly that. At a terminal the switcher is named once, in a bold header that also says what is on program and preview (and, when the command opened the session, the model, video mode and how long connecting took); each result is indented under it as the verb, before → after and the time, right-aligned; `confirmed` is never printed, and `sent`, `rejected` (the switcher kept what it held) take the time's place. Nothing is wider than 72 columns. Repeats collapse (`aux 1–6  PGM`, empty slots counted, not listed; --all lists them), an error is `error` then the message and, under it, what to do next, a wait of more than 200 ms shows a spinner on stderr that the result replaces (a transfer of known size, a progress bar; a profile, a line per step as it finishes), and a destructive command says what it would affect and asks [y/N] when stdin is a terminal too; -t starts each result with the time of day. Colour only on a terminal, in red, green, bold and dim alone: the value now on program (and a keyer on air) red, on preview green, the switcher's name and bmdctl's own lines bold, ids, times, hints and empty slots dim.
- Indexes: M/Es, upstream and downstream keyers, aux outputs, media and macro slots, media players, colour generators, multiviewers and their windows, SuperSources and their boxes, HyperDeck slots and EQ bands count from 1, as ATEM Software Control and the panels count them: on the command line, in --help and in results, --json included. The atem library counts them from 0, and so do the switcher state `state` and `watch` print (and `state --path` reads) and the index attributes of ATEM Software Control's XML files.
- Sources: A source is found in this order, case ignored: a number is that input (or source id); cam2, camera 2, in2 and input 2 are input 2, and mp1, bars, black (blk), col1, pgm, pvw, cf1 and aux3 the switcher's fixed sources, whatever their names; else an input's short or long name; else the one input whose name starts so. Where an alias is also an input's name the alias wins, and a terminal says so once. A source nothing names is an error listing the first inputs.
- Rates: A rate is a frame count or seconds:frames at the switcher's display rate (half the field rate from 48 fps up).

### Exit status

- 0: Done.
- 1: A usage error, or any other failure.
- 2: The switcher or the engine is unreachable.
- 3: Sent, but the switcher did not confirm it in time.
- 4: The switcher refused it (a transfer, a lock), or a profile restore recorded errors.
- 5: Destructive, and --yes was not given.

### Environment

- `BMDCTL_HOST`: The host a command goes to when --host doesn't name one.
- `BMDCTL_PIN_IDLE`: How long a session `connect` pinned may go without a command before the engine closes it: a duration (90m, 2h) or seconds, 0 for never; an hour unless set. Read when the engine starts.
- `BMDCTL_SOCKET`: The engine's socket (a pipe name on Windows), instead of $XDG_RUNTIME_DIR/bmdctl.sock.
- `BMDCTL_STILL_CACHE_MB`: How much of the media pool's stills the engine keeps for profile saves, in megabytes: 1024 unless set, 0 for none. Read when the engine starts.
- `XDG_CONFIG_HOME, XDG_STATE_HOME, XDG_CACHE_HOME, XDG_RUNTIME_DIR`: Where hosts.yml, the engine's log and last host, the still cache, and the socket live.
- `NO_COLOR`: Set to anything: no colour, even on a terminal, as --no-color.
- `TERM`: `dumb`: no colour and no spinner.

### Files

- `~/.config/bmdctl/hosts.yml`: The named hosts and the default.
- `~/.local/state/bmdctl/engine.log`: The engine's log, rotated at 5 MiB, three old files kept.
- `~/.local/state/bmdctl/last-host`: The host the last --host named.
- `~/.cache/bmdctl/stills`: The media pool's stills at full size, one PNG per picture named by the switcher's MD5, which a profile save writes without downloading them again. The least recently used go past the size ($BMDCTL_STILL_CACHE_MB), and a still no save has used for 30 days. Safe to delete.
- `$XDG_RUNTIME_DIR/bmdctl.sock`: The engine's socket; a lock file beside it.

## atem

### [switching](commands/atem-switching.md)

What is on program, preview and the aux outputs: program, preview, cut, auto, the T-bar, fade to black, undo.

[`auto`](commands/atem-switching.md#bmdctl-atem-auto) · [`aux`](commands/atem-switching.md#bmdctl-atem-aux-aux-source) · [`cut`](commands/atem-switching.md#bmdctl-atem-cut) · [`ftb enable`](commands/atem-switching.md#bmdctl-atem-ftb-enable-enabled) · [`ftb rate`](commands/atem-switching.md#bmdctl-atem-ftb-rate-rate) · [`ftb toggle`](commands/atem-switching.md#bmdctl-atem-ftb-toggle) · [`preview`](commands/atem-switching.md#bmdctl-atem-preview-source) · [`program`](commands/atem-switching.md#bmdctl-atem-program-source) · [`tbar`](commands/atem-switching.md#bmdctl-atem-tbar-position) · [`undo`](commands/atem-switching.md#bmdctl-atem-undo)

### [transition](commands/atem-transition.md)

The next transition: its style and rate, each style's settings (mix, dip, wipe, DVE, stinger), what it takes, preview.

[`transition dip`](commands/atem-transition.md#bmdctl-atem-transition-dip) · [`transition dve`](commands/atem-transition.md#bmdctl-atem-transition-dve) · [`transition mix`](commands/atem-transition.md#bmdctl-atem-transition-mix-rate) · [`transition next`](commands/atem-transition.md#bmdctl-atem-transition-next) · [`transition preview`](commands/atem-transition.md#bmdctl-atem-transition-preview-enabled) · [`transition rate`](commands/atem-transition.md#bmdctl-atem-transition-rate-rate) · [`transition settings`](commands/atem-transition.md#bmdctl-atem-transition-settings) · [`transition stinger`](commands/atem-transition.md#bmdctl-atem-transition-stinger) · [`transition style`](commands/atem-transition.md#bmdctl-atem-transition-style-style) · [`transition wipe`](commands/atem-transition.md#bmdctl-atem-transition-wipe)

### [usk](commands/atem-usk.md)

Upstream keyers: fill and key, type (luma, chroma, pattern, DVE) and its settings, mask, fly keyframes, on air.

[`usk chroma`](commands/atem-usk.md#bmdctl-atem-usk-keyer-chroma) · [`usk chroma-sample`](commands/atem-usk.md#bmdctl-atem-usk-keyer-chroma-sample) · [`usk dve`](commands/atem-usk.md#bmdctl-atem-usk-keyer-dve) · [`usk fill`](commands/atem-usk.md#bmdctl-atem-usk-keyer-fill-source) · [`usk key`](commands/atem-usk.md#bmdctl-atem-usk-keyer-key-source) · [`usk keyframe-run`](commands/atem-usk.md#bmdctl-atem-usk-keyer-keyframe-run-to) · [`usk keyframe-set`](commands/atem-usk.md#bmdctl-atem-usk-keyer-keyframe-set-keyframe) · [`usk luma`](commands/atem-usk.md#bmdctl-atem-usk-keyer-luma) · [`usk mask`](commands/atem-usk.md#bmdctl-atem-usk-keyer-mask) · [`usk on-air`](commands/atem-usk.md#bmdctl-atem-usk-keyer-on-air-on-air) · [`usk pattern`](commands/atem-usk.md#bmdctl-atem-usk-keyer-pattern) · [`usk toggle`](commands/atem-usk.md#bmdctl-atem-usk-keyer-toggle) · [`usk type`](commands/atem-usk.md#bmdctl-atem-usk-keyer-type)

### [dsk](commands/atem-dsk.md)

Downstream keyers: fill and key, gain, mask, rate, tie, on air, auto.

[`dsk auto`](commands/atem-dsk.md#bmdctl-atem-dsk-auto) · [`dsk fill`](commands/atem-dsk.md#bmdctl-atem-dsk-fill-source) · [`dsk gain`](commands/atem-dsk.md#bmdctl-atem-dsk-gain) · [`dsk key`](commands/atem-dsk.md#bmdctl-atem-dsk-key-source) · [`dsk mask`](commands/atem-dsk.md#bmdctl-atem-dsk-mask) · [`dsk on-air`](commands/atem-dsk.md#bmdctl-atem-dsk-on-air-on-air) · [`dsk rate`](commands/atem-dsk.md#bmdctl-atem-dsk-rate-rate) · [`dsk tie`](commands/atem-dsk.md#bmdctl-atem-dsk-tie-tie) · [`dsk toggle`](commands/atem-dsk.md#bmdctl-atem-dsk-toggle)

### [audio](commands/atem-audio.md)

The audio mixer: Fairlight strips, EQ, dynamics, the master and meters; the classic mixer's (classic-audio ...).

[`audio compressor`](commands/atem-audio.md#bmdctl-atem-audio-compressor-source) · [`audio eq`](commands/atem-audio.md#bmdctl-atem-audio-eq-source-band) · [`audio expander`](commands/atem-audio.md#bmdctl-atem-audio-expander-source) · [`audio levels`](commands/atem-audio.md#bmdctl-atem-audio-levels-enable) · [`audio limiter`](commands/atem-audio.md#bmdctl-atem-audio-limiter-source) · [`audio master`](commands/atem-audio.md#bmdctl-atem-audio-master) · [`audio master-compressor`](commands/atem-audio.md#bmdctl-atem-audio-master-compressor) · [`audio master-eq`](commands/atem-audio.md#bmdctl-atem-audio-master-eq-band) · [`audio master-limiter`](commands/atem-audio.md#bmdctl-atem-audio-master-limiter) · [`audio meters`](commands/atem-audio.md#bmdctl-atem-audio-meters) · [`audio strip`](commands/atem-audio.md#bmdctl-atem-audio-strip-source) · [`classic-audio input`](commands/atem-audio.md#bmdctl-atem-classic-audio-input-source) · [`classic-audio levels`](commands/atem-audio.md#bmdctl-atem-classic-audio-levels-enable) · [`classic-audio master`](commands/atem-audio.md#bmdctl-atem-classic-audio-master) · [`classic-audio meters`](commands/atem-audio.md#bmdctl-atem-classic-audio-meters) · [`classic-audio monitor`](commands/atem-audio.md#bmdctl-atem-classic-audio-monitor)

### [media](commands/atem-media.md)

The media pool and players: stills uploaded to and downloaded from slots, cleared, what each player shows.

[`media capture`](commands/atem-media.md#bmdctl-atem-media-capture) · [`media clear`](commands/atem-media.md#bmdctl-atem-media-clear-slot) · [`media clear-unlocked`](commands/atem-media.md#bmdctl-atem-media-clear-unlocked-slot) · [`media download`](commands/atem-media.md#bmdctl-atem-media-download-slot-file) · [`media list`](commands/atem-media.md#bmdctl-atem-media-list) · [`media player`](commands/atem-media.md#bmdctl-atem-media-player-player) · [`media upload`](commands/atem-media.md#bmdctl-atem-media-upload-slot-file)

### [macro](commands/atem-macro.md)

Macros: run, stop, record, list, upload and download, decode and encode.

[`macro action`](commands/atem-macro.md#bmdctl-atem-macro-action-action) · [`macro decode`](commands/atem-macro.md#bmdctl-atem-macro-decode-file) · [`macro delete`](commands/atem-macro.md#bmdctl-atem-macro-delete-slot) · [`macro download`](commands/atem-macro.md#bmdctl-atem-macro-download-slot-file) · [`macro encode`](commands/atem-macro.md#bmdctl-atem-macro-encode-file-out) · [`macro list`](commands/atem-macro.md#bmdctl-atem-macro-list) · [`macro record`](commands/atem-macro.md#bmdctl-atem-macro-record-slot-name) · [`macro run`](commands/atem-macro.md#bmdctl-atem-macro-run-slot) · [`macro sleep`](commands/atem-macro.md#bmdctl-atem-macro-sleep-frames) · [`macro stop`](commands/atem-macro.md#bmdctl-atem-macro-stop) · [`macro upload`](commands/atem-macro.md#bmdctl-atem-macro-upload-slot-file)

### [profile](commands/atem-profile.md)

ATEM Software Control profiles: save the switcher's setup, restore one, download its stills.

[`profile images`](commands/atem-profile.md#bmdctl-atem-profile-images) · [`profile restore`](commands/atem-profile.md#bmdctl-atem-profile-restore-file) · [`profile save`](commands/atem-profile.md#bmdctl-atem-profile-save-file)

### [sources](commands/atem-sources.md)

Inputs and sources: the list, renames, colour generators, camera control, HyperDeck slots.

[`camera`](commands/atem-sources.md#bmdctl-atem-camera-destination-category-parameter) · [`color`](commands/atem-sources.md#bmdctl-atem-color-generator) · [`hyperdeck-slot`](commands/atem-sources.md#bmdctl-atem-hyperdeck-slot-slot) · [`input rename`](commands/atem-sources.md#bmdctl-atem-input-rename-source) · [`inputs`](commands/atem-sources.md#bmdctl-atem-inputs)

### [outputs](commands/atem-outputs.md)

Multiviewers, SuperSource, recording and streaming.

[`multiviewer input`](commands/atem-outputs.md#bmdctl-atem-multiviewer-input-window-source) · [`multiviewer properties`](commands/atem-outputs.md#bmdctl-atem-multiviewer-properties) · [`recording settings`](commands/atem-outputs.md#bmdctl-atem-recording-settings) · [`recording state`](commands/atem-outputs.md#bmdctl-atem-recording-state-recording) · [`streaming audio-bitrate`](commands/atem-outputs.md#bmdctl-atem-streaming-audio-bitrate-min-max) · [`streaming service`](commands/atem-outputs.md#bmdctl-atem-streaming-service) · [`streaming state`](commands/atem-outputs.md#bmdctl-atem-streaming-state-streaming) · [`supersource box`](commands/atem-outputs.md#bmdctl-atem-supersource-box-box) · [`supersource properties`](commands/atem-outputs.md#bmdctl-atem-supersource-properties)

### [system](commands/atem-system.md)

The switcher itself: what it is, its video mode, startup state, time.

[`auto-video-mode`](commands/atem-system.md#bmdctl-atem-auto-video-mode-enable) · [`info`](commands/atem-system.md#bmdctl-atem-info) · [`startup clear`](commands/atem-system.md#bmdctl-atem-startup-clear) · [`startup save`](commands/atem-system.md#bmdctl-atem-startup-save) · [`time`](commands/atem-system.md#bmdctl-atem-time) · [`time-of-day`](commands/atem-system.md#bmdctl-atem-time-of-day) · [`time-request`](commands/atem-system.md#bmdctl-atem-time-request) · [`video-mode`](commands/atem-system.md#bmdctl-atem-video-mode-mode) · [`video-modes`](commands/atem-system.md#bmdctl-atem-video-modes)

### [session](commands/atem-session.md)

The session and reads: connect, disconnect, status (what is on air), show one area, the raw state, watch changes.

[`connect`](commands/atem-session.md#bmdctl-atem-connect-address) · [`disconnect`](commands/atem-session.md#bmdctl-atem-disconnect) · [`show`](commands/atem-session.md#bmdctl-atem-show-area) · [`state`](commands/atem-session.md#bmdctl-atem-state) · [`status`](commands/atem-session.md#bmdctl-atem-status) · [`watch`](commands/atem-session.md#bmdctl-atem-watch)

## hyperdeck

### [transport](commands/hyperdeck-transport.md)

The transport: play, stop, record, loop, next and previous clip, goto a clip or a timecode, the active slot.

[`goto`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-goto-clip) · [`loop`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-loop-on) · [`next`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-next) · [`play`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-play) · [`prev`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-prev) · [`record`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-record-name) · [`slot`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-slot-slot) · [`stop`](commands/hyperdeck-transport.md#bmdctl-hyperdeck-stop)

### [session](commands/hyperdeck-session.md)

The session and reads: status (transport, timecode, clip, slots, time left), the timeline's or a disk's clips, the raw state, watch changes, connect, disconnect.

[`clips`](commands/hyperdeck-session.md#bmdctl-hyperdeck-clips) · [`connect`](commands/hyperdeck-session.md#bmdctl-hyperdeck-connect-address) · [`disconnect`](commands/hyperdeck-session.md#bmdctl-hyperdeck-disconnect) · [`state`](commands/hyperdeck-session.md#bmdctl-hyperdeck-state) · [`status`](commands/hyperdeck-session.md#bmdctl-hyperdeck-status) · [`watch`](commands/hyperdeck-session.md#bmdctl-hyperdeck-watch)
