# AGENTS.md

## Releases

- A push or merge to `main` releases nothing. It runs CI (`.github/workflows/ci.yml`), whose e2e job makes real searches with the `SERPAPI_KEY` secret.
- Only a pushed `v*` tag releases. `.github/workflows/release.yml` then:
  1. runs GoReleaser (`.goreleaser.yml`), which publishes a GitHub release with 5 archives (darwin and linux on amd64 and arm64, windows on amd64) and `serpapi_<version>_checksums.txt`, and stamps the version into `serpapi --version`;
  2. writes `Formula/serpapi-cli.rb` in `serpapi/homebrew-tap` from those checksums and pushes it as a `serpapi-cli <version>` commit.
- A tag reaches every Homebrew and `go install` user, so tagging is a maintainer's call. Tag the merge commit on `main` once its CI has passed, live e2e included: the live tests skip without `SERPAPI_KEY`, so the e2e log must have no `--- SKIP` line. Tags here are lightweight:
  ```bash
  git fetch origin
  git tag v0.3.0 <merge commit sha>
  git rev-list -n1 v0.3.0   # must print that sha
  git push origin v0.3.0
  ```
- Never move, delete or reuse a published tag: Go's module proxy and checksum database keep the first content they saw for each version, so a moved tag never reaches most `go install` users and fails checksum verification for the rest.
- The release body is GoReleaser's list of commits. To publish written notes instead, run `gh release edit v0.3.0 -R serpapi/serpapi-cli --notes-file <file>` after the Release run.
- Release binaries build with the latest stable Go (`go-version: stable`); the `go` line in `go.mod` is the oldest Go that `go install` users need.
- The formula's `test do` block in `release.yml` runs the released binary, e.g. `serpapi account` without a key must exit 2 with "No API key". Change it together with the behavior it checks.
- Check a release: the Release run is green, the release has the 5 archives and the checksums file, the tap has the `serpapi-cli <version>` commit, and `brew upgrade serpapi-cli && brew test serpapi-cli` passes.
- To pull a bad release, revert the formula commit in the tap so new installs get the previous version, and fix forward: release the next patch version with the bad one retracted in its `go.mod` (`retract v0.3.0` in v0.3.1), since `go install ...@latest` takes the highest tag that isn't retracted.
