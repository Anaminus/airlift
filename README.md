# Airlift

`airlift` is a quick program that downloads versions of Roblox place assets.
Versions can optionally be written as a Git repository.

## Usage

	airlift [options] [transform [args...]]

- `-id INT` is the ID of the asset to retrieve. This is required.
- `-auth PATH` is the path to a file containing authentication cookies
  (`.ROBLOSECURITY`). The file is formatted as a number of `Set-Cookie` HTTP
  headers. If unspecified, the program will prompt the user to log in.
- `-output PATH` is the directory to which files will be written. Defaults to
  the working directory.
- `filename FORMAT` formats the name of the place file. Currently, this is
  ignored when `-git` is disabled.
- `-git` causes files to be written to a Git repository. Each version is written
  as a commit.
- `-tag` causes each written commit to be tagged with the version number.
- `-pipe` causes a version file to be piped to the transform command (if
  specified) instead of written to the output.
- `-v` enables verbose logging.
- `--` terminates flag processing.

Any unprocessed arguments are interpreted as a command with arguments, which can
be used to transform files. This command runs with `-output` as the working
directory, and runs after each version is downloaded. If the command fails, then
that version is skipped. If `-git` is enabled, then the entire working tree is
committed after the command succeeds.

## Installation

1. [Install Go](https://golang.org/doc/install)
2. [Install Git](https://git-scm.com/downloads)
3. Using a shell with Git (such as Git Bash), run the following command:

```
go get -u github.com/anaminus/airlift
```

If you configured Go correctly, this will install airlift to `$GOPATH/bin`,
which will allow you run it directly from a shell.
