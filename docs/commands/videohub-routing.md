# videohub: routing

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Routing and labels: route an input to an output, rename an input or an output.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl videohub input rename <input> <label>`

Rename an input on the router.

```
bmdctl input rename 3 "Wide Shot"
bmdctl input rename "Camera 3" ""
```

Echo: `input labels:<input>`.

| argument | value | | |
|---|---|---|---|
| `<input>` | string | required | an input: its number, from 1, or its label or the start of it, in any case |
| `<label>` | string | required | the new label, at most 64 characters; runs of spaces and line breaks become one space, and an empty one clears it |

## `bmdctl videohub output rename <output> <label>`

Rename an output on the router.

```
bmdctl output rename 5 Director
```

Echo: `output labels:<output>`.

| argument | value | | |
|---|---|---|---|
| `<output>` | string | required | an output: its number, from 1, or its label or the start of it, in any case |
| `<label>` | string | required | the new label, at most 64 characters; runs of spaces and line breaks become one space, and an empty one clears it |

## `bmdctl videohub route <output> <input>`

Route an input to an output.

```
bmdctl route 5 3
bmdctl route "Monitor A" "Camera 3"
bmdctl route projector graphics
```

Echo: `routing:<output>`.

| argument | value | | |
|---|---|---|---|
| `<output>` | string | required | an output: its number, from 1, or its label or the start of it, in any case |
| `<input>` | string | required | an input: its number, from 1, or its label or the start of it, in any case |
