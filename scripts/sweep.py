#!/usr/bin/env python3
"""Run every command of a printed CLI against its live instance and report problems.

Usage: python3 scripts/sweep.py <cli-dir>

Load the CLI's credentials first (see CLAUDE.md). The sweep builds the CLI,
walks its whole command tree from `agent-context` (hidden groups included) and,
in a throwaway --home:

- runs every GET leaf and every read-only framework leaf live, with --agent;
- runs every POST/PUT/PATCH/DELETE leaf with --dry-run, so nothing changes.

It reports a non-zero exit, stdout that is not JSON (or JSON lines), unexpected stderr (only
`hint:` lines and the dry-run request echo are expected), and PUT/PATCH bodies
carrying fields the run did not set.

Sample arguments live in <cli-dir>/.sweep, one directive per line:

    var NAME = <command> | <jq filter>   # resolved live, used as {NAME}
    run <command path> = <args>          # arguments for a leaf
    skip <command path> = <reason>       # never run this leaf

A leaf that needs a positional argument and has no `run` line, or a framework
leaf that is neither read-only nor listed, is reported as uncovered. Exit 0
only when nothing is reported.
"""

import json
import os
import re
import shlex
import subprocess
import sys
import tempfile

WRITE_METHODS = {"POST", "PUT", "PATCH", "DELETE"}
SKIP_NAMES = {"help", "completion"}


def load_directives(path):
    variables, runs, skips = {}, {}, {}
    if not os.path.exists(path):
        return variables, runs, skips
    with open(path) as f:
        for n, raw in enumerate(f, 1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            kind, _, rest = line.partition(" ")
            key, sep, value = rest.partition("=")
            if not sep or kind not in ("var", "run", "skip"):
                sys.exit(f"{path}:{n}: expected 'var|run|skip <name> = <value>'")
            {"var": variables, "run": runs, "skip": skips}[kind][key.strip()] = value.strip()
    return variables, runs, skips


def leaves(node, path):
    path = path + [node["name"]]
    if path[0] in SKIP_NAMES:
        return
    subs = node.get("subcommands") or []
    if not subs:
        yield " ".join(path), node
    for sub in subs:
        yield from leaves(sub, path)


def run(binary, args, home):
    proc = subprocess.run(
        [binary, *args, "--agent", "--home", home],
        capture_output=True, text=True, timeout=120,
    )
    return proc.returncode, proc.stdout, proc.stderr


def resolve_var(binary, home, spec):
    command, _, jq_filter = spec.partition("|")
    rc, out, err = run(binary, shlex.split(command), home)
    if rc != 0:
        sys.exit(f"var '{spec}': command failed: {err.strip()}")
    value = subprocess.run(["jq", "-r", jq_filter.strip()], input=out,
                           capture_output=True, text=True).stdout.strip()
    if not value or value == "null":
        sys.exit(f"var '{spec}': jq filter produced nothing")
    return value


def dry_run_body(stderr):
    """Return the JSON body the dry run echoed after 'Body:', or None."""
    lines = stderr.splitlines()
    for i, line in enumerate(lines):
        if line.strip() == "Body:":
            buf = ""
            for nxt in lines[i + 1:]:
                buf += nxt + "\n"
                try:
                    return json.loads(buf)
                except json.JSONDecodeError:
                    continue
    return None


def unexpected_stderr(stderr, dry_run):
    noise = []
    for line in stderr.splitlines():
        s = line.strip()
        if not s or s.startswith("hint:"):
            continue
        if dry_run and not (s.startswith("warning:") or s.startswith("Error")):
            continue  # the echoed request, headers and body
        noise.append(s)
    return noise


def is_json(out):
    """A JSON document, or JSON lines (sync and tail stream events)."""
    try:
        json.loads(out)
        return True
    except json.JSONDecodeError:
        pass
    lines = [ln for ln in out.splitlines() if ln.strip()]
    try:
        return bool(lines) and all(json.loads(ln) is not None for ln in lines)
    except json.JSONDecodeError:
        return False


def set_flags(args):
    """Body keys the given flags may produce: --time-between-check-days -> time_between_check..."""
    return [a[2:].split("=")[0].replace("-", "_") for a in args if a.startswith("--")]


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    cli_dir = sys.argv[1].rstrip("/")
    name = os.path.basename(os.path.abspath(cli_dir))
    subprocess.run(["make", "-s", "build"], cwd=cli_dir, check=True)
    binary = os.path.join(os.path.abspath(cli_dir), "bin", name)
    variables, runs, skips = load_directives(os.path.join(cli_dir, ".sweep"))

    home = tempfile.mkdtemp(prefix="sweep-")
    tree = json.loads(run(binary, ["agent-context"], home)[1])
    values = {k: resolve_var(binary, home, v) for k, v in variables.items()}

    problems, ran = [], 0
    for path, node in (leaf for top in tree["commands"] for leaf in leaves(top, [])):
        if path in skips:
            continue
        ann = node.get("annotations") or {}
        method = ann.get("pp:method", "")
        read_only = ann.get("mcp:read-only") == "true"
        if path in runs:
            extra = shlex.split(runs[path].format(**values))
        elif "<" in node.get("use", ""):
            problems.append(f"{path}: needs arguments; add a 'run' or 'skip' line to .sweep")
            continue
        elif method or read_only:
            extra = []
        else:
            problems.append(f"{path}: framework command, neither read-only nor in .sweep")
            continue

        dry_run = method in WRITE_METHODS
        args = path.split() + extra + (["--dry-run"] if dry_run else [])
        rc, out, err = run(binary, args, home)
        ran += 1
        label = " ".join(args)
        if rc != 0:
            problems.append(f"{label}: exit {rc}: {err.strip().splitlines()[-1] if err.strip() else ''}")
            continue
        if not is_json(out):
            problems.append(f"{label}: stdout is not JSON")
        for line in unexpected_stderr(err, dry_run):
            problems.append(f"{label}: stderr: {line}")
        if method in ("PUT", "PATCH"):
            body = dry_run_body(err) or {}
            given = set_flags(extra)
            stray = [k for k in body if not any(g == k or g.startswith(k + "_") for g in given)]
            if stray:
                problems.append(f"{label}: body carries fields the run did not set: {', '.join(sorted(stray))}")

    for p in problems:
        print(p)
    print(f"sweep: {ran} commands run, {len(problems)} problem(s)", file=sys.stderr)
    sys.exit(1 if problems else 0)


if __name__ == "__main__":
    main()
