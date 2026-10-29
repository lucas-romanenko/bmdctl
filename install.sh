#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Install bmdctl on macOS or Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/lucas-romanenko/bmdctl/main/install.sh | sh
#
# It finds this machine's OS and architecture, downloads that release
# archive from GitHub and checks it against the release's checksums.txt,
# installs bmdctl to ~/.local/bin (/usr/local/bin when run as root), and
# links atemctl, hdctl, vhctl and ultctl to it: each is bmdctl for one
# device. Nothing else is touched.
#
# Settings, all optional:
#   BMDCTL_VERSION      a release tag (v0.1.0); the latest release if unset
#   BMDCTL_INSTALL_DIR  where to install, instead of the above
#   BMDCTL_BASE_URL     where releases are, instead of
#                       https://github.com/lucas-romanenko/bmdctl/releases
#                       (for a mirror, or a test)
set -eu

REPO=lucas-romanenko/bmdctl
BASE=${BMDCTL_BASE_URL:-https://github.com/$REPO/releases}
ALIASES="atemctl hdctl vhctl ultctl"

say() { printf '%s\n' "$*"; }
die() { printf 'bmdctl install: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "this needs $1"; }
need uname
need tar
need mktemp
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
	location() { curl -fsSI -o /dev/null -w '%{redirect_url}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
	# The first hop's Location: BusyBox's wget has no --max-redirect.
	location() { wget -q -S -O /dev/null "$1" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tr -d '\r' | head -n 1; }
else
	die "this needs curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	die "this needs sha256sum or shasum to check the download"
fi

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "no release for $(uname -s): on Windows, use the Scoop manifest (see README.md)" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "no release for $(uname -m)" ;;
esac

tag=${BMDCTL_VERSION:-}
if [ -z "$tag" ]; then
	# The latest release redirects to its tag's page.
	tag=$(location "$BASE/latest" | sed -n 's|.*/tag/||p')
	[ -n "$tag" ] || die "can't find the latest release at $BASE/latest"
fi
case $tag in v*) ;; *) tag=v$tag ;; esac

archive=bmdctl_${os}_${arch}.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' INT TERM

say "downloading bmdctl $tag for $os/$arch"
fetch "$BASE/download/$tag/$archive" "$tmp/$archive" || die "can't download $BASE/download/$tag/$archive"
fetch "$BASE/download/$tag/checksums.txt" "$tmp/checksums.txt" || die "can't download the release's checksums.txt"
want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "checksums.txt has no line for $archive"
got=$(sha256 "$tmp/$archive")
[ "$got" = "$want" ] || die "$archive doesn't match checksums.txt (got $got, want $want)"

mkdir "$tmp/x"
tar -xzf "$tmp/$archive" -C "$tmp/x"
[ -f "$tmp/x/bmdctl" ] || die "$archive holds no bmdctl"

dir=${BMDCTL_INSTALL_DIR:-}
if [ -z "$dir" ]; then
	if [ "$(id -u)" = 0 ]; then
		dir=/usr/local/bin
	else
		dir=$HOME/.local/bin
	fi
fi
mkdir -p "$dir" || die "can't create $dir"
[ -w "$dir" ] || die "can't write to $dir: set BMDCTL_INSTALL_DIR, or run as root for /usr/local/bin"

# Into place by rename, so a running bmdctl keeps its old file.
cp "$tmp/x/bmdctl" "$dir/.bmdctl.new"
chmod 755 "$dir/.bmdctl.new"
mv -f "$dir/.bmdctl.new" "$dir/bmdctl"
for a in $ALIASES; do
	ln -sf bmdctl "$dir/$a"
done

say "installed $dir/bmdctl, with $(echo "$ALIASES" | sed 's/ /, /g') linked to it"
"$dir/bmdctl" version || true
case :${PATH}: in
*:"$dir":*) ;;
*) say "" && say "$dir is not on your PATH: add it, e.g. in ~/.profile:" && say "  export PATH=\"$dir:\$PATH\"" ;;
esac
say ""
say "next: bmdctl connect <ip>"
