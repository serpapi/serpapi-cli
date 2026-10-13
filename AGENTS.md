# AGENTS.md

## Releases

- A merge to `main` releases the CLI once CI passes on the merge commit, live e2e included; outside pull requests a skipped live test fails CI, so a release never rests on skipped tests. The CI `tag` job runs `.github/scripts/release-tag.sh`, which tags the commit with the next version, then calls `.github/workflows/release.yml` for that tag.
- The next version is a patch after the highest `vX.Y.Z` tag. Label the pull request `release:minor` or `release:major` for those, or `release:skip` to release nothing. A merge releases nothing either when nothing that ships changed since the latest release: `cmd/`, `pkg/`, `go.mod`, `go.sum` and `.goreleaser.yml`, tests excluded. The "Release preview" check on each pull request says what merging it releases.
- A tag reaches every Homebrew and `go install` user, so merging to `main` is the release decision: label the pull request first.
- `release.yml`:
  1. runs GoReleaser (`.goreleaser.yml`), which publishes a GitHub release with 5 archives (darwin and linux on amd64 and arm64, windows on amd64) and `serpapi_<version>_checksums.txt`, and stamps the version into `serpapi --version`. `.github/release-notes/<tag>.md`, when it exists, is the release body; otherwise GoReleaser lists the commits;
  2. writes `Formula/serpapi-cli.rb` in `serpapi/homebrew-tap` from those checksums and pushes it as a `serpapi-cli <version>` commit.
- To release by hand, run the CI workflow on `main` from the Actions tab with a bump (`dry_run` is on by default), or push a lightweight `v*` tag on a `main` commit whose CI passed with no `--- SKIP` in the e2e log; `release.yml` runs on the pushed tag.
- If a release job fails after the tag was made, re-run the failed jobs of that CI run, or, once the cause is fixed on `main`, run the Release workflow from the Actions tab with that tag. The tag stays.
- Never move, delete or reuse a published tag: Go's module proxy and checksum database keep the first content they saw for each version, so a moved tag never reaches most `go install` users and fails checksum verification for the rest.
- v0.2.1 to v0.2.3 are tagged on `go-rewrite`, which `main` doesn't contain; `release-tag.sh` compares files with the latest release, not history.
- To change a published release's notes: `gh release edit v0.3.0 -R serpapi/serpapi-cli --notes-file <file>`.
- Release binaries build with the latest stable Go (`go-version: stable`); the `go` line in `go.mod` is the oldest Go that `go install` users need.
- The formula's `test do` block in `release.yml` runs the released binary, e.g. `serpapi account` without a key must exit 2 with "No API key". Change it together with the behavior it checks.
- Check a release: the CI run's `tag` and `release` jobs (or, for a pushed tag, the Release run) are green, the release has the 5 archives and the checksums file, the tap has the `serpapi-cli <version>` commit, and `brew upgrade serpapi-cli && brew test serpapi-cli` passes.
- To pull a bad release, revert the formula commit in the tap so new installs get the previous version, and fix forward: release the next patch version with the bad one retracted in its `go.mod` (`retract v0.3.0` in v0.3.1), since `go install ...@latest` takes the highest tag that isn't retracted.
