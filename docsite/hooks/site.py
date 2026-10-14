# SPDX-License-Identifier: MIT
"""The documentation site's page hooks (mkdocs.yml, `hooks:`).

The docs are written to be read on GitHub too, so the files stay where
they are and as they are; these make them a site:

- URLs: a top-level page named in capitals (INSTALL.md, GUIDE.md) is
  served at its name in lower case (/install/, /guide/), so the guide's
  pages (guide/hosts.md) and the reference's (commands/...) sit under
  their index.
- Includes: a line `--8<-- "README.md:intro"` is replaced by the part of
  that file, at the repository's root, between
  `<!-- --8<-- [start:intro] -->` and `<!-- --8<-- [end:intro] -->`
  (the whole file without `:name`), as pymdownx.snippets would, but with
  its relative links rewritten to work from the page that includes it.
- Links: a relative link to a file the site doesn't build (Go source,
  the licences, PORTING.md) goes to it on GitHub instead.
"""

import os
import posixpath
import re

INCLUDE = re.compile(r'^--8<-- "([^":]+)(?::([\w-]+))?"[ \t]*$', re.M)
# Fenced code blocks and code spans, which are left alone, then links:
# [text](target) and ![alt](target), and reference definitions.
CODE = r"(?P<fence>^[ \t]*```.*?^[ \t]*```[ \t]*$)|(?P<span>`[^`\n]+`)"
LINK = r"(?P<pre>\]\()(?P<target>[^)\s]+)(?P<post>[^)]*\))|(?P<defpre>^\[[^\]]+\]:[ \t]+)(?P<deftarget>\S+)"
TOKENS = re.compile(CODE + "|" + LINK, re.M | re.S)

_root = _docs = _repo = None


def on_config(config):
    global _root, _docs, _repo
    _root = os.path.dirname(os.path.abspath(config.config_file_path))
    _docs = os.path.abspath(config.docs_dir)
    _repo = config.repo_url.rstrip("/")


def on_files(files, config):
    for f in files.documentation_pages():
        parent, name = posixpath.split(f.src_uri)
        stem = name[: -len(".md")]
        if not parent and stem.isupper():
            f.dest_uri = posixpath.join(stem.lower(), "index.html")
            for derived in ("url", "abs_dest_path"):
                f.__dict__.pop(derived, None)
    return files


def on_page_markdown(markdown, page, config, files):
    here = os.path.dirname(os.path.join(_docs, page.file.src_uri))
    markdown = INCLUDE.sub(lambda m: _include(m.group(1), m.group(2), here), markdown)
    built = {f.abs_src_path for f in files if f.abs_src_path and not f.inclusion.is_excluded()}
    return _links(markdown, lambda target: _to_github(target, here, built))


def _include(path, section, here):
    src = os.path.join(_root, path)
    with open(src, encoding="utf-8") as f:
        text = f.read()
    if section:
        start = f"<!-- --8<-- [start:{section}] -->"
        end = f"<!-- --8<-- [end:{section}] -->"
        if start not in text or end not in text:
            raise ValueError(f"{path} has no section {section!r}")
        text = text.split(start, 1)[1].split(end, 1)[0]
    there = os.path.dirname(src)
    return _links(text.strip("\n"), lambda target: _moved(target, there, here))


def _links(markdown, rewrite):
    """markdown with each relative link's target passed through rewrite,
    code left alone."""

    def sub(m):
        if m.group("fence") or m.group("span"):
            return m.group(0)
        if m.group("target") is not None:
            return m.group("pre") + _relative(m.group("target"), rewrite) + m.group("post")
        return m.group("defpre") + _relative(m.group("deftarget"), rewrite)

    return TOKENS.sub(sub, markdown)


def _relative(target, rewrite):
    if re.match(r"^([a-z][a-z0-9+.-]*:|#|/)", target):
        return target  # absolute, or within the page
    return rewrite(target)


def _split(target):
    path, _, fragment = target.partition("#")
    return path, ("#" + fragment if fragment else "")


def _moved(target, there, here):
    """A link written in a file in there, rewritten for a page in here."""
    path, fragment = _split(target)
    moved = os.path.relpath(os.path.normpath(os.path.join(there, path)), here)
    return moved.replace(os.sep, "/") + fragment


def _to_github(target, here, built):
    """A link to a file the site doesn't build, to that file on GitHub."""
    path, fragment = _split(target)
    full = os.path.normpath(os.path.join(here, path))
    if full in built or not full.startswith(_root + os.sep) or not os.path.exists(full):
        return target  # a page or file of the site; or MkDocs says what is wrong
    kind = "tree" if os.path.isdir(full) else "blob"
    rel = os.path.relpath(full, _root).replace(os.sep, "/")
    return f"{_repo}/{kind}/main/{rel}{fragment}"
