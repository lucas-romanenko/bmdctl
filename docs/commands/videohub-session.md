# videohub: session

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The session and reads: status (the routing, with labels and locks), the inputs, the outputs, the raw state, watch changes, connect, disconnect.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl videohub connect [address]`

Connect to a Videohub and keep the session open; an address is saved as the default host.

```
bmdctl connect 192.168.81.95
bmdctl connect
bmdctl connect --pin false
```

| argument | value | | |
|---|---|---|---|
| `[address]` | string | optional | an address (saved to hosts.yml by the name the router gives itself, as the default) or a host's name; else the default |
| `--pin` | bool | default true | keep the session open until disconnect or an hour without a command ($BMDCTL_PIN_IDLE); false lets it close after 10 idle minutes |
| `--type` | string | optional | what the device is, without asking it: atem, hyperdeck, videohub or ultimatte |

## `bmdctl videohub disconnect`

Close the engine's session to the router, pinned or not.

```
bmdctl disconnect
```

## `bmdctl videohub inputs`

List the inputs: each one's number and label, and the outputs it is routed to.

```
bmdctl inputs
```

## `bmdctl videohub outputs`

List the outputs: each one's number, label, the input routed to it and its lock.

```
bmdctl outputs
bmdctl outputs --json
```

## `bmdctl videohub state`

Print what the session holds of the router, or one part of it.

```
bmdctl state
bmdctl state --path routing.4
```

| argument | value | | |
|---|---|---|---|
| `--path` | string | optional | dotted key into the state: routing.3, device.model name |

## `bmdctl videohub status`

Show the router and its routing: each output's input, label and lock.

```
bmdctl status
```

## `bmdctl videohub watch`

Print each change the router reports, as it comes: routes, labels, locks.

```
bmdctl watch
bmdctl watch --count 5
bmdctl watch --json
```

Runs until interrupted.

| argument | value | | |
|---|---|---|---|
| `--count` | uint | optional | stop after this many changes |
