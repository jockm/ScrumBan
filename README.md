# ScrumBan

A local ScrumBan board that uses the filesystem for storage.
Designed for use within version-controlled project directories.

---

## Requirements

- Go 1.25 or later

To check your Go version:

```bash
go version
```

If Go is not installed, download it from https://go.dev/dl/ and follow the
installer instructions for your platform.

---

## Getting the source

If you have the source as a directory, navigate into it:

```bash
cd scrumban
```

---

## Fetching dependencies

ScrumBan uses one external dependency (`gopkg.in/yaml.v3`). Run the following
to download it and fix the `go.sum` file:

```bash
go mod tidy
```

This requires internet access on the first run. After that, the module cache
is local and no network access is needed.

---

## Building

### Linux / macOS

```bash
go build -o scrumban .
```

This produces an executable named `scrumban` in the current directory.

### Windows

```bash
go build -o scrumban.exe .
```

This produces `scrumban.exe` in the current directory.

### Cross-compiling

You can build for a different platform from any machine by setting `GOOS` and
`GOARCH`:

```bash
# Build for Linux (64-bit) from macOS or Windows
GOOS=linux GOARCH=amd64 go build -o scrumban .

# Build for macOS (Apple Silicon) from Linux or Windows
GOOS=darwin GOARCH=arm64 go build -o scrumban .

# Build for Windows (64-bit) from Linux or macOS
GOOS=windows GOARCH=amd64 go build -o scrumban.exe .
```

---

## Running

```bash
./scrumban <data-directory>
./scrumban --port 9090 <data-directory>
./scrumban --help
```

On Windows:

```bash
scrumban.exe <data-directory>
scrumban.exe --port 9090 <data-directory>
scrumban.exe --help
```

`<data-directory>` is the path to a folder containing your board column
folders (see Data format below). The directory must already exist.

If `--port` is omitted, a free port is chosen automatically and printed to
stdout. The app opens your default browser automatically on launch.

---

## Usage

```
scrumban [--port PORT] [--help] <data-directory>

Flags:
  --port PORT   TCP port to listen on (default: auto)
  --help        Show help and exit
```

---

## Data format

### Columns

Each column is a subdirectory of `<data-directory>` named with a two-digit
number prefix and a dot, followed by the column name:

```
01.Backlog
02.In Progress
03.In Review
04.Done
```

Columns are displayed left to right in numerical order. Numbers must be unique.
Any directory that does not match the `##.Name` pattern is ignored.

### Stories

Each story is a `.md` file inside a column folder. The filename minus the
`.md` extension is the story ID.

Files must begin with YAML front matter:

```markdown
---
assignee: jock
status: In Progress
priority: High
---

# Fix Database Connection Timeout
The application currently drops connections after 30 seconds of inactivity.

# Commentary
* **2026-02-26:** Testing a local fix now.
```

- `assignee`, `status`, and `priority` are free-form text. If any are missing
  they default to `UNKNOWN`.
- The first `#` heading is the story summary.
- Text between the first and second `#` headings is the description.
- The second `#` heading begins the comment section. Its text can be anything.
- Text after the second `#` heading is the comment section body (free-form).

### Priority colour coding

The priority field is colour coded in the UI when it matches one of these
values (case-insensitive):

| Value      | Colour |
|------------|--------|
| `Critical` | Red    |
| `High`     | Orange |
| `Medium`   | Blue   |
| `Low`      | Green  |

Any other value is displayed without colour coding.

---

## Version control workflow

ScrumBan is designed to work inside a git (or other VCS) repository. Each
team member runs their own local instance against their own working copy of
the board directory. Drag-and-drop moves are file system renames, so they
appear as normal file moves in version control diffs.

If two users move the same story to different columns and then sync, this
will produce a VCS conflict (a missing file in one location) which must be
resolved manually outside the app.
