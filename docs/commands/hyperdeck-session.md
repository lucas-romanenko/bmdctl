# hyperdeck: session

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The session and reads: status (transport, timecode, clip, slots, time left), the timeline's or a disk's clips, the raw state, watch changes, connect, disconnect.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl hyperdeck clips`

List the timeline's clips, or a disk's.

```
bmdctl clips
bmdctl clips --disk
bmdctl clips --slot 2
```

| argument | value | | |
|---|---|---|---|
| `--disk` | bool | default false | the active slot's disk, not the timeline |
| `--slot` | uint | optional | this slot's disk (implies --disk) |

## `bmdctl hyperdeck connect [address]`

Connect to a HyperDeck and keep the session open; an address is saved as the default host.

```
bmdctl connect 192.168.81.90
bmdctl connect
bmdctl connect --pin false
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (saved to hosts.yml by the name the deck gives itself, as the default) or a host's name; else the default |
| `--pin` | bool | default true | keep the session open until disconnect or an hour without a command ($BMDCTL_PIN_IDLE); false lets it close after 10 idle minutes |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

## `bmdctl hyperdeck disconnect`

Close the engine's session to the deck, pinned or not.

```
bmdctl disconnect
```

## `bmdctl hyperdeck state`

Print what the session holds of the deck, or one part of it.

```
bmdctl state
bmdctl state --path transport.status
```

| argument | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: transport.status, slots.1 |

## `bmdctl hyperdeck status`

Show the transport, timecode, clip, slots and recording time left.

```
bmdctl status
```

## `bmdctl hyperdeck watch`

Print each change the deck reports, as it comes.

```
bmdctl watch
bmdctl watch --count 5
bmdctl watch --json
```

Runs until interrupted.

| argument | value | | |
|---|---|---|---|
| `--count` | uint | optional | stop after this many changes |
