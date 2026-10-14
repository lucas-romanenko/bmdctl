# Hosts

bmdctl keeps the devices it knows in `hosts.yml`
(`~/.config/bmdctl/hosts.yml`; `%AppData%\bmdctl\hosts.yml` on Windows).
You don't name them: a host goes by the name the device gives itself,
which for an ATEM is the name set in ATEM Setup, else its model, and for
a HyperDeck the name set in HyperDeck Setup.

`hosts add ADDRESS` asks what answers at the address (an ATEM on UDP
9910, a HyperDeck on TCP 9993, a Videohub on TCP 9990 or an Ultimatte on
TCP 9998, all at once, for 2 seconds) and saves it by its name:

```
$ bmdctl hosts add 192.168.1.20
added STUDIO A (192.168.1.20)
$ bmdctl hosts add 192.168.1.50
added Deck 1 (192.168.1.50)
$ bmdctl hosts default "STUDIO A"
default host: STUDIO A
$ bmdctl hosts
STUDIO A  192.168.1.20  atem       default
Deck 1    192.168.1.50  hyperdeck
```

- **Two devices with one name** (two switchers of one model with no
  ATEM Setup name both call themselves by the model): give the second
  one a name with `--as`. It goes by that name from then on.

  ```
  $ bmdctl hosts add 192.168.1.21 --as "STUDIO B"
  added STUDIO B (192.168.1.21), which calls itself "ATEM 1 M/E Constellation HD"
  ```

- **A short name:** a device's own name can be awkward to type. `hosts
  alias NAME SHORT` gives it a short one it goes by as well, wherever a
  name is taken; `bmdctl` shows it before the name, `hosts` after it.

  ```
  $ bmdctl hosts alias "BC 1.2 - TESTTt" bc
  BC 1.2 - TESTTt  192.168.1.22
    alias       bc
  $ bmdctl --host bc cut
  BC 1.2 - TESTTt  PGM CAM2 · PVW CAM1
    cut         CAM1 on program · CAM2 on preview                    31 ms
  ```

  An alias is one word, and can't be a command's (`cut`, `hosts`) or
  another host's name or alias. Another alias replaces it; `hosts alias
  bc --rm` takes it away.
- **Renamed in ATEM Setup:** the next command to that switcher renames
  its host in hosts.yml and says so once. `--host` still takes the old
  name, so scripts keep working.
- **Which host a command goes to:** `--host`, else `$BMDCTL_HOST`, else
  the host the last `--host` named, else the default. `--host` takes a
  name or an alias in any case, any unambiguous start of one (`--host
  "studio b"`, `--host deck`), the name a host had before a rename, or an
  address.
- **The host as the first word:** `bmdctl bc cut` is `bmdctl --host bc
  cut`, for that command alone: the next command without a host goes
  where it went before. The first word takes what `--host` does, except
  a DNS name no host has (a mistyped command would be dialled): an IP
  address works. A word that is a command is always the command, so a
  host called `info` needs `--host info`; `--host`, given too, wins. A
  first word that is neither says so and lists the hosts:

  ```
  $ bmdctl progam 2
  error  no command or host named "progam"
         hosts: bc · studio · deck · bmdctl --help lists the commands
  ```

  In an `exec` file, a line's first word names its host the same way.
- **Changing the default:** `bmdctl use HOST` makes a host the one
  commands without a host go to, without connecting (`connect HOST` also
  pins a session). `bmdctl` marks it `*`. `$BMDCTL_HOST`, when set,
  still wins, and `use` says so.

  ```
  $ bmdctl use bc
  BC 1.2 - TESTTt  192.168.1.22
    default     STUDIO A → bc
  ```
- **`hosts discover`** adds every ATEM that announces itself over
  Bonjour within 2 seconds (`--for 5s` waits longer). Bonjour doesn't
  cross VLANs or VPNs; there, add switchers by address.
- **`hosts rm NAME`** forgets one.

The file is plain YAML and safe to edit or keep in git:

```yaml
version: 2
default: STUDIO A
hosts:
  - name: STUDIO A
    address: 192.168.1.20
    device: atem
  - name: Deck 1
    address: 192.168.1.50
    device: hyperdeck
```

## Connecting

You don't have to connect: the first command to a switcher opens a
session, through the background engine ([The engine](engine.md)), and
later commands reuse it. A session closes after 10 minutes without a
command. `connect ADDRESS` does what `hosts add` does, makes the switcher
the default, and pins the session open until `disconnect`, or until an
hour passes without a command:

```
$ bmdctl connect 192.168.1.20
STUDIO A  PGM CAM2 · PVW CAM1 · 1080p25 · connected 328 ms
  saved as "STUDIO A", now the default
  tab completes input names
```

The first line is the header every command starts with: the switcher's
name, then what was on program and preview when the command ran, and,
when this command opened the session, the video mode and how long it
took. `connect` alone pins the default host's session; `connect --pin
off` opens it without pinning.

```
$ bmdctl connect
STUDIO A  PGM CAM2 · PVW CAM1
  connect     pinned until disconnect or 1h idle
$ bmdctl disconnect
STUDIO A  disconnected
```

`bmdctl` alone lists the hosts and their sessions (how long each has
been open and idle, and whether it is pinned), and never starts the
engine. A session past its idle timeout that hasn't closed yet (the
machine slept through it) says `stale` until it goes. `bmdctl status`
is the switcher at a glance:

```
$ bmdctl
bmdctl v0.1.0

* STUDIO A  192.168.1.20  1080p25               ● 4m · idle 12s · pinned

  * default · bmdctl use HOST · bmdctl help
$ bmdctl status
STUDIO A  1080p25 · session 4m, idle 12s, pinned

  ME 1        PGM  CAM2           PVW  CAM1
              mix 1:00 · next background · FTB off
  keys        1  luma  CAM4 / CAM4                                   off
              2  luma  MP1 / MP1K                                    off
              3  luma  MP1 / MP1K                                    off
              4  luma  MP1 / MP1K                                    off
  dsk 1       MP2 / MP2K                                          on air
  dsk 2       COL1 / COL1                                            off
  aux 1–6     PGM
  macro       idle
```

`bmdctl show keys`, `show dsk`, `show transition`, `show audio`, `show
media`, `show macros`, `show aux` and `show hyperdecks` each lay out one
area in more detail.
