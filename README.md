# releasetool

A utility for helping make software releases

## Overview

Right now this helps me create homebrew recipes for my golang projects by inspecting github and
generateing recipe files. See [my tap](https://github.com/deweysasser/homebrew-tap) for an example.

## Usage

```shell
releasetool brew deweysasser/cumulus
```

or

```shell
releasetool brew -f repos.yaml
```

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