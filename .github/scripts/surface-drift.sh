#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Compare bmdctl's copies of bmdwire's SURFACE.md with bmdwire's own, for
# .github/workflows/surface-drift.yml. Run it from the repository root:
#
#   .github/scripts/surface-drift.sh BMDWIRE_DIR OUT_DIR
#
# BMDWIRE_DIR is a checkout of bmdwire (main, in the workflow). Each copy
# is compared without the header comment bmdctl puts above the copied
# text. When any differ, OUT_DIR gets body.md, the drift issue's body, and
# one .diff per copy that differs; when none do, OUT_DIR stays empty.
# RUN_URL, when set, is linked from the body. Nothing here talks to a
# switcher.
#
# shellcheck disable=SC2016 # backticks in single quotes are Markdown.
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 BMDWIRE_DIR OUT_DIR" >&2
	exit 2
fi
bmdwire=$1
out=$2
repo_url=https://github.com/lucas-romanenko/bmdwire
# GitHub refuses an issue body over 65,536 characters; diffs past this
# many bytes are cut, and the run's artifact keeps them whole.
budget=60000

mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

tracked=$(sed -n 's/^Tracks: bmdwire \(v[^ ]*\)$/\1/p' docs/PORTING.md)
if [ -z "$tracked" ] || [ "$(printf '%s\n' "$tracked" | wc -l)" -ne 1 ]; then
	echo "docs/PORTING.md needs exactly one line 'Tracks: bmdwire <describe>'" >&2
	exit 1
fi
case $tracked in
*-g*) base=${tracked##*-g} ;;
*) base=$tracked ;;
esac
main=$(git -C "$bmdwire" rev-parse HEAD)

# copied drops a leading <!-- ... --> comment and the blank line after it.
copied() {
	awk '
		NR == 1 && /^<!--/ { inhdr = 1 }
		inhdr { if (/-->/) { inhdr = 0; after = 1 }; next }
		after && /^$/ { after = 0; next }
		{ after = 0; print }
	' "$1"
}

# fence is a backtick fence longer than any backtick run in the file.
fence() {
	local n
	n=$({ grep -o '`*' "$1" || true; } | awk '{ if (length($0) > m) m = length($0) } END { print m + 1 }')
	[ "$n" -lt 4 ] && n=4
	printf '%*s' "$n" '' | tr ' ' '`'
}

sections=$work/sections.md
missing=$work/missing.md
: >"$sections"
: >"$missing"
drift=0

# compare OURS THEIRS REQUIRED: OURS is bmdctl's copy, THEIRS the path in
# bmdwire. A THEIRS that doesn't exist is an error when REQUIRED, and is
# otherwise listed as not compared.
compare() {
	local ours=$1 theirs=$2 required=$3 name diff status
	if [ ! -f "$bmdwire/$theirs" ]; then
		if [ "$required" = required ]; then
			echo "$theirs is not in bmdwire ($main)" >&2
			exit 1
		fi
		echo "not compared: $theirs is not on bmdwire main"
		printf -- '- `%s` is not on bmdwire main, so `%s` has nothing to match yet.\n' \
			"$theirs" "$ours" >>"$missing"
		return
	fi
	name=$(basename "$ours" .md)
	diff=$work/$name.diff
	copied "$ours" >"$work/$name.ours"
	status=0
	diff -u --label "bmdctl $ours" --label "bmdwire main $theirs" \
		"$work/$name.ours" "$bmdwire/$theirs" >"$diff" || status=$?
	case $status in
	0)
		echo "same: $ours and $theirs"
		return
		;;
	1) ;;
	*) exit "$status" ;;
	esac
	drift=1
	cp "$diff" "$out/$name.diff"
	echo "differ: $ours and $theirs ($(grep -c '^@@' "$diff") hunks)"
	{
		printf '### `%s` and `%s`\n\n' "$ours" "$theirs"
		printf '`-` lines are bmdctl'\''s copy, `+` lines bmdwire main.\n\n'
		printf '%sdiff\n' "$(fence "$diff")"
		awk -v max="$budget" '
			{ n += length($0) + 1; if (n > max) { cut = 1; exit } print }
			END { if (cut) print "... (cut here; the whole diff is in the run'\''s artifact)" }
		' "$diff"
		printf '%s\n\n' "$(fence "$diff")"
	} >>"$sections"
	budget=$((budget - $(wc -c <"$diff")))
	[ "$budget" -lt 0 ] && budget=0
	return 0
}

compare docs/SURFACE-atem.md atemwire/docs/SURFACE.md required
# hyperdeckwire has no SURFACE.md yet (docs/PORTING.md, "Source of
# truth"); once bmdwire has one, make this required too.
compare docs/SURFACE-hyperdeck.md hyperdeckwire/docs/SURFACE.md optional

if [ "$drift" -eq 0 ]; then
	exit 0
fi

{
	printf 'bmdctl'\''s copies of bmdwire'\''s SURFACE.md no longer match bmdwire main '
	printf '([`%s`](%s/commit/%s)).\n' "${main:0:7}" "$repo_url" "$main"
	printf 'The port tracks bmdwire `%s` (`docs/PORTING.md`, "Source of truth"); ' "$tracked"
	printf 'bmdwire since then: [`%s...%s`](%s/compare/%s...%s).\n\n' \
		"$base" "${main:0:7}" "$repo_url" "$base" "$main"
	printf 'Port the bmdwire PRs behind the change (`CLAUDE.md`, "bmdwire first"), '
	printf 'copy SURFACE.md over again below its header, and move the `Tracks:` line. '
	printf 'The nightly SURFACE drift workflow'
	if [ -n "${RUN_URL:-}" ]; then
		printf ' ([last run](%s))' "$RUN_URL"
	fi
	printf ' rewrites this issue while they differ and closes it once they match.\n\n'
	cat "$sections"
	if [ -s "$missing" ]; then
		printf '### Not compared\n\n'
		cat "$missing"
	fi
} >"$out/body.md"
