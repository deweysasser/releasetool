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