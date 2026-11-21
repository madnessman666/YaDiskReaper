# YaDisk Reaper GUI

A small, fast native desktop frontend for [YaDiskReaper](../README.md), written
in Rust with [egui](https://github.com/emilk/egui).

The GUI deliberately contains **no download logic of its own**. It starts the
original Go engine as a child process with `--json`, renders the resulting
event stream, and kills the child on Cancel. This keeps aria2c, retries, MD5
checks and file layout exactly the same as in the CLI.

## Features

- Public Yandex Disk link + destination folder picker
- **Scan only**: count files and total size before downloading
- Concurrent downloads (1–16, passed to aria2c)
- Progress in files and bytes, transfer speed, ETA, active/queued counters
- MD5-based skip of already downloaded files
- Cancel button (stops the engine and its aria2c daemon)
- Failed-file list, activity log with copy/clear
- Dark and light theme
- Native, GPU-accelerated UI with a single small binary

## Build and run

Requirements: Go 1.26+, Rust 1.95+ (edition 2024).

```sh
# 1. Build the engine (repository root)
go build -o yadiskreaper .

# 2. Build and run the GUI
cd gui
cargo run --release
```

The ready-to-run GUI binary is `gui/target/release/yadiskreaper-gui`.

### Self-contained build

Build the engine straight into `gui/engine-bin/` and cargo embeds it into
the GUI binary; on first run it is extracted to the OS cache dir and
executed from there. This is what CI ships, so the artifact needs no
separate engine download:

```sh
go build -o gui/engine-bin/yadiskreaper .   # .exe on Windows
cd gui
cargo build --release
```

For a smaller download, the GUI uses the `glow` (OpenGL) backend only; `wgpu`
is not compiled in. Release profile enables LTO and strips symbols. The
`monochrome_emoji_fonts` egui feature is enabled so the emoji used by the theme
switch and in engine logs render consistently on all platforms.

## Finding the engine

The GUI looks for the engine in this order:

1. `YADISKREAPER_BIN` environment variable
2. the path entered under **Advanced → Engine binary**
3. next to the GUI executable (`yadiskreaper`, `yadiskreaper.exe`, …)
4. the crate directory and its parent (works with `cargo run` in a checkout)
5. the current directory and its parent
6. an engine embedded at build time (CI builds always have one), extracted
   to the OS cache dir on first run: `~/.cache/yadiskreaper-gui/` (Linux),
   `~/Library/Caches/yadiskreaper-gui/` (macOS),
   `%LOCALAPPDATA%\yadiskreaper-gui\` (Windows)
7. every `PATH` entry

Only engines built for the current platform are picked up: every candidate
is validated by its binary magic, so a foreign artifact like
`yadiskreaper-linux` sitting next to the GUI on Windows is ignored
instead of failing to start.

If no executable is found, the footer shows a hint and a build command. Note
that on Unix the binary must have the executable bit set; `go build` does that.

The engine is started with the GUI's destination folder as its working
directory, so `failed.log` is written next to the downloaded files.

## Event protocol

The engine writes newline-delimited JSON to stdout; all human-readable logs go
to stderr. Every object has an `event` field:

| Event | Fields | Meaning |
|-------|--------|---------|
| `starting` | — | Process started |
| `info` | `name`, `type` | Root resource metadata |
| `scanning` | `path` | Tree walk started |
| `scan_progress` | `files`, `bytes` | Throttled (2 Hz) scan counters |
| `scan_done` | `total`, `bytes` | Tree walk finished |
| `progress` | `done`, `total`, `skipped`, `failed`, `bytes_done`, `bytes_total`, `speed`, `active`, `waiting`, `activity` | Throttled (2 Hz) download snapshot |
| `file_done` | `path`, `size`, `skipped` | One file finished or skipped by MD5 |
| `file_failed` | `path`, `error` | One file permanently failed |
| `finished` | `ok`, `total`, `skipped`, `failed`, `bytes`, `scan_only` | Final summary |
| `fatal` | `message` | Terminal error |

`--scan-only` stops after `scan_done` (and still emits `finished`). The GUI uses
it for the **Scan only** button.

Example:

```sh
./yadiskreaper --json --scan-only \
  -u https://disk.yandex.ru/d/xxxx \
  -o /tmp/out
```

```json
{"event":"scan_done","total":937,"bytes":580903796}
{"event":"finished","failed":0,"ok":true,"scan_only":true,"skipped":0,"total":937,"bytes":580903796}
```

Unknown events are ignored by the GUI, so new events can be added without
breaking older builds.

## Cancel semantics

Cancel sends `SIGKILL` to the engine. aria2c runs with `--stop-with-process`, so
it exits together with its parent. Partially downloaded files stay on disk and
aria2c resumes them (`-c`) on the next run; MD5 verification handles finished
files.

## Platform notes

- **macOS**: works out of the box (Apple Silicon and Intel).
- **Linux**: needs the usual winit/eframe build dependencies (`pkg-config`,
  X11 or Wayland development headers). Folder dialogs use the XDG portal via
  `rfd`.
- **Windows**: build with the MSVC toolchain. The GUI is a windows-subsystem
  binary in release mode (no console window).

## Project layout

```
gui/
├── Cargo.toml
└── src/
    ├── main.rs           # window setup
    ├── app.rs            # app shell, lifecycle, engine polling
    ├── state.rs          # phases, transfer state, event mapping
    ├── format.rs         # byte/speed/ETA formatting
    ├── theme.rs          # shared colors
    ├── engine/
    │   ├── mod.rs        # public engine API + Mode
    │   ├── events.rs     # JSON event types (serde)
    │   ├── process.rs    # child process and reader threads
    │   └── discovery.rs  # finding the engine binary
    └── ui/
        ├── mod.rs
        ├── header.rs     # title + theme switch
        ├── settings.rs   # form, buttons, advanced settings
        ├── status.rs     # phase, progress, counters, failures
        ├── log.rs        # activity log
        ├── footer.rs     # resolved engine path
        └── widgets.rs    # shared small widgets
```

Run tests with:

```sh
cargo test
```
