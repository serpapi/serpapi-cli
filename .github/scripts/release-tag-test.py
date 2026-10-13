#!/usr/bin/env python3
"""Tests release-tag.sh in scratch git repos, offline: a stub gh answers the
two API calls the script makes, a merged pull request's labels and creating a
tag ref (a local git tag, refused when it exists, as the API refuses with 422).
Prints one line per case and exits 1 if any fails. CI runs it before tagging.
Usage: release-tag-test.py [SCRIPT]"""
import os
import shutil
import stat
import subprocess
import sys
import tempfile

STUB = r"""#!/usr/bin/env bash
echo "gh $*" >> "$STUB_LOG"
if [ "$1 $2" = "api repos/serpapi/serpapi-cli/git/refs" ]; then
  ref=${4#ref=refs/tags/}; sha=${6#sha=}
  if [ "$ref" = "${STUB_EXISTS:-}" ] || git rev-parse -q --verify "refs/tags/$ref" >/dev/null; then echo "Reference already exists (HTTP 422)" >&2; exit 1; fi
  [ -n "${STUB_FAIL_CREATE:-}" ] && { echo "HTTP 500" >&2; exit 1; }
  git tag "$ref" "$sha"; exit 0
fi
case "$2" in repos/serpapi/serpapi-cli/commits/*/pulls)
  [ -n "${STUB_FAIL_LABELS:-}" ] && { echo "HTTP 502" >&2; exit 1; }
  sha=${2#repos/serpapi/serpapi-cli/commits/}; sha=${sha%/pulls}
  if [ -f "${STUB_DIR:-/nonexistent}/labels-$sha" ]; then cat "$STUB_DIR/labels-$sha"; else printf '%s\n' "${STUB_LABELS:-}"; fi
  exit 0 ;;
esac
echo "unexpected gh call: $*" >&2; exit 3
"""


def git(repo, *args):
    return subprocess.run(["git", "-C", repo, *args], check=True, capture_output=True, text=True).stdout.strip()


def commit(repo, path, text="x"):
    full = os.path.join(repo, path)
    os.makedirs(os.path.dirname(full) or repo, exist_ok=True)
    with open(full, "a") as f:
        f.write(text + "\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-q", "-m", f"change {path}")
    return git(repo, "rev-parse", "HEAD")


def fresh(root, tags=True):
    repo = tempfile.mkdtemp(dir=root)
    git(repo, "init", "-q", "-b", "main")
    git(repo, "config", "user.email", "t@example.com")
    git(repo, "config", "user.name", "t")
    base = commit(repo, "pkg/cmd/root.go", "package cmd")
    if tags:
        git(repo, "tag", "v0.2.2", base)
        base = commit(repo, "pkg/cmd/search.go", "package cmd")
        git(repo, "tag", "v0.2.3", base)
    return repo, base


def run(script, repo, root, **env):
    out = os.path.join(root, "out")
    open(out, "w").close()
    log = os.path.join(root, "gh.log")
    open(log, "w").close()
    e = {"PATH": f"{root}/bin:{os.environ['PATH']}", "GITHUB_OUTPUT": out, "GITHUB_REPOSITORY": "serpapi/serpapi-cli",
         "STUB_LOG": log, "HOME": root}
    e.update({k: v for k, v in env.items() if v is not None})
    p = subprocess.run(["bash", script], cwd=repo, env=e, capture_output=True, text=True)
    outputs = dict(line.split("=", 1) for line in open(out).read().splitlines() if "=" in line)
    return p.returncode, p.stdout.strip(), p.stderr.strip(), outputs, open(log).read()


