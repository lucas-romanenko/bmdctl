# ultimatte: profile

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

Archive and restore: the presets and settings to a Smart Remote 4 zip, and back.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl ultimatte archive [file]`

Save the keyer's presets and settings to a zip, as Smart Remote 4's Archive All does.

```
bmdctl archive
bmdctl archive keyer-a.zip
```

| argument | value | | |
|---|---|---|---|
| `[file]` | file, written | optional | the zip to write; - is stdout (default: the keyer's label .zip) |

## `bmdctl ultimatte restore <file>`

Push an archive's presets and settings to the keyer, as Smart Remote 4's Restore does.

```
bmdctl restore keyer-a.zip
```

**Destructive.**

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the zip: one archive wrote, or Smart Remote's Archive All |
