//! Locating the Go engine binary.

use std::path::{Path, PathBuf};

/// Binaries we look for, in order, when no explicit path is given.
/// `yadiskreaper-macos-arm` matches the artifact built by CI (go.yml).
const ENGINE_NAMES: [&str; 8] = [
    "yadiskreaper",
    "yadiskreaper.exe",
    "yadiskreaper-macos",
    "yadiskreaper-macos-arm",
    "yadiskreaper-linux",
    "yadiskreaper-win.exe",
    "yadiskreaper-gui-engine",
    "yadiskreaper-gui-engine.exe",
];

/// Find the Go engine binary. Checked in order:
/// 1. an explicit path (env var or the Advanced field),
/// 2. next to the GUI executable,
/// 3. the crate directory and its parent (works for `cargo run` in a checkout),
/// 4. the current directory and its parent,
/// 5. every `PATH` entry,
/// 6. an engine embedded at build time, extracted to the OS cache dir.
pub fn resolve_engine(explicit: &str) -> Option<PathBuf> {
    let explicit = explicit.trim();
    if !explicit.is_empty() {
        let path = expand_home(explicit);
        return is_executable(&path).then_some(path);
    }

    if let Some(env_path) = std::env::var_os("YADISKREAPER_BIN") {
        let path = PathBuf::from(env_path);
        if is_executable(&path) {
            return Some(path);
        }
    }

    let mut dirs = Vec::new();
    if let Ok(exe) = std::env::current_exe()
        && let Some(dir) = exe.parent()
    {
        dirs.push(dir.to_path_buf());
    }

    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    dirs.push(manifest.clone());
    if let Some(parent) = manifest.parent() {
        dirs.push(parent.to_path_buf());
    }

    if let Ok(cwd) = std::env::current_dir() {
        dirs.push(cwd.clone());
        if let Some(parent) = cwd.parent() {
            dirs.push(parent.to_path_buf());
        }
    }

    for dir in &dirs {
        if let Some(path) = find_in(dir) {
            return Some(path);
        }
    }

    if let Some(paths) = std::env::var_os("PATH") {
        for dir in std::env::split_paths(&paths) {
            if let Some(path) = find_in(&dir) {
                return Some(path);
            }
        }
    }

    // Last resort: an engine embedded into this binary at build time
    // (CI builds always have one).
    super::embedded::extract()
}

fn find_in(dir: &Path) -> Option<PathBuf> {
    ENGINE_NAMES
        .iter()
        .map(|name| dir.join(name))
        .find(|path| is_executable(path))
}

fn is_executable(path: &Path) -> bool {
    let Ok(metadata) = std::fs::metadata(path) else {
        return false;
    };
    if !metadata.is_file() {
        return false;
    }

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        metadata.permissions().mode() & 0o111 != 0
    }
    #[cfg(windows)]
    {
        true
    }
}

/// Expand a leading `~/` (used for the destination and engine path fields).
pub fn expand_home(input: &str) -> PathBuf {
    if let Some(rest) = input.strip_prefix("~/")
        && let Some(home) = home_dir()
    {
        return home.join(rest);
    }
    PathBuf::from(input)
}

pub fn home_dir() -> Option<PathBuf> {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
}
