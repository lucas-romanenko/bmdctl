# bmdctl for AI agents

bmdctl gives an agent the same control a person at the command line has:
every command of the command table, run through bmdctl's engine, on the
devices hosts.yml names. There are two ways in, and they can be used
together:

- **`bmdctl mcp`**, a Model Context Protocol server: a tool per command
  (`atem_program`, `atem_usk_on_air`, `hyperdeck_play`, ...), plus
  `list_hosts`, `get_state` and `list_inputs`, and each host's state as a
  resource the agent can subscribe to.
- **The skill** in `skills/bmdctl/`, which tells the agent how to work a
  live show safely: read before writing, say what is on air before a cut,
  ask a person before anything destructive, and the recipes for common
  jobs. It works with the MCP tools or with the `bmdctl` command itself.

bmdctl must be installed (see README.md) and the devices added once
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
`.mcp.json` for everyone who clones it. `claude mcp list` shows whether it
connected; inside a session, `/mcp` does.

### Claude Desktop

In Settings > Developer > Edit Config (the file is
`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS
and `%APPDATA%\Claude\claude_desktop_config.json` on Windows), add:

```json
{
  "mcpServers": {
    "bmdctl": { "command": "bmdctl", "args": ["mcp"] }
  }
}
```

and restart Claude Desktop. A desktop app may not see the PATH your shell
has, so if the server doesn't start, give the binary's full path instead
of `bmdctl` (`which bmdctl` prints it: `/Users/you/.local/bin/bmdctl`, or
on Windows `C:\\Users\\you\\scoop\\shims\\bmdctl.exe`). The server's own
messages are in `mcp-server-bmdctl.log` under `~/Library/Logs/Claude/`
(`%APPDATA%\Claude\logs\` on Windows).

### What the tools do

- **Names** are the device and the command's words: `atem_program`,
  `atem_transition_wipe`, `atem_media_upload`, `hyperdeck_goto`. Each
  takes the command's arguments as its input schema (ranges, units,
  defaults; indexes count from 1, as ATEM Software Control counts them)
  and `host`: a host's name from `list_hosts`, the start of one, or an
  address, the default host when left out. A switcher's tool sent to a
  HyperDeck (or the other way) says so.
- **Results say whether anything changed**: `status` is `confirmed` (the
  device reported it back), `sent` (no confirmation exists for that
  command), `already` (the device held it, so nothing was sent) or
  `rejected` (it kept its value); `changed` lists each value before and
  after; reads say `done`. A failure is an error result with the engine's
  code (`usage`, `unreachable`, `no_echo`, `rejected`, `destructive`) and
  message, for the agent to act on.
- **Destructive tools** (`atem_video_mode`, `atem_startup_save`,
  `atem_startup_clear`, `atem_media_clear`, `atem_media_clear_unlocked`,
  `atem_macro_delete`, `atem_profile_restore`) are annotated so, and do
  nothing without
  `yes: true`: they answer with what they would affect (`video mode
  1080p50 → 525i5994: every output resyncs`) for the agent to put to a
  person. Replacing a used still or macro slot asks the same way.
- **Files** are on the machine running `bmdctl mcp`: `atem_media_upload`
  reads the path it is given, downloads write where `file` says.
- **Watching**: subscribe to `bmdctl://hosts/<host>/state` and the server
  sends a notification on each change (at most four a second); read the
  resource for the state. `atem_watch` and `hyperdeck_watch` collect a few
  seconds of changes and return them, for an agent without subscriptions.
- **Both protocol eras**: the server answers clients of the current
  revision (2026-07-28, no handshake) and of the `initialize` revisions
  before it.

No authentication is involved: the server speaks only to the program
that started it, over stdin and stdout.

## The HTTP API

For an agent framework that speaks HTTP rather than MCP, `bmdctl serve`
serves the same commands as JSON on 127.0.0.1:8880, described by
`GET /openapi.json` (generated from the same table). It has no
authentication either: keep it on localhost.
