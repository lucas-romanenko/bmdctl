# ultimatte: network

Generated from the command table (`internal/table`); do not edit. After
changing a table, run `go generate ./cmd/bmdctl`.

The network interface: read it, or set its address, mask, gateway, DNS or DHCP.

[COMMANDS.md](../COMMANDS.md) says how to read an entry, and lists the
flags every command takes and the built-in commands.

## `bmdctl ultimatte network set`

Set the keyer's network interface, and read it back once it has reapplied.

```
bmdctl network set --address 192.168.1.20 --netmask 255.255.255.0
bmdctl network set --gateway 192.168.1.1 --dns 192.168.1.1
bmdctl network set --dhcp on
```

**Destructive.** Give at least one flag.

| argument | value | | |
|---|---|---|---|
| `--interface` | uint | default 0 | the network interface: 0 on an Ultimatte 12 |
| `--address` | string | optional | the static address; goes with --netmask, as one field on the wire |
| `--netmask` | string | optional | the static netmask, dotted (255.255.252.0), not a prefix length |
| `--gateway` | string | optional | the static gateway |
| `--dns` | list | optional | the static DNS servers, comma-separated; empty clears them |
| `--dhcp` | bool | optional | take an address by DHCP (on), or use the static one (off) |

## `bmdctl ultimatte network show`

Show the keyer's network interface: address, mask, gateway and DNS, in use and configured.

```
bmdctl network show
bmdctl network show --json
```

| argument | value | | |
|---|---|---|---|
| `--interface` | uint | default 0 | the network interface: 0 on an Ultimatte 12 |
