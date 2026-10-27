#!/usr/bin/env python3
"""Run every command of a printed CLI against its live instance and report problems.

Usage: python3 scripts/sweep.py <cli-dir>

The sweep loads the CLI's own credentials (<cli-dir>/.env, and config.toml as
<NAME>_CONFIG), builds the CLI, walks its whole command tree from `agent-context`
(hidden groups included) and, in a throwaway --home:

- runs every GET leaf and every read-only framework leaf live, with --agent;
- runs every POST/PUT/PATCH/DELETE leaf with --dry-run, so nothing changes.

It reports a non-zero exit, stdout that is not JSON (or JSON lines), unexpected stderr (only
`hint:` lines, sync/tail progress events and the dry-run request echo are expected),
--agent rows cut to one field while --json rows carry more, and PUT/PATCH bodies
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


def load_credentials(cli_dir, name):
    """Load <cli-dir>/.env and export <NAME>_CONFIG=<cli-dir>/config.toml, as CLAUDE.md
    describes, so a run never inherits another CLI's environment or the default instance."""
    env_file = os.path.join(cli_dir, ".env")
    config = os.path.join(cli_dir, "config.toml")
    if not os.path.exists(env_file) and not os.path.exists(config):
        templates = [t for t in ("config.toml.example", ".env.template")
                     if os.path.exists(os.path.join(cli_dir, t))]
        if templates:
            sys.exit(f"{cli_dir}: no config.toml or .env; create one from {' or '.join(templates)}")
        return
    if os.path.exists(env_file):
        with open(env_file) as f:
            for line in f:
                key, sep, value = line.strip().partition("=")
                if sep and key and not key.startswith("#"):
                    os.environ[key] = value
    if os.path.exists(config):
        os.environ[name.removesuffix("-pp-cli").upper() + "_CONFIG"] = config


def run(binary, args, home, agent=True):
    proc = subprocess.run(
        [binary, *args, "--agent" if agent else "--json", "--home", home],
        capture_output=True, text=True, timeout=120,
    )
    return proc.returncode, proc.stdout, proc.stderr


def rows(out):
    """The result rows of a list response: a top-level array or the envelope's results."""
    try:
        doc = json.loads(out)
    except json.JSONDecodeError:
        return None
    if isinstance(doc, dict):
        doc = doc.get("results")
    if isinstance(doc, list) and doc and all(isinstance(r, dict) for r in doc):
        return doc
    return None


def thin_agent_rows(binary, args, home, agent_out):
    """--agent rows of one key or none while --json rows carry more: compaction dropped
    what identifies the row (the {"type": ...} bug). Returns the --json key count, or 0."""
    agent_rows = rows(agent_out)
    if not agent_rows or max(len(r) for r in agent_rows) > 1:
        return 0
    rc, out, _ = run(binary, args, home, agent=False)
    full = rows(out) if rc == 0 else None
    keys = max(len(r) for r in full) if full else 0
    return keys if keys > 1 else 0


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
        if not s or s.startswith("hint:") or is_event(s):
            continue
        if dry_run and not (s.startswith("warning:") or s.startswith("Error")):
            continue  # the echoed request, headers and body
        noise.append(s)
    return noise


# Progress events that report no problem. Newer prints send them to stderr under
# --agent; warnings, anomalies, truncation and errors stay reported.
PROGRESS_EVENTS = {
    "sync_start", "sync_progress", "sync_complete", "sync_dryrun",
    "page_fetch", "complete", "reconcile", "data", "output_metadata",
}


def is_event(line):
    """A progress event that reports no problem (sync_summary only when clean)."""
    try:
        doc = json.loads(line)
    except json.JSONDecodeError:
        return False
    event = doc.get("event") if isinstance(doc, dict) else None
    if not isinstance(event, str):
        return False
    if event == "sync_summary":
        return not doc.get("errored") and not doc.get("warned")
    if event == "output_metadata":
        return not (doc.get("meta") or {}).get("truncated")
    return event in PROGRESS_EVENTS


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
    load_credentials(cli_dir, name)
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
        elif not dry_run:
            keys = thin_agent_rows(binary, args, home, out)
            if keys:
                problems.append(f"{label}: --agent rows keep at most one field, --json rows carry {keys}")
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
