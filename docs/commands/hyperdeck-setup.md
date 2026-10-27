# hyperdeck: setup

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The deck's setup: remote control on or off, and its configuration (inputs, file format, record prefix, timecode): show and set.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl hyperdeck configuration set <setting> <value>`

Change one of the deck's configuration settings, as configuration show names it.

```
bmdctl configuration set "record prefix" Interview
bmdctl configuration set video-input HDMI
```

| argument | value | | |
|---|---|---|---|
| `<setting>` | string | required | a setting configuration show lists (video input, record prefix, …); - or _ will do for its spaces |
| `<value>` | string | required | its new value, as the deck spells it |

## `bmdctl hyperdeck configuration show`

Show the deck's configuration: each setting as the deck names it, and its value.

```
bmdctl configuration show
bmdctl configuration show --json
```

## `bmdctl hyperdeck remote <on>`

Turn the deck's remote control on or off; off, it refuses every other write, bmdctl's included.

```
bmdctl remote on
bmdctl remote off
```

**Unverified.**

| argument | value | | |
|---|---|---|---|
| `<on>` | bool | required | on lets clients control the deck; off refuses their writes until it is on again |
