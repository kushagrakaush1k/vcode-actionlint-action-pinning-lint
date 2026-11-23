Configuration
=============

This document describes how to configure [actionlint](..) behavior.

Note that configuration file is optional. The author tries to keep configuration file as minimal as possible not to
bother users to configure behavior of actionlint. Running actionlint without configuration file would work fine in most
cases.

## Configuration file

Configuration file `actionlint.yaml` or `actionlint.yml` can be put in `.github` directory.

Note: If you're using [Super-Linter][], the file should be placed in a different directory. Please check the project's document.

```yaml
# Configuration related to self-hosted runner.
self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels:
    - linux.2xlarge
    - windows-latest-xl
    - linux-multi-gpu

# Configuration variables in array of strings defined in your repository or organization.
config-variables:
  - DEFAULT_RUNNER
  - JOB_NAME
  - ENVIRONMENT_STAGE

# Optional remote action and reusable workflow pinning checks.
# null disables the rule; {} enables it with level: semver.
action-pinning:
  level: semver
  allowed-owners: [my-org]
  allowed-actions: [vendor/shared-actions]

# Path-specific configurations.
paths:
  # Glob pattern relative to the repository root for matching files. The path separator is always '/'.
  # This example configures any YAML file under the '.github/workflows/' directory.
  .github/workflows/**/*.{yml,yaml}:
    # List of regular expressions to filter errors by the error messages.
    ignore:
      # Ignore the specific error from shellcheck
      - 'shellcheck reported issue in this script: SC2086:.+'
  # This pattern only matches '.github/workflows/release.yaml' file.
  .github/workflows/release.yaml:
    action-pinning:
      level: commit-sha
      allowed-actions: [vendor/release-actions]
    ignore:
      # Ignore errors from the old runner check. This may be useful for (outdated) self-hosted runner environment.
      - 'the runner of ".+" action is too old to run on GitHub Actions'
```

- `self-hosted-runner`: Configuration for your self-hosted runner environment.
  - `labels`: Label names added to your self-hosted runners as list of pattern. Glob syntax supported by [`path.Match`][pat]
    is available.
- `config-variables`: [Configuration variables][vars]. When an array is set, actionlint will check `vars` properties strictly.
  An empty array means no variable is allowed. The default value `null` disables the check.
- `action-pinning`: Check version pinning for remote step actions and job-level reusable workflows. Omitted or `null`
  leaves the rule off unless a matching path setting or CLI flag enables it; `{}` enables it with the default `semver` level.
  - `level`: One of `major-minor`, `semver` (default), or `commit-sha`, in increasing order of strictness. A ref accepted at a
    stricter level is also accepted at every weaker level. Other values are configuration errors.
    - `major-minor`: At least a major and minor version, such as `v1.2`.
    - `semver`: A complete major, minor, and patch version, such as `v1.2.3`. Prerelease and build metadata are allowed,
      such as `v1.2.3-rc.1+build.7`. The `v` prefix is optional for both version levels.
    - `commit-sha`: Exactly 40 lowercase hexadecimal characters.
  - `allowed-owners`: Owner names exempt from pinning, such as `my-org`. Names must contain only letters, digits,
    and internal hyphens; slashes, whitespace, refs, and wildcards are rejected.
  - `allowed-actions`: Exact `owner/repo` names exempt from pinning, such as `vendor/shared-actions`. Refs, subpaths,
    URLs, whitespace, and wildcards are rejected. The exemption includes actions in repository subdirectories and
    reusable workflows in that repository. Both allow lists match names case-insensitively.
  Local references starting with `./` and Docker references starting with `docker://` are skipped. Expressions in the
  repository or action name are skipped; expressions only in the ref produce a "cannot be verified" diagnostic.
  This rule validates ref syntax offline; it does not verify that a tag or commit exists remotely. See
  [version pinning](checks.md#version-pinning) for examples.
- `paths`: Configurations for specific file path patterns. This is a mapping from a glob pattern and the corresponding
  configuration.
  - `{glob}`: A file path glob pattern to apply the configuration. The path separator is always '/'. It is matched to the
    relative path from the repository root. For example `.github/workflows/**/*.yaml` matches all the workflow files (with
    `.yaml` file extension). For the glob syntax, please read the [doublestar][] library's documentation.
    - `ignore`: The configuration to ignore (filter) the errors by the error messages. This is an array of regular
      expressions. When one of the patterns matches the error message, the error will be ignored. It's similar to the
      `-ignore` command line option.
    - `action-pinning`: Pinning settings for matching files, using the same `level`, `allowed-owners`, and
      `allowed-actions` fields as the global section. A non-null section enables pinning even without global settings.
      An omitted level inherits the global level, or defaults to `semver` without a global section. An explicit path
      level overrides the global level; if multiple matching entries set levels, the strictest one wins.
      Global allow lists and allow lists from every matching path entry are combined. A path value of `null`
      contributes no settings and does not disable global pinning.

The `-action-pinning-level` CLI flag enables pinning and overrides both global and path-specific levels. It preserves
all configured allow lists. Invalid levels and allow-list entries are configuration errors even when the flag overrides
the level or an invalid path entry does not match the file being checked.

## Generate the initial configuration

You don't need to write the first configuration file by your hand. `actionlint` command can generate a default configuration
with `-init-config` flag.

```sh
actionlint -init-config
vim .github/actionlint.yaml
```

---

[Checks](checks.md) | [Installation](install.md) | [Usage](usage.md) | [Go API](api.md) | [References](reference.md)

[Super-Linter]: https://github.com/super-linter/super-linter
[pat]: https://pkg.go.dev/path#Match
[vars]: https://docs.github.com/en/actions/learn-github-actions/variables
[doublestar]: https://github.com/bmatcuk/doublestar
