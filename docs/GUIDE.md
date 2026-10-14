# bmdctl guide

This walks through bmdctl one task at a time. Each page shows a
command and what it prints at a terminal. The examples use one switcher,
STUDIO A at 192.168.1.20 (an ATEM 1 M/E Constellation HD at 1080p25),
and one HyperDeck, Deck 1 at 192.168.1.50; your names, sources and
times will differ.

At a terminal, and in the documentation site's samples, the value now on
program is red, the value now on preview green, the switcher's name
bold, and ids, times and hints dim; read on GitHub, the samples are
plain. Piped into another program, a command prints the
plain line format instead, and `--json` prints the engine's JSON
([Scripting](guide/scripting.md)). [COMMANDS.md](COMMANDS.md) lists every
command and flag; `bmdctl <command> --help` prints one with examples.

- [Hosts](guide/hosts.md): the devices bmdctl knows and their names,
  which one a command goes to, connecting and the session.
- [Switching](guide/switching.md): program, preview, cut, auto and the
  transition, fade to black, every way to name a source, the aux outputs.
- [Keys and DSKs](guide/keys.md): upstream and downstream keyers, their
  fill and key, on air.
- [Media pool](guide/media.md): stills uploaded, downloaded and cleared,
  what each media player shows.
- [Macros](guide/macros.md): running, recording, and moving macros to
  and from files.
- [Profiles](guide/profiles.md): the switcher's setup saved as ATEM
  Software Control's XML, and restored.
- [watch](guide/watch.md): what changes on the switcher, from any panel,
  as it happens.
- [exec: cue files](guide/exec.md): a file of commands, checked before
  the first is sent; `-t` timestamps.
- [undo](guide/undo.md): putting back the last change.
- [HyperDeck](guide/hyperdeck.md): a deck's transport, clips and slots.
- [Scripting](guide/scripting.md): the line format a pipe gets, `--json`,
  exit status, the state, `--dry-run`.
- [The engine](guide/engine.md): the background process that holds the
  sessions, running it as a service, and the HTTP API.

Claude Code, Claude Desktop and other AI agents use bmdctl through its
MCP server: [AGENTS.md](AGENTS.md).