def main():
    script = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "release-tag.sh"))
    root = tempfile.mkdtemp(prefix="release-tag-test-")
    os.makedirs(f"{root}/bin")
    with open(f"{root}/bin/gh", "w") as f:
        f.write(STUB)
    os.chmod(f"{root}/bin/gh", stat.S_IRWXU)
    failures = 0

    def case(name, want_code, want_reason, want_tag, repo, sha, created=None, **env):
        nonlocal failures
        before = set(git(repo, "tag", "--list").split())
        code, stdout, stderr, outputs, log = run(script, repo, root, SHA=sha, **env)
        new_tags = sorted(set(git(repo, "tag", "--list").split()) - before)
        ok = (code == want_code and want_reason in (stdout + stderr) and outputs.get("tag", "") == want_tag
              and new_tags == ([created] if created else []))
        failures += not ok
        print(f"{'ok  ' if ok else 'FAIL'} {name:52} exit={code} tag={outputs.get('tag', '')!r} next={outputs.get('next', '')!r} "
              f"created={new_tags} :: {(stdout or stderr)[:95]}")
        return log

    repo, _ = fresh(root)
    code = commit(repo, "pkg/api/client.go", "package api")
    case("code change, no labels: patch", 0, "Tagged", "v0.2.4", repo, code, created="v0.2.4", LABELS_JSON="[]")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("release:minor label", 0, "minor after v0.2.3", "v0.3.0", repo, code, created="v0.3.0", LABELS_JSON='["release:minor"]')
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("release:major label", 0, "major after v0.2.3", "v1.0.0", repo, code, created="v1.0.0", LABELS_JSON='["release:major"]')
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("release:skip label", 0, "release:skip", "", repo, code, LABELS_JSON='["release:skip","release:minor"]')
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("multi-word label beside release:minor", 0, "minor", "v0.3.0", repo, code, created="v0.3.0",
         LABELS_JSON='["good first issue","release:minor"]')
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("label that only contains release:minor", 0, "patch after", "v0.2.4", repo, code, created="v0.2.4",
         LABELS_JSON='["not release:minor"]')
    repo, _ = fresh(root)
    for path in ["README.md", ".github/workflows/ci.yml", "pkg/cmd/x_test.go", "tests/e2e_test.go", "pkg/cmd/testdata/a.json", "AGENTS.md"]:
        docs = commit(repo, path)
    case("only docs, tests, testdata and CI changed", 0, "nothing that ships", "", repo, docs, LABELS_JSON='["release:minor"]')
    case("explicit BUMP=minor releases them anyway", 0, "minor after v0.2.3", "v0.3.0", repo, docs, created="v0.3.0", BUMP="minor")
    repo, _ = fresh(root); sha = commit(repo, "go.mod", "module x")
    case("go.mod change ships", 0, "Tagged", "v0.2.4", repo, sha, created="v0.2.4", LABELS_JSON="[]")
    repo, _ = fresh(root); sha = commit(repo, ".goreleaser.yml", "version: 2")
    case(".goreleaser.yml change ships", 0, "Tagged", "v0.2.4", repo, sha, created="v0.2.4", LABELS_JSON="[]")
    repo, _ = fresh(root); old = commit(repo, "pkg/a.go"); new = commit(repo, "pkg/b.go"); git(repo, "tag", "v0.2.4", new)
    case("commit older than the latest release", 0, "older than v0.2.4", "", repo, old, LABELS_JSON="[]")
    case("commit already tagged", 0, "already tagged v0.2.4", "", repo, new, LABELS_JSON="[]")
    repo, base = fresh(root); git(repo, "checkout", "-q", "-b", "side", base); side = commit(repo, "pkg/side.go")
    git(repo, "tag", "v0.2.4", side); git(repo, "checkout", "-q", "main"); git(repo, "tag", "-d", "v0.2.4")
    git(repo, "tag", "v0.2.4-not-a-release", side)
    main_sha = commit(repo, "pkg/main.go")
    case("pre-release style tags don't count", 0, "patch after v0.2.3", "v0.2.4", repo, main_sha, created="v0.2.4", LABELS_JSON="[]")
    repo, base = fresh(root); git(repo, "checkout", "-q", "-b", "side", base); side = commit(repo, "pkg/side.go")
    git(repo, "checkout", "-q", "main"); main_sha = commit(repo, "pkg/main.go")
    git(repo, "tag", "v0.2.4", side)
    case("latest release on a branch main left (go-rewrite)", 0, "comparing their files", "v0.2.5", repo, main_sha, created="v0.2.5",
         LABELS_JSON="[]")
    repo, base = fresh(root); git(repo, "checkout", "-q", "-b", "side", base); side = commit(repo, "pkg/side.go")
    git(repo, "tag", "v0.2.4", side); git(repo, "checkout", "-q", "main"); git(repo, "merge", "-q", "--no-edit", "side")
    docs = commit(repo, "README.md")
    case("main has the release's files, plus docs", 0, "nothing that ships", "", repo, docs, LABELS_JSON="[]")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("next tag made meanwhile by another run", 1, "already exists", "", repo, code, LABELS_JSON="[]", STUB_EXISTS="v0.2.4")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("dry run creates nothing", 0, "Would release v0.3.0", "", repo, code, LABELS_JSON='["release:minor"]', DRY_RUN="true")
    repo, _ = fresh(root, tags=False); code = commit(repo, "pkg/api/client.go")
    case("no tags yet: first release", 0, "patch after v0.0.0", "v0.0.1", repo, code, created="v0.0.1", LABELS_JSON="[]")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    log = case("labels looked up from the merged PR", 0, "minor after", "v0.3.0", repo, code, created="v0.3.0", STUB_LABELS="release:minor")
    if f"commits/{code}/pulls" not in log:
        failures += 1
        print("FAIL the label lookup didn't ask for the commit's pull requests:", log.strip()[:120])
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("unknown BUMP", 2, "BUMP must be", "", repo, code, BUMP="bogus")
    repo, _ = fresh(root); major_pr = commit(repo, "pkg/api/client.go"); head = commit(repo, "pkg/cmd/root.go")
    with open(f"{root}/labels-{major_pr}", "w") as f:
        f.write("release:major\n")
    log = case("earlier release:major merge, unlabeled head", 0, "major after v0.2.3", "v1.0.0", repo, head, created="v1.0.0",
               STUB_DIR=root)
    if f"commits/{major_pr}/pulls" not in log:
        failures += 1
        print("FAIL the earlier merge's labels weren't looked up:", log.strip()[:160])
    repo, _ = fresh(root); skip_pr = commit(repo, "pkg/api/client.go"); head = commit(repo, "pkg/cmd/root.go")
    with open(f"{root}/labels-{skip_pr}", "w") as f:
        f.write("release:skip\n")
    case("earlier release:skip doesn't hold back the head", 0, "patch after v0.2.3", "v0.2.4", repo, head, created="v0.2.4",
         STUB_DIR=root)
    repo, _ = fresh(root); minor_pr = commit(repo, "pkg/api/client.go"); head = commit(repo, "pkg/cmd/root.go")
    with open(f"{root}/labels-{minor_pr}", "w") as f:
        f.write("release:minor\n")
    case("preview: PR labels plus an earlier minor merge", 0, "Would release v0.3.0", "", repo, head, LABELS_JSON="[]",
         DRY_RUN="true", STUB_DIR=root)
    repo, _ = fresh(root); docs_major = commit(repo, "README.md"); head = commit(repo, "pkg/cmd/root.go")
    with open(f"{root}/labels-{docs_major}", "w") as f:
        f.write("release:major\n")
    case("earlier docs-only release:major doesn't count", 0, "patch after v0.2.3", "v0.2.4", repo, head, created="v0.2.4",
         STUB_DIR=root)
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("tag API failure fails the job", 1, "HTTP 500", "", repo, code, LABELS_JSON="[]", STUB_FAIL_CREATE="1")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    case("label lookup failure fails the job", 1, "HTTP 502", "", repo, code, STUB_FAIL_LABELS="1")
    repo, _ = fresh(root); code = commit(repo, "pkg/api/client.go")
    log = case("dry run with labels makes no API call", 0, "Would release", "", repo, code, LABELS_JSON="[]", DRY_RUN="true")
    if log.strip():
        failures += 1
        print("FAIL dry run called gh:", log.strip()[:120])

    shutil.rmtree(root)
    print(f"{failures} failed")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
