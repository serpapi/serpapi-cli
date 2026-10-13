#!/usr/bin/env bash
# Decides whether a commit on main is released, and as which version, and
# tags it. ci.yml runs it after the tests pass on main; release-preview.yml
# runs it with DRY_RUN=true on a pull request's merge commit, to show what
# merging releases.
#
# Environment:
#   SHA         the commit
#   BUMP        auto (default), patch, minor or major. auto bumps minor or
#               major when any pull request merged since the latest release
#               has the release:minor or release:major label, else patch;
#               releases nothing when the merged pull request has
#               release:skip; and releases only when something that ships
#               changed since the latest release.
#   LABELS_JSON the pull request's labels as a JSON array; looked up from the
#               commit's merged pull request when unset
#   DRY_RUN     true to only report
#   GITHUB_REPOSITORY, GH_TOKEN for the API
# Outputs, to GITHUB_OUTPUT: tag, the tag created for release.yml (empty when
# nothing is released); next, the version this commit gets or would get;
# reason, one line on what happened.
set -euo pipefail

sha=$(git rev-parse --verify "${SHA:?SHA is required}^{commit}")
bump=${BUMP:-auto}
dry_run=${DRY_RUN:-false}

output() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"; fi
}

finish() {
	echo "$1"
	output reason "$1"
	exit 0
}

# Releases are vMAJOR.MINOR.PATCH; pre-release tags such as v1.0.0-rc.1 don't count.
release_tags() { git tag --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V || true; }

existing=$(git tag --points-at "$sha" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || true)
if [ -n "$existing" ]; then
	output next "$existing"
	finish "Not released again: $sha is already tagged $existing"
fi

pr_labels() {
	gh api "repos/${GITHUB_REPOSITORY:?}/commits/$1/pulls" --jq '.[] | select(.merged_at != null) | .labels[].name'
}

# The files between two commits that ship in the binary, tests excluded.
shipped() {
	git diff --name-only "$1" "$2" -- cmd pkg internal go.mod go.sum .goreleaser.yml \
		':(exclude)*_test.go' ':(exclude)*/testdata/*'
}

latest=$(release_tags | tail -n 1)
since=$(git hash-object -t tree /dev/null)
earlier=""
if [ -n "$latest" ]; then
	since=$(git rev-list -n 1 "$latest")
	if git merge-base --is-ancestor "$sha" "$since"; then
		finish "Not released: $sha is older than $latest"
	fi
	# v0.2.1 to v0.2.3 were tagged on go-rewrite after it merged, so main
	# doesn't contain them; comparing trees doesn't need it to.
	if ! git merge-base --is-ancestor "$since" "$sha"; then
		echo "$latest is on a commit that $sha doesn't contain; comparing their files"
	fi
	if git rev-parse -q --verify "$sha^" >/dev/null; then
		earlier=$(git rev-list --first-parent "$sha^" --not "$since")
	fi
else
	latest=v0.0.0
fi

if [ "$bump" = auto ]; then
	if [ -n "${LABELS_JSON:-}" ]; then
		labels=$(jq -r '.[]' <<<"$LABELS_JSON")
	else
		labels=$(pr_labels "$sha")
	fi
	if grep -qx 'release:skip' <<<"$labels"; then
		finish "Not released: the pull request has the release:skip label"
	fi
	# A release:major or release:minor pull request merged since the latest
	# release counts too, since its own run may have failed, but only if it
	# changed what ships: a docs-only one released nothing.
	while read -r c; do
		if [ -n "$c" ] && [ -n "$(shipped "$c^" "$c")" ]; then labels+=$'\n'$(pr_labels "$c"); fi
	done <<<"$earlier"
	if grep -qx 'release:major' <<<"$labels"; then
		bump="major"
	elif grep -qx 'release:minor' <<<"$labels"; then
		bump="minor"
	else
		bump="patch"
	fi
	if [ -z "$(shipped "$since" "$sha")" ]; then
		finish "Not released: nothing that ships changed since $latest, only docs, tests or CI"
	fi
fi

IFS=. read -r major minor patch <<<"${latest#v}"
case $bump in
major) next="v$((major + 1)).0.0" ;;
minor) next="v$major.$((minor + 1)).0" ;;
patch) next="v$major.$minor.$((patch + 1))" ;;
*)
	echo "BUMP must be auto, patch, minor or major, not $bump" >&2
	exit 2
	;;
esac
output next "$next"

if git rev-parse -q --verify "refs/tags/$next" >/dev/null; then
	echo "::error::$next already exists on another commit; a published tag is never moved or reused" >&2
	exit 1
fi

if [ "$dry_run" = true ]; then
	finish "Would release $next ($bump after $latest) at $sha"
fi

# A lightweight tag, as the earlier releases have. The API refuses a tag that
# already exists, so two runs can't both make it.
gh api "repos/${GITHUB_REPOSITORY:?}/git/refs" -f ref="refs/tags/$next" -f sha="$sha" >/dev/null
output tag "$next"
finish "Tagged $sha as $next ($bump after $latest)"
