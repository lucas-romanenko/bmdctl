# Security

## Reporting a vulnerability

Please don't open a public issue. Report it privately, through GitHub's
[private vulnerability reporting](https://github.com/lucas-romanenko/bmdctl/security/advisories/new)
or by email to lucas@romanenko.com, with what you found, how to
reproduce it and the version (`bmdctl version`). You'll hear back within
a week. A fix goes out in a release with an advisory, which credits you
unless you'd rather it didn't.

## Supported versions

The latest release. bmdctl is before 1.0, so fixes go into the next
release, not older ones.

## Where the boundaries are

- **The devices' own protocols have no authentication.** An ATEM (UDP
  9910), a HyperDeck (TCP 9993), a Videohub (TCP 9990) and an Ultimatte
  (TCP 9998 and 9996) take commands from anyone who can reach them, and
  bmdctl can't change that: keep them on a network you trust. That
  someone on that network can control a device is not a bmdctl
  vulnerability.
- **The engine's socket** is `$XDG_RUNTIME_DIR/bmdctl.sock`, mode 0600 in
  a directory only its user owns; on Windows, a named pipe only the
  current user (and SYSTEM) can open. Another local user reaching it is
  a vulnerability.
- **`bmdctl serve` and `bmdctl engine --serve`** have no authentication:
  whoever can reach the port controls every device in hosts.yml. They
  listen on 127.0.0.1:8880 unless `--listen` says otherwise, and
  listening elsewhere is the user's choice. Reaching one that listens
  only on loopback from another machine is a vulnerability.
- **`bmdctl mcp`** speaks only to the program that started it, over
  stdin and stdout. Its destructive tools do nothing without
  `confirm: true` ([docs/AGENTS.md](docs/AGENTS.md)); one that acts
  without it is a vulnerability.
- **Files bmdctl reads** (profiles, still images, macro files, Ultimatte
  archives, `exec` scripts) come from the user. A crafted one that makes
  bmdctl write outside the place it was told to, or run anything, is a
  vulnerability.
