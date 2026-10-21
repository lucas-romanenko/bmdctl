# SPDX-License-Identifier: MIT
"""Terminal samples, coloured as bmdctl colours them (mkdocs.yml, `hooks:`).

A code block whose first line is a prompt (`$ bmdctl ...`) is a terminal
sample. The docs keep them plain, to read on GitHub; the site shows them
as docs/terminal-mock.html draws a terminal, with bmdctl's colours put
back by the rules the CLI colours by (CLAUDE.md, "Terminal output"):
the switcher's name and bmdctl's own lines bold, the value now on
program red and on preview green, ids, times, hints and empties dim,
`error` red. Output that isn't a terminal's (a pipe, --json, --dry-run,
the built-ins that print plain tables) or isn't bmdctl's stays plain.

colour() returns lines in the tags of the CLI's terminal goldens
(cmd/bmdctl/testdata/terminal: <b>, <dim>, <red>, <green>, </>), and
docsite/test_terminal.py checks it against them; a page gets spans
(.b, .dim, .pgm, .pvw, .err) that docsite/overrides/assets/stylesheets/
bmdctl.css colours.
"""

import html
import re
import shlex

# A fenced block without a language, as pymdownx.highlight renders it.
PRE = re.compile(r'<div class="highlight"><pre><span></span><code>(.*?)</code></pre></div>', re.S)

TIME = r"\d\d:\d\d:\d\d"
STATUS = re.compile(r"^(.*\S)(\s{2,})(\d+ ms|\d+(?:\.\d+)? s|# ms|sent|already|rejected)$")
HINT = re.compile(
    r"^\s+(tab completes input names|nothing to undo|cancelled|stopped at line \d+|done · .*"
    r"|\d+ empty slots? hidden · .*|bmdctl .*|\* default · .*|no authentication: .*"
    r"|not saved: .*|.* means input \d+ .*|.* is unverified on hardware)$"
)
BOLD = re.compile(r'^\s+(saved as ".*", now the default|".*" is now the default)$')
IP = re.compile(r"\d+\.\d+\.\d+\.\d+")

# What prints plain: these flags, and the built-ins whose output is a
# plain table or line (hosts alias prints a header, as a device does).
PLAIN_FLAGS = {"--json", "--quiet", "-q", "--dry-run", "--no-color"}
PLAIN_BUILTINS = {"hosts", "engine", "version", "help", "man", "completion", "doctor", "mcp", "state"}
GLOBAL_WITH_VALUE = {"--host", "--fps", "--timeout"}


def tag(name, text):
    return f"<{name}>{text}</>" if text else ""


def indented(line, name):
    """line with what follows its indent in tag name."""
    n = len(line) - len(line.lstrip())
    return line[:n] + tag(name, line[n:])


def words(command):
    """A command's words: the program, global flags and their values
    left out."""
    try:
        ws = shlex.split(command, comments=True)[1:]
    except ValueError:
        return []
    out, skip = [], False
    for w in ws:
        if skip:
            skip = False
        elif w in GLOBAL_WITH_VALUE:
            skip = True
        elif w not in ("-t", "--timestamps", "-y", "--yes"):
            out.append(w)
    return out


def kind(command):
    """What a command prints: plain, the overview (bmdctl alone), serve's
    log, or a device's views."""
    try:
        ws = shlex.split(command, comments=True)
    except ValueError:
        return "plain"
    if not ws or ws[0] not in ("bmdctl", "hdctl", "atemctl", "vhctl", "ultctl") or re.search(r"[|;>]", command) or PLAIN_FLAGS & set(ws):
        return "plain"
    rest = words(command)
    if not rest:
        return "overview"
    if rest[0] in PLAIN_BUILTINS and rest[:2] != ["hosts", "alias"]:
        return "plain"
    return "serve" if rest[0] == "serve" else "device"


