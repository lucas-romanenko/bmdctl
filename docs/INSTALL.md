# Installing bmdctl

bmdctl is one static binary with no runtime dependencies. Every release
on the [releases page](https://github.com/lucas-romanenko/bmdctl/releases)
has:

| File | For |
|---|---|
| `bmdctl_darwin_arm64.tar.gz`, `bmdctl_darwin_amd64.tar.gz` | macOS, Apple silicon and Intel |
| `bmdctl_linux_amd64.tar.gz`, `bmdctl_linux_arm64.tar.gz` | Linux, x86-64 and 64-bit ARM (a Raspberry Pi on a 64-bit OS) |
| `bmdctl_windows_amd64.zip` | Windows, x86-64 |
| `bmdctl_<version>_amd64.deb`, `bmdctl_<version>_arm64.deb` | Debian, Ubuntu, Raspberry Pi OS (64-bit) |
| `bmdctl-<version>-1.x86_64.rpm`, `bmdctl-<version>-1.aarch64.rpm` | Fedora, RHEL and the like |
| `checksums.txt` | SHA-256 of every file above |

The archives keep the same name from one release to the next, so
`https://github.com/lucas-romanenko/bmdctl/releases/latest/download/bmdctl_linux_amd64.tar.gz`
is always the latest. Each archive holds `bmdctl`, the four alias
commands (below), `README.md` and the licences. There is no 32-bit
build.

## macOS and Linux: install.sh

```sh
curl -fsSL https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/install.sh | sh
```

It picks the archive for this machine (`uname -s`, `uname -m`), downloads
it and `checksums.txt` from the same release, refuses an archive whose
SHA-256 doesn't match, and installs:

- `bmdctl` into `~/.local/bin`, or `/usr/local/bin` when run as root;
- `atemctl`, `hdctl`, `vhctl` and `ultctl` beside it, as symlinks to
  `bmdctl`.

It touches nothing else. It needs `curl` or `wget`, `tar`, and
`sha256sum` or `shasum`. Three settings, all optional:

| Variable | Default | Meaning |
|---|---|---|
| `BMDCTL_VERSION` | the latest release | a release tag, such as `v0.1.0` |
| `BMDCTL_INSTALL_DIR` | `~/.local/bin`, or `/usr/local/bin` as root | where to install |
| `BMDCTL_BASE_URL` | `https://github.com/lucas-romanenko/bmdctl/releases` | a mirror |

```sh
curl -fsSL https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/install.sh \
  | BMDCTL_VERSION=v0.1.0 BMDCTL_INSTALL_DIR=/opt/bin sh
```

### PATH

`~/.local/bin` is on the PATH of most Linux desktop shells, but not of
macOS's. When the directory isn't on your PATH, install.sh says so. Add
it to your shell's startup file and open a new terminal:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc     # macOS (zsh)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile   # most Linux shells
```

`command -v bmdctl` then prints the path, and `bmdctl version` the
release.

### macOS and Gatekeeper

The macOS binaries are not signed or notarized. install.sh downloads with
curl, which doesn't mark files for Gatekeeper, so they run as installed.
If you unpack an archive downloaded with a browser instead, macOS refuses
to open it until you clear the mark:

```sh
xattr -d com.apple.quarantine bmdctl
```

### By hand

```sh
curl -fLO https://github.com/lucas-romanenko/bmdctl/releases/latest/download/bmdctl_linux_amd64.tar.gz
curl -fLO https://github.com/lucas-romanenko/bmdctl/releases/latest/download/checksums.txt
grep ' bmdctl_linux_amd64.tar.gz$' checksums.txt | sha256sum -c -   # shasum -a 256 -c - on macOS
tar -xzf bmdctl_linux_amd64.tar.gz
mv bmdctl atemctl hdctl vhctl ultctl ~/.local/bin/
```

The aliases in the archive are symlinks to `bmdctl`; keep them beside it.

## Debian, Ubuntu, Fedora: packages

Download the package for your architecture from the
[latest release](https://github.com/lucas-romanenko/bmdctl/releases/latest),
then:

```sh
sudo apt install ./bmdctl_0.1.0_amd64.deb         # Debian, Ubuntu, Raspberry Pi OS
sudo dnf install ./bmdctl-0.1.0-1.x86_64.rpm      # Fedora, RHEL
```

A package installs:

| Path | What |
|---|---|
| `/usr/bin/bmdctl` | the binary |
| `/usr/bin/atemctl`, `hdctl`, `vhctl`, `ultctl` | symlinks to it |
| `/usr/share/man/man1/bmdctl.1` | the manual page (`man bmdctl`) |
| `/usr/share/bash-completion/completions/bmdctl` | bash completion, linked for each alias |
| `/usr/share/zsh/vendor-completions/_bmdctl` | zsh completion |
| `/usr/share/fish/vendor_completions.d/bmdctl.fish` | fish completion |
| `/usr/share/doc/bmdctl/` | the licences |

`/usr/share/zsh/vendor-completions` is Debian's directory. Fedora's zsh
doesn't read it; there, use the `source` line under
[Completion](#completion) instead.

## Windows

With [Scoop](https://scoop.sh):

```powershell
scoop install https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/scoop/bmdctl.json
```

The manifest always points at the latest release, so Scoop lists it as
`nightly-<date>`; `bmdctl version` says which release it is. Scoop puts
`bmdctl.exe`, `atemctl.exe`, `hdctl.exe`, `vhctl.exe` and `ultctl.exe` on
the PATH through its shims.

By hand: unpack `bmdctl_windows_amd64.zip` into a directory on your PATH.
The four alias executables in it are copies of `bmdctl.exe`, since
Windows archives can't hold symlinks.

### Files and the engine's pipe

| What | Where |
|---|---|
| hosts.yml | `%AppData%\bmdctl\hosts.yml` |
| the engine's log, the last host | `%LocalAppData%\bmdctl\` |
| the engine | the named pipe `\\.\pipe\bmdctl-<user>` |

The pipe is created for the current user only: its access list grants
that user and SYSTEM, it refuses clients from other machines, and a
second engine can't open it. `BMDCTL_SOCKET` names a different pipe. The
Windows engine code is checked by `go vet` with `GOOS=windows` and the
executables were run under Wine for the release dry run, but none of it
has run on Windows yet ([HARDWARE.md](HARDWARE.md#unverified)). There is
no PowerShell completion.

## Docker

The image `ghcr.io/lucas-romanenko/bmdctl` (linux amd64 and arm64, tags
`<version>` and `latest`) is the binary on `scratch`, running
`bmdctl engine --serve`: the engine in the foreground, with the HTTP API.
A switcher is reached over UDP on the local network, so give the
container the host's network:

```sh
docker run -d --name bmdctl --network host \
  -v bmdctl:/data -v /run/bmdctl:/run/bmdctl \
  ghcr.io/lucas-romanenko/bmdctl
docker exec bmdctl /bmdctl connect 192.168.1.20
curl -s localhost:8880/hosts
```

- **`/data`** holds hosts.yml (`/data/config/bmdctl/hosts.yml`) and the
  engine's log (`/data/state/bmdctl/`).
- **`/run/bmdctl`** holds the engine's socket. Share it, and a bmdctl
  installed on the host uses the container's engine:
  `sudo BMDCTL_SOCKET=/run/bmdctl/bmdctl.sock bmdctl status`. The engine
  in the image runs as root and makes its socket its owner's alone,
  hence `sudo`. It answers a CLI of any build, since it was started by
  hand.
- **The HTTP API** listens on the host's `127.0.0.1:8880` and has no
  authentication. To serve it to other machines, override the command
  (`... ghcr.io/lucas-romanenko/bmdctl engine --serve --listen 0.0.0.0:8880`),
  knowing anyone who can reach it controls the switchers.

## Go

```sh
go install github.com/lucas-romanenko/bmdctl/cmd/bmdctl@latest
```

This needs Go 1.27 and installs `bmdctl` alone into `$(go env GOPATH)/bin`;
make the aliases yourself if you want them (below). From a clone:

```sh
git clone https://github.com/lucas-romanenko/bmdctl
cd bmdctl && go build -o bmdctl ./cmd/bmdctl
```

## The alias commands

Run under another name, bmdctl is the command for one device:

| Name | Is | Example |
|---|---|---|
| `atemctl` | `bmdctl atem` | `atemctl cut` |
| `hdctl` | `bmdctl hyperdeck` | `hdctl --host "Deck 1" play --loop` |
| `vhctl` | `bmdctl videohub` | no commands yet |
| `ultctl` | `bmdctl ultimatte` | no commands yet |

bmdctl reads the name it was started as (case and `.exe` ignored), so
any symlink or copy works. `hdctl --help` lists the HyperDeck's commands
alone; built-ins such as `hdctl hosts` and `hdctl version` are bmdctl's.
To make them by hand:

```sh
cd ~/.local/bin && for a in atemctl hdctl vhctl ultctl; do ln -sf bmdctl $a; done
```

## Completion

`bmdctl completion bash|zsh|fish` prints a script. Each one calls
`bmdctl __complete` for the candidates, so all three complete the same
things: commands, flags, enum values, files, host names from hosts.yml
and, while the engine runs, the switcher's input names and the indexes it
has, counted from 1. Pressing Tab never starts the engine or opens a
session. The scripts register all five names, so the aliases complete
too.

**bash** (needs the bash-completion package for the file form):

```sh
echo 'source <(bmdctl completion bash)' >> ~/.bashrc
# or, loaded on demand:
mkdir -p ~/.local/share/bash-completion/completions
bmdctl completion bash > ~/.local/share/bash-completion/completions/bmdctl
for a in atemctl hdctl vhctl ultctl; do
  ln -sf bmdctl ~/.local/share/bash-completion/completions/$a
done
```

bash-completion loads a file by the command's name, hence a link per
alias.

**zsh**, after `compinit` in `~/.zshrc`:

```sh
echo 'source <(bmdctl completion zsh)' >> ~/.zshrc
# or, as a function file on $fpath:
mkdir -p ~/.zfunc && bmdctl completion zsh > ~/.zfunc/_bmdctl
# and in ~/.zshrc, before compinit: fpath=(~/.zfunc $fpath)
```

**fish**:

```sh
bmdctl completion fish > ~/.config/fish/completions/bmdctl.fish
```

fish loads that file when you first complete `bmdctl`. For the aliases
to complete before then, put `bmdctl completion fish | source` in
`~/.config/fish/config.fish` instead.

The deb and rpm install all three. The scripts are syntax-checked by the
tests; zsh's and fish's haven't been tried in a real shell yet
([HARDWARE.md](HARDWARE.md#unverified)).

## Upgrading

Install the new release the way you installed the old one:

| Installed with | Upgrade with |
|---|---|
| install.sh | run it again (it replaces the file by rename, so a running bmdctl is unaffected) |
| deb or rpm | `sudo apt install ./bmdctl_<new>_amd64.deb`, `sudo dnf upgrade ./bmdctl-<new>-1.x86_64.rpm` |
| Scoop | `scoop update bmdctl` |
| Docker | `docker pull ghcr.io/lucas-romanenko/bmdctl`, then recreate the container |
| go install | run it again |

The background engine restarts by itself: every command carries the
build of the bmdctl that sent it, and an engine a command started from
another build stops and is replaced by one of the new build. An engine
you run yourself (`bmdctl engine` under systemd, or the container) keeps
running the old code until you restart it; `bmdctl doctor` warns when
the engine and the CLI differ.

## Uninstalling

Stop the engine first, so its sessions say goodbye to the switchers:

```sh
bmdctl engine stop
```

Then remove what the install put in place:

| Installed with | Remove with |
|---|---|
| install.sh | `rm ~/.local/bin/{bmdctl,atemctl,hdctl,vhctl,ultctl}` (or the `/usr/local/bin` ones) |
| deb or rpm | `sudo apt remove bmdctl`, `sudo dnf remove bmdctl` |
| Scoop | `scoop uninstall bmdctl` |
| Docker | `docker rm -f bmdctl && docker volume rm bmdctl` |
| go install | `rm "$(go env GOPATH)/bin/bmdctl"` |

bmdctl's own files are left behind for you to remove:

| What | macOS and Linux | Windows |
|---|---|---|
| hosts.yml | `~/.config/bmdctl/` | `%AppData%\bmdctl\` |
| the engine's log, the last host, one-time notices | `~/.local/state/bmdctl/` | `%LocalAppData%\bmdctl\` |
| the media pool's stills, kept for profile saves | `~/.cache/bmdctl/` | `%LocalAppData%\bmdctl\cache\` |
| the engine's socket and lock | `$XDG_RUNTIME_DIR/bmdctl.sock*`, else `$TMPDIR/bmdctl-<uid>/` or `/tmp/bmdctl-<uid>/` | none (a named pipe) |

`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` move the first
three, where set.
