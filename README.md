# Airlift

`airlift` is a quick program that downloads versions of Roblox place assets to a
Git repository.

## Usage

	airlift [options] [transform [args...]]

- `--id INTEGER` is the ID of the asset to retrieve. This is required.
- `--auth PATH` is the path to a file containing authentication cookies
  (`.ROBLOSECURITY`). The file is formatted as a number of `Set-Cookie` HTTP
  headers. If unspecified, the program will prompt the user to log in.
- `--output PATH` is the directory to which files will be written. Defaults to
  the working directory.
- `filename FORMAT` formats the name of written version files.
- `--git` causes files to be written to a Git repository. Each version is written
  as a commit.
- `--tag` causes each written commit to be tagged with the version number.
- `--pipe` causes a version file to be piped to the transform command (if
  specified) instead of written to the output.
- `--verbose` enables verbose logging.
- `--` terminates flag processing.

Any unprocessed arguments are interpreted as a command with arguments, which can
be used to transform files. This command runs with `--output` as the working
directory, and runs after each version is downloaded. If the command fails, then
that version is skipped. If `--git` is enabled, then the entire working tree is
committed after the command succeeds.

The `--filename` format may contain variables of the form `%VARIABLE` that
expand based on data from the version currently being processed. `%%` emits a
literal `%` character, and unknown variables emit empty strings. Variables are
case-insensitive.

Variable               | Alias | Description
-----------------------|-------|------------
`Id`                   | `vid` | Asset version ID.
`AssetId`              | `aid` | Asset ID.
`VersionNumber`        | `v`   | Current version number.
`ParentAssetVersionId` | `pid` | ID of the parent or previous version.
`CreatorTargetId`      | `cid` | ID of the asset creator.
`CreatorType`          | `ct`  | Number indicating the creator type.
`CreatingUniverseId`   |       | Universe ID, if present.
`Created`              | `t`   | When the version was created.
`Updated`              | `u`   | When the version was last updated.

When `--git` is disabled, the format must produce names that are unique per
version. If not, `_v%VersionNumber` is appended to the filename, before the file
extension. Using any of the `Id`, `VersionNumber`, `Created`, or `Updated`
variables will produce unique names.

## Installation

1. [Install Go](https://golang.org/doc/install)
2. [Install Git](https://git-scm.com/downloads)
3. Using a shell with Git (such as Git Bash), run the following command:

```
go get -u github.com/anaminus/airlift
```

If you configured Go correctly, this will install airlift to `$GOPATH/bin`,
which will allow you run it directly from a shell.

This document uses POSIX-style flags (`-f`, `--flag`), although windows-style
flags (`/f`, `/flag`) are possible when airlift is compiled for Windows. If you
are compiling for Windows, you may choose to force POSIX-style flags with the
`forceposix` build tag:

```
go get -u -tags forceposix github.com/anaminus/airlift
```

For more information, see the [go-flags](https://github.com/jessevdk/go-flags) package.