class Sample:
    """One command and what it printed, coloured line by line."""

    def __init__(self, command):
        self.kind = kind(command)
        self.view = " ".join(w for w in words(command) if not w.startswith("-"))
        self.header_done = False
        self.error_at = None  # an error's column: deeper lines are its hint

    def colour(self, line):
        if self.kind == "plain" or not line.strip():
            return line
        if self.kind == "overview":
            return overview(line)
        if self.kind == "serve":
            return serve(line)
        return self.device(line)

    def device(self, line):
        m = re.match(r"^( *)((?:\d+  )?)error( {2,}.*)?$", line)
        if m:
            indent, number, rest = m.group(1), m.group(2), m.group(3) or ""
            self.header_done = True
            if indent and not number and rest.startswith("   "):
                self.error_at = None  # a profile's row, error its verb
            else:
                self.error_at = len(indent) + len(number)
            return indent + number + tag("red", "error") + rest
        if self.error_at is not None:
            if len(line) - len(line.lstrip()) > self.error_at:
                return indented(line, "dim")
            self.error_at = None
        if not self.header_done:
            self.header_done = True
            if not line.startswith(" "):
                return header(line)
        if HINT.match(line):
            return indented(line, "dim")
        if BOLD.match(line):
            return indented(line, "b")
        v = self.view
        if re.search(r"(^| )(status|outputs)$", v) and HUB_ROW.match(line):
            return hub_row(line)
        if re.search(r"(^| )(status|show keys|show dsk)$", v):
            return status_row(line)
        if re.search(r"(^| )inputs$", v):
            return inputs_row(line)
        if re.search(r"(^| )(media list|show media|macro list|show macros|video-modes|show hyperdecks)$", v):
            return list_row(line)
        if re.search(r"(^| )info$", v):
            return re.sub(r" (model \d+)$", lambda m: " " + tag("dim", m.group(1)), line)
        if re.search(r"(^| )clips$", v):
            return clip_row(line)
        if line.lstrip().startswith("✗"):  # a step that failed: deeper lines are its hint
            self.error_at = len(line) - len(line.lstrip())
        return result(line)


def header(line):
    """The switcher's name bold, what follows it dim."""
    name, sep, rest = line.partition("  ")
    if not sep:
        return tag("b", line)
    if rest == "disconnected":
        return f"{tag('b', name)}  {rest}"
    return f"{tag('b', name)}  {tag('dim', rest)}"


def result(line):
    """A result line: exec's line number, the time of day dim (-t,
    watch), the change, the status dim."""
    m = re.match(rf"^(  )((?:\d+  )?)((?:{TIME}  )?)(\S.*)$", line)
    if not m:
        return line
    indent, number, clock, rest = m.groups()
    status = ""
    s = STATUS.match(rest)
    if s:
        rest, status = s.group(1), s.group(2) + tag("dim", s.group(3))
    if clock:
        clock = tag("dim", clock[:-2]) + "  "
    return indent + number + clock + change(rest) + status


def change(rest):
    """The verb, then what it changed, coloured by what the verb is."""
    if rest in ("session dropped", "reconnected"):
        return tag("b", rest)
    if rest.startswith("✗"):
        return tag("red", rest)
    if rest.startswith("· "):
        return tag("dim", rest)
    m = re.match(r"^(\S.*?)( {2,})(.*)$", rest)
    if not m:
        return rest
    verb, lead, what = m.group(1), m.group(1) + m.group(2), m.group(3)
    if verb == "undo":
        u = re.match(r"^(\S.*?)( {2,})(.*)$", what)
        if u:
            verb, lead, what = "undo " + u.group(1), lead + u.group(1) + u.group(2), u.group(3)
    return lead + paint(verb, what)


