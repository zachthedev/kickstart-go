# Usage

<!-- TODO(kickstart): write what `--help` and the README do not carry: settings and their files, data
locations, exit codes, and any URL or file format the binary promises. This file is the reference for the
binary; the README says what it is and how to get it. -->

The placeholder binary has one command, `demo`, which runs one code path per internal package and prints
what it found. `--help` lists its flags.

## Files

The `demo` command reads `KICKSTART_CONFIG` and the XDG variables (`XDG_CONFIG_HOME`, `XDG_CACHE_HOME`,
`XDG_STATE_HOME`) to resolve the paths it prints, runs `git remote get-url origin` in the working directory
when no owner was stamped at build time, and writes a temporary log directory it removes before it exits.
It prints the paths the binary would use, resolved by `internal/paths` for the host's conventions: a
config file under the user's configuration directory, a cache directory, a state directory and a log
file. It creates none of them.

## Exit status

`0` when every demonstrated package ran, `1` with the failing package named on stderr.
