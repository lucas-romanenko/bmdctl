# SPDX-License-Identifier: MIT
"""The site colours terminal samples as bmdctl does: hooks/terminal.py's
colours, checked against the CLI's own. The terminal goldens in
cmd/bmdctl/testdata/terminal are what bmdctl prints at a terminal, with
its colours as tags; each sample in them, the tags taken out, must come
back from terminal.colour() as the golden has it.

    python3 docsite/test_terminal.py
"""

import pathlib
import re
import sys
import unittest

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE / "hooks"))

import terminal  # noqa: E402

GOLDENS = HERE.parent / "cmd" / "bmdctl" / "testdata" / "terminal"
UNTAG = re.compile(r"<(b|dim|red|green|/)>")
SKIP = {"spinner.golden"}  # the spinner's frames, redrawn in place on stderr
CONTEXT = {  # goldens of rows alone: the command they come from
    "watch.golden": "$ bmdctl watch",
    "steps.golden": "$ bmdctl profile save show.xml",
}


def samples(name):
    """A golden's samples: each a list of lines, the prompt's first, with
    stderr's "! " and the exit status taken out."""
    lines = (GOLDENS / name).read_text(encoding="utf-8").rstrip("\n").split("\n")
    if name in CONTEXT:
        lines.insert(0, CONTEXT[name])
    out = []
    for line in lines:
        if re.fullmatch(r"\[exit \d+\]", line):
            continue
        line = line[2:] if line.startswith("! ") else line
        if line.startswith("$ ") or not out:
            out.append([])
        out[-1].append(line)
    return out


class TestColours(unittest.TestCase):
    def test_goldens(self):
        names = sorted(p.name for p in GOLDENS.glob("*.golden") if p.name not in SKIP)
        self.assertGreater(len(names), 10)
        checked = 0
        for name in names:
            for want in samples(name):
                plain = [UNTAG.sub("", line) for line in want]
                if want == plain and terminal.kind(plain[0][2:]) == "device":
                    continue  # run with NO_COLOR
                with self.subTest(golden=name, command=want[0]):
                    self.assertEqual("\n".join(terminal.colour(plain)), "\n".join(want))
                checked += 1
        self.assertGreater(checked, 80)

    def test_plain(self):
        for command in ("bmdctl program cam1 | cat", "bmdctl --json cut", "bmdctl --dry-run program 2",
                        "bmdctl hosts", "bmdctl engine status", "curl -s localhost:8880/hosts"):
            self.assertEqual(terminal.kind(command), "plain", command)

    def test_html(self):
        got = terminal.sample_html('$ bmdctl program "Camera 1"\nSTUDIO A  PGM CAM2 · PVW CAM1\n'
                                   "  program     CAM2 → CAM1 <x>     21 ms\nerror  no input\n")
        self.assertIn('<span class="prompt">$</span> <span class="cmd">bmdctl program "Camera 1"</span>', got)
        self.assertIn('<span class="b">STUDIO A</span>  <span class="dim">PGM CAM2 · PVW CAM1</span>', got)
        self.assertIn("&lt;x&gt;", got)
        self.assertIn('<span class="err">error</span>', got)
        self.assertIsNone(terminal.sample_html("bmdctl cut\n"))


if __name__ == "__main__":
    unittest.main()