def paint(verb, w):
    if re.search(r"\S+ on program · \S+ on preview$", w):  # cut, auto, their undo
        return re.sub(r"(\S+)( on program · )(\S+)( on preview)$",
                      lambda m: tag("red", m.group(1)) + m.group(2) + tag("green", m.group(3)) + m.group(4), w)
    if verb == "tally":
        w = re.sub(r"(\S+ pgm)", lambda m: tag("red", m.group(1)), w)
        return re.sub(r"(\S+ pvw)", lambda m: tag("green", m.group(1)), w)
    if re.fullmatch(r"(key|dsk) \d+", verb) and w == "on air":  # watch
        return tag("red", w)
    w = re.sub(r" (\([^()]*\))$", lambda m: " " + tag("dim", m.group(1)), w)  # what was typed
    w = re.sub(r'(was ".*")$', lambda m: tag("dim", m.group(1)), w)
    w = re.sub(r"(^|→ )none$", lambda m: m.group(1) + tag("dim", "none"), w)
    bus = None
    if re.fullmatch(r"(undo )?program( ME\d+)?", verb):
        bus = "red"
    elif re.fullmatch(r"(undo )?preview( ME\d+)?", verb):
        bus = "green"
    elif (re.match(r"(usk \d+|dsk) (on-air|toggle|auto)", verb) and w.endswith("→ on")
          or verb.startswith("ftb") and w.endswith("→ black") or verb == "record" and w.endswith("→ record")):
        bus = "red"  # a keyer on air, black on program, recording
    if bus:
        w = re.sub(r"^((?:.* )?→ )?([^ <]+)", lambda m: (m.group(1) or "") + tag(bus, m.group(2)), w)
    return w


def status_row(line):
    """status, show keys and show dsk (an ATEM's), and a deck's status."""
    line = re.sub(r"(PGM  \S+)", lambda m: tag("red", m.group(1)), line)
    line = re.sub(r"(PVW  \S+)", lambda m: tag("green", m.group(1)), line)
    m = re.match(r"^(.*\S / \S+  )(\S.*?)(\s{2,})(on air|off)$", line)
    if m:  # a keyer's settings
        line = m.group(1) + tag("dim", m.group(2)) + m.group(3) + m.group(4)
    line = re.sub(r"(\s{2,})on air$", lambda m: m.group(1) + tag("red", "on air"), line)
    line = re.sub(r"(\s{2,})off$", lambda m: m.group(1) + tag("dim", "off"), line)
    line = re.sub(r"^(  macro\s+)idle$", lambda m: m.group(1) + tag("dim", "idle"), line)
    line = re.sub(r"^(  clip\s+)(\d+)(  .*  )(\d\d:\d\d:\d\d:\d\d)$",
                  lambda m: m.group(1) + tag("dim", m.group(2)) + m.group(3) + tag("dim", m.group(4)), line)
    return re.sub(r"(\s{2,})(● active)$", lambda m: m.group(1) + tag("dim", m.group(2)), line)


# A Videohub's output: its number, its label, the input routed to it,
# maybe a lock at the line's end. No ATEM or deck status row starts with
# a number.
HUB_ROW = re.compile(r"^  ( {0,2}\d{1,3})  (\S.*)$")


def hub_row(line):
    """A router's output: its number dim, no input ("none") dim, a lock at
    the line's end dim."""
    m = HUB_ROW.match(line)
    rest = re.sub(r"(\s{2,})(locked|ours)$", lambda m: m.group(1) + tag("dim", m.group(2)), m.group(2))
    rest = re.sub(r"(\s{2,})none(\s|$)", lambda m: m.group(1) + tag("dim", "none") + m.group(2), rest)
    return f"  {tag('dim', m.group(1))}  {rest}"


def inputs_row(line):
    """An input: its number dim, and on the one on program or preview a dim
    leader to its mark; a router's, where it is routed to, dim."""
    m = re.match(r"^  (   |\s*\d+)  (.*)$", line)
    if not m:
        return line
    rest = re.sub(r"(\s{2,})(to \d+(?:, \d+)*|routed nowhere)$", lambda m: m.group(1) + tag("dim", m.group(2)), m.group(2))
    rest = re.sub(r"(·(?: ·)*)(\s+)(● PGM|○ PVW)$", lambda m: tag("dim", m.group(1)) + m.group(2) + m.group(3), rest)
    rest = re.sub(r"● PGM$", tag("red", "● PGM"), rest)
    rest = re.sub(r"○ PVW$", tag("green", "○ PVW"), rest)
    return f"  {tag('dim', m.group(1))}  {rest}"


