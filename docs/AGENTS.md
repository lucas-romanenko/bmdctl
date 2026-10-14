# bmdctl for AI agents

bmdctl gives an agent the same control a person at the command line has:
every command of the command table, run through bmdctl's engine, on the
devices hosts.yml names. There are two ways in, and they can be used
together:

- **`bmdctl mcp`**, a Model Context Protocol server: fifteen tools, one
  per command group (`atem_switching`, `atem_usk`, `atem_media`,
  `hyperdeck_transport`, ...) with the command as its `subcommand`, plus
  `list_hosts`, and each host's state as a resource the agent can
  subscribe to.
- **The skill** in `skills/bmdctl/`, which tells the agent how to work a
  live show safely: read before writing, say what is on air before a cut,
  ask a person before anything destructive, and the recipes for common
  jobs. It works with the MCP tools or with the `bmdctl` command itself.

bmdctl must be installed ([INSTALL.md](INSTALL.md)) and the devices added once
(`bmdctl connect 192.168.1.20` saves one and makes it the default). The
engine starts by itself the first time a tool runs.

## The MCP server

### Claude Code

One line:

```sh
claude mcp add --transport stdio bmdctl -- bmdctl mcp
```

`--scope user` makes it available in every project; the default,
`local`, is this project only, and `project` writes it to the project's
`.mcp.json` for everyone who clones it, as this:

```json
{
  "mcpServers": {
    "bmdctl": {
      "type": "stdio",
      "command": "bmdctl",
      "args": ["mcp"]
    }
  }
}
```

`claude mcp list` shows whether it connected; inside a session, `/mcp`
does. It has been run this way against a switcher
([HARDWARE.md](HARDWARE.md)).

### Claude Desktop

Settings > Developer > Edit Config opens `claude_desktop_config.json`
(`~/Library/Application Support/Claude/` on macOS, `%APPDATA%\Claude\`
on Windows). Add the server and restart Claude Desktop:

```json
{
  "mcpServers": {
    "bmdctl": {
      "command": "/Users/you/.local/bin/bmdctl",
      "args": ["mcp"]
    }
  }
}
```

A desktop app doesn't see the PATH your shell has, so give the binary's
full path (`command -v bmdctl` prints it; on Windows with Scoop,
`C:\\Users\\you\\scoop\\shims\\bmdctl.exe`, backslashes doubled in JSON).
The server's own messages are in `mcp-server-bmdctl.log` under
`~/Library/Logs/Claude/` (`%APPDATA%\Claude\logs\` on Windows). Claude
Desktop hasn't been tried with bmdctl yet
([HARDWARE.md](HARDWARE.md#unverified)).

### What the tools do

`bmdctl program cam2` is the tool call `atem_switching {"subcommand":
"program", "source": "cam2"}`, and its result says `confirmed`, `sent`,
`already` or `rejected`, with each value before and after.

- **A tool per group.** The ATEM's are `atem_switching` (program,
  preview, cut, auto, tbar, undo, aux, ftb), `atem_transition`,
  `atem_usk`, `atem_dsk`, `atem_audio`, `atem_media`, `atem_macro`,
  `atem_profile`, `atem_sources` (inputs, renames, colour generators,
  camera control), `atem_outputs` (multiviewer, SuperSource, recording,
  streaming), `atem_system` (info, video mode, startup, time) and
  `atem_session` (status, state, show, watch, connect, disconnect); a
  HyperDeck's are `hyperdeck_transport` and `hyperdeck_session`. Each
  takes `subcommand`, the command less the group's word (`bmdctl
  transition wipe --rate 25` is `atem_transition {"subcommand": "wipe",
  "rate": 25}`; `bmdctl ftb toggle` is `atem_switching {"subcommand":
  "ftb toggle"}`), that command's arguments (ranges, units, defaults;
  indexes count from 1, as ATEM Software Control counts them; each
  argument's description says which subcommands take it) and `host`: a
  host's name from `list_hosts`, the start of one, or an address, the
  default host when left out. The tool's description lists its
  subcommands with their usage. An argument the subcommand doesn't
  take is an error naming what it does take; a switcher's tool sent to
  a HyperDeck (or the other way) says so.
- **Results say whether anything changed**: `status` is `confirmed` (the
  device reported it back), `sent` (no confirmation exists for that
  command), `already` (the device held it, so nothing was sent) or
  `rejected` (it kept its value); `changed` lists each value before and
  after; reads say `done`. A failure is an error result with the engine's
  code (`usage`, `unreachable`, `no_echo`, `rejected`, `destructive`) and
  message, for the agent to act on.
- **Destructive subcommands** (`atem_system` `video-mode`, `startup
  save` and `startup clear`; `atem_media` `clear` and `clear-unlocked`;
  `atem_macro` `delete`; `atem_profile` `restore`) do nothing without
  `confirm: true`, and their tools are annotated destructive: they
  answer with what they would affect (`video mode 1080p50 → 525i5994:
  every output resyncs`) for the agent to put to a person. Uploading
  over a used still or macro slot, recording a macro into one, and a
  macro `action` delete ask the same way.
- **Files** are on the machine running `bmdctl mcp`: `atem_media`
  `upload` reads the path it is given, downloads write where `file`
  says.
- **Watching**: subscribe to `bmdctl://hosts/<host>/state` and the server
  sends a notification on each change (at most four a second); read the
  resource for the state. The `watch` subcommand of `atem_session` and
  `hyperdeck_session` collects a few `seconds` of changes and returns
  them, for an agent without subscriptions.
- **Both protocol eras**: the server answers clients of the current
  revision (2026-07-28, no handshake) and of the `initialize` revisions
  before it.

No authentication is involved: the server speaks only to the program
that started it, over stdin and stdout.

## The skill

`skills/bmdctl/SKILL.md` teaches an agent to use bmdctl on a live show:
when to use it, to read the state before writing and say what is on air
before a cut, which commands are destructive and that a person, not the
agent, says yes to them, why sequences go through `exec`, how to read
`watch --json`, five recipes (keying a graphic, a pre-show reset, a
scheduled content change, tally-driven routing, a HyperDeck playback
cue), and that untested models need checking after each change. Install
it beside the MCP server:

- **Claude Code**, for every project: copy the directory to your skills,

  ```sh
  mkdir -p ~/.claude/skills && cp -r skills/bmdctl ~/.claude/skills/
  ```

  or, for one project, to its `.claude/skills/` (commit it, and everyone
  who clones the project has it). Claude loads it when a request matches
  its description.
- **Claude Desktop and claude.ai**: zip the directory with `bmdctl/` at
  the zip's top (`cd skills && zip -r bmdctl.zip bmdctl`), then in
  Customize > Skills choose +, Create skill, Upload a skill, pick the zip
  and turn it on. Skills need code execution on (Settings > Capabilities).

The skill names the commands as the CLI types them; with the MCP server
the same command is its group's tool with the rest as `subcommand`
(`bmdctl dsk on-air on` is `atem_dsk` with `{"subcommand": "on-air",
"on-air": true}`), and the skill says so. A test in
`cmd/bmdctl/skill_test.go` checks that every command line in it is one
bmdctl accepts.

## The HTTP API

For an agent framework that speaks HTTP rather than MCP, `bmdctl serve`
serves the same commands as JSON on 127.0.0.1:8880, described by
`GET /openapi.json` (generated from the same table). It has no
authentication either: keep it on localhost.
