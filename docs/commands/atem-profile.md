# atem: profile

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

ATEM Software Control profiles: save the switcher's setup, restore one, download its stills.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl atem profile images`

Download every used still as a PNG, named as profiles name them.

```
bmdctl profile images
bmdctl profile images --dir stills/
```

| argument | value | | |
|---|---|---|---|
| `--dir` | dir | default ATEM Media Pool | where to write them |

## `bmdctl atem profile restore <file>`

Apply a profile XML to the switcher, and upload the images it names.

```
bmdctl profile restore show.xml
bmdctl profile restore show.xml --program --preview
bmdctl profile restore show.xml --macros-only
```

**Destructive.**

| argument | value | | |
|---|---|---|---|
| `<file>` | file | required | the profile XML; its images are read from beside it |
| `--mes` | string | default all | the M/Es the M/E sections cover: all, or a list from 1 (1,2) |
| `--program` | bool | default false | each M/E's program source |
| `--preview` | bool | default false | each M/E's preview source |
| `--next-transition` | bool | default true | each M/E's next-transition layers |
| `--transition-style` | bool | default true | each M/E's transition style and its settings |
| `--ftb` | bool | default true | each M/E's fade to black |
| `--keyers` | string | default all | the upstream keyers: all, none, or a list from 1 (1,2) |
| `--macros-only` | bool | default false | start from nothing but the macros (ApplyOptions.macros_only), then add flags given |
| `--video-mode` | bool | default true | change the video mode if it differs: outputs resync |
| `--inputs` | bool | default true | input names |
| `--settings-flags` | bool | default true | settings flags (fade to black enabled) |
| `--color-generators` | bool | default true | colour generators |
| `--aux` | bool | default true | aux outputs |
| `--hyperdecks` | bool | default true | HyperDeck slots |
| `--downstream-keys` | bool | default true | downstream keyers |
| `--fly-keyframes` | bool | default false | drive each keyer through its saved keyframes to store them (shows on air) |
| `--media-players` | bool | default true | media players |
| `--audio` | bool | default true | the Fairlight mixer: master, strips, EQ, dynamics |
| `--macros` | bool | default true | the macro pool, uploading each macro |
| `--media-pool-images` | bool | default true | upload the stills the XML names, from "ATEM Media Pool" beside it |
| `--multiview` | bool | default false | no setter exists: noted as skipped |
| `--camera-control` | bool | default false | no setter exists: noted as skipped |
| `--talkback` | bool | default false | no setter exists: noted as skipped |

## `bmdctl atem profile save <file>`

Save the switcher's setup as ATEM Software Control's profile XML, and its media pool images.

```
bmdctl profile save show.xml
bmdctl profile save show.xml --media-pool-images off
```

| argument | value | | |
|---|---|---|---|
| `<file>` | file, written | required | the profile XML to write; images go beside it |
| `--mes` | string | default all | the M/Es the M/E sections cover: all, or a list from 1 (1,2) |
| `--program` | bool | default true | each M/E's program source |
| `--preview` | bool | default true | each M/E's preview source |
| `--next-transition` | bool | default true | each M/E's next-transition layers |
| `--transition-style` | bool | default true | each M/E's transition style and its settings |
| `--ftb` | bool | default true | each M/E's fade to black |
| `--keyers` | string | default all | the upstream keyers: all, none, or a list from 1 (1,2) |
| `--downstream-keys` | bool | default true | <DownstreamKeys> |
| `--color-generators` | bool | default true | <ColorGenerators> |
| `--fairlight` | bool | default true | <FairlightAudioMixer> |
| `--camera-control` | bool | default true | <CameraControl> |
| `--media-pool` | bool | default true | <MediaPool>: the stills' names and paths |
| `--media-pool-images` | bool | default true | each used still as a PNG in "ATEM Media Pool" beside the XML |
| `--media-players` | bool | default true | <MediaPlayers> |
| `--aux` | bool | default true | <Auxiliaries> |
| `--counters` | bool | default true | <Counters> |
| `--settings` | bool | default true | <Settings>, input names included |
| `--video-mode` | bool | default true | <VideoMode> |
| `--hyperdecks` | bool | default true | <HyperDecks> |
| `--macros` | bool | default true | <MacroPool> and <MacroControl>, downloading each macro |