def list_row(line):
    """A list of slots: the number dim, an empty slot dim, the player
    showing a still dim."""
    m = re.match(r"^  ( *\d+)  (.*)$", line)
    if not m:
        return line
    rest = re.sub(r"^\(empty\)", tag("dim", "(empty)"), m.group(2))
    rest = re.sub(r"(\s{2,})(MP\d)$", lambda m: m.group(1) + tag("dim", m.group(2)), rest)
    return f"  {tag('dim', m.group(1))}  {rest}"


def clip_row(line):
    """A deck's clip: its number dim, its name, the rest dim but the
    transport (● stopped)."""
    m = re.match(r"^  (\d+)(  .*)$", line)
    if not m:
        return line
    out, named = [], False
    for part in re.split(r"( {2,})", m.group(2)):
        if not part.strip():
            out.append(part)
        elif not named:
            out.append(part)
            named = True
        else:
            out.append(part if part.startswith("●") else tag("dim", part))
    return f"  {tag('dim', m.group(1))}" + "".join(out)


def overview(line):
    """bmdctl alone: its version dim, each host's name bold, its session
    after the dot dim but stale, the hint dim."""
    if line.startswith("bmdctl "):
        return tag("dim", line)
    if HINT.match(line):
        return indented(line, "dim")
    ip = IP.search(line)
    if not ip:
        return line
    head = line[: ip.start()]
    name = re.split(r" {2,}", head[2:].strip())[-1]
    at = head.rindex(name)
    line = line[:at] + tag("b", name) + line[at + len(name):]
    # The device's type, where its model had no room: dim.
    line = re.sub(r"(\d+\.\d+\.\d+\.\d+(?::\d+)?  )(ATEM|HD|VH|ULT)(?=  |$)",
                  lambda m: m.group(1) + tag("dim", m.group(2)), line)
    line = re.sub(r"(\s)(not connected)$", lambda m: m.group(1) + tag("dim", m.group(2)), line)

    def session(m):
        rest, stale = m.group(2), ""
        if rest.endswith("stale"):
            rest, stale = rest[: -len("stale")], "stale"
        return m.group(1) + tag("dim", rest) + stale

    return re.sub(r"(● \S+)( · .*)$", session, line)


def serve(line):
    """serve's header, its warning dim, and a line per request: the time
    and the answer dim."""
    if line.startswith("bmdctl serve"):
        return header(line)
    if HINT.match(line):
        return indented(line, "dim")
    m = re.match(rf"^(  )({TIME})(  .*?)(\s{{2,}})(\d{{3}} · .*)$", line)
    if m:
        return m.group(1) + tag("dim", m.group(2)) + m.group(3) + m.group(4) + tag("dim", m.group(5))
    return line


def colour(lines):
    """A terminal sample's lines, tagged as the goldens are; a prompt's
    line as it is."""
    out, sample = [], Sample("")
    for line in lines:
        if line.startswith("$ "):
            sample = Sample(line[2:])
            out.append(line)
        else:
            out.append(sample.colour(line))
    return out


SPANS = {"b": "b", "dim": "dim", "red": "pgm", "green": "pvw"}


def to_html(tagged):
    """A tagged line as HTML: the text escaped, each tag a span, the
    error's red a class of its own."""
    s = html.escape(tagged, quote=False)
    s = s.replace("&lt;red&gt;error&lt;/&gt;", '<span class="err">error</span>')
    for t, cls in SPANS.items():
        s = s.replace(f"&lt;{t}&gt;", f'<span class="{cls}">')
    return s.replace("&lt;/&gt;", "</span>")


def sample_html(text):
    """A terminal sample's HTML, or None if text isn't one."""
    lines = text.rstrip("\n").split("\n")
    if not lines[0].startswith("$ "):
        return None
    body = []
    for line, tagged in zip(lines, colour(lines)):
        if line.startswith("$ "):
            body.append(f'<span class="prompt">$</span> <span class="cmd">{html.escape(line[2:], quote=False)}</span>')
        else:
            body.append(to_html(tagged))
    return '<div class="term"><pre><code>' + "\n".join(body) + "</code></pre></div>"


def on_page_content(content, page, config, files):
    return PRE.sub(lambda m: sample_html(html.unescape(m.group(1))) or m.group(0), content)
