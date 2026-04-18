# releasetool

A utility for helping make software releases

## Overview

Right now this helps me create homebrew recipes for my golang projects by inspecting github and
generateing recipe files. See [my tap](https://github.com/deweysasser/homebrew-tap) for an example.

For each repo, releasetool writes one Homebrew formula per GitHub release:

- `{repo}.rb` — the **default** formula, pointing at the newest non-prerelease.
- `{repo}@{version}.rb` — a **versioned** formula per release (including prereleases).

Versioned files are immutable once written; re-running the tool only writes the default when the
newest stable release has moved. This lets tap users install any historical version or opt into an
`-rc` build without affecting the default install.

## Usage

```shell
releasetool brew deweysasser/cumulus
```

or

```shell
releasetool brew -f repos.yaml
```

Installing from the generated tap:

```shell
brew install deweysasser/tap/cumulus             # newest stable (default)
brew install deweysasser/tap/cumulus@1.2.0       # a specific stable release
brew install deweysasser/tap/cumulus@1.2.0-rc1   # opt into a prerelease
```

## How versions are detected

- **Prerelease flag**: GitHub's `prerelease: true` on a release is authoritative — those releases
  are emitted as versioned formulas only, never as the default.
- **Tag-suffix fallback**: tags matching `-rc`, `-alpha`, `-beta`, or `-pre` (case-insensitive) are
  treated as prereleases even when the flag is not set — both the compact form (`v1.0.0-rc1`) and
  the semver-dotted form (`v1.0.0-rc.1`, `v1.0.0-alpha.1.2`) are recognized. This is a safety net
  for repos that tag release candidates without checking the box.
- **Default selection**: the default unversioned formula points at the newest release that neither
  rule flags as a prerelease. If a repo has only prereleases, no default formula is written and a
  warning is logged.

## The Config file

```yaml
owner: deweysasser
tap: deweysasser/tap
recipes:
  - owner: deweysasser
    repo: cumulus
    description: Bulk access to multiple AWS clouds
  - repo: olympus

  - owner: deweysasser
    repo: cumulus-private
docs:
  - file: examples/README-test.md
    section: "## Software"
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token. Required when generating formulas for private repositories. Used to authenticate GitHub API requests. |
| `HOMEBREW_GITHUB_API_TOKEN` | Used by generated Homebrew formulas at install time to download assets from private GitHub repositories. Set this in your Homebrew environment when installing private formulas. |

## Why this tool?

Because I am annoyed by "open core" software that tries to leverage Open Source to build a market
for proprietary "features". Maybe I'll get over that annoyance and depreate this project. Maybe not.

Also, this tool generates live from github rather than during the build process. Both can be useful
-- this is the pattern that works for me right now.