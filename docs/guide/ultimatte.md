# Ultimatte

An Ultimatte keyer is a host like a switcher: commands go to it with
`--host`, or through `ultctl`, which is `bmdctl ultimatte`. There is no
session to keep: each command connects, does its work and hangs up, as
Smart Remote 4 does. A keyer goes by its label. All of it but `network
set` has run on an Ultimatte 12 4K
([HARDWARE.md](../HARDWARE.md#unverified)).

```
$ bmdctl hosts add 192.168.1.41
added Keyer A (192.168.1.41)
$ bmdctl --host keyer status
Keyer A  Ultimatte 12 4K

  presets     Green Screen, Blue Screen, Studio B
  address     192.168.1.41/255.255.252.0
  gateway     192.168.1.1
  dns         192.168.1.53
  dhcp        off: static
  mac         7c:2e:0d:00:00:01
```

`presets` lists the saved presets, and `controls` the live control
values: each one raw, as the keyer sends it, and in the units Smart
Remote shows (`--defaults` lists the defaults instead). A real unit
lists many more than this:

```
$ bmdctl --host keyer presets
Keyer A  3 presets

  1  Green Screen
  2  Blue Screen
  3  Studio B
$ bmdctl --host keyer controls
Keyer A  7 controls

  Matte Density         5000  100%
  Black Gloss           1200  12%
  Shadow Level          7500  75%
  Cursor X              5000  50%
  Transition Rate       30    30 frames
  Offset Black Balance  2500  -50%
  FG Saturation Master  5000  100%
```

`prelude` prints everything the keyer sends when a client connects, as
it sends it.

## Archive and restore

`archive` saves the keyer's presets and settings to a zip, as Smart
Remote 4's Archive All does, and Smart Remote can restore the file. With
no file named, the zip takes the keyer's label:

```
$ bmdctl --host keyer archive
Keyer A
  ✓ presets   Green Screen, Blue Screen, Studio B                 640 ms
  ✓ settings  GPISettings, SavedSettings                          410 ms
  archive     Keyer_A.zip  1 KB · 3 presets · 2 settings           1.1 s
```

A preset or setting that can't be read is left out and named under the
result, so a later restore never writes placeholder bytes to the keyer.

`restore` pushes an archive back: each preset replaces the one of its
name, and the saved settings become what the keyer is doing now.
Anything changed on the unit and not saved there is
lost, so it asks first, and says when there is something to lose
(`--yes` doesn't ask). Save on the unit first if you want to keep it:

```
$ bmdctl --host keyer restore Keyer_A.zip
Keyer A
  Keyer A gets 3 presets and the settings
  the saved settings become its live state
  its unsaved changes to "Studio B" are lost
  restore it? [y/N] y
  ✓ presets   Blue Screen, Green Screen, Studio B                 720 ms
  ✓ settings  GPISettings, SavedSettings                          380 ms
  restore     Keyer_A.zip  3 presets · 2 settings                  1.1 s
```

If the keyer refuses part of it, the restore stops there, and what came
before stays written.

## Network settings

`network show` is the network interface, the address in use and the one
set where they differ. `network set` changes it. A wrong address puts a
keyer out of reach until someone visits it, so it asks first, and it
waits for the keyer to take the new settings before it says they took:

```
$ bmdctl --host keyer network set --address 192.168.1.42 --netmask 255.255.255.0
Keyer A
  address 192.168.1.41/255.255.252.0 → 192.168.1.42/255.255.255.0
  a wrong address puts Keyer A out of reach until someone visits it
  set it? [y/N] n
  cancelled
$ bmdctl --host keyer network set --dns 192.168.1.53,192.168.1.54 --yes
Keyer A
  dns         192.168.1.53 → 192.168.1.53, 192.168.1.54           840 ms
```

`--address` and `--netmask` go together, as the keyer takes them as one
setting. `--gateway`, `--dns` (an empty list clears it) and `--dhcp`
change the rest.

A keyer that doesn't answer says so, with the port:

```
$ bmdctl --host keyer presets
error  Keyer A (192.168.1.41) did not answer
       TCP 9998: connection refused · bmdctl doctor checks the network
```
