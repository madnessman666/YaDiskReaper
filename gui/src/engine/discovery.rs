//! Locating the Go engine binary.

use std::path::{Path, PathBuf};

/// Engine file names to look for, in order, when no explicit path is given.
/// Only names that can actually run on this platform are listed: a
/// `yadiskreaper-linux` artifact sitting next to the GUI on Windows would
/// otherwise be picked up and fail to start with os error 193.
/// `yadiskreaper-macos-arm` matches the artifact built by CI (go.yml).
#[cfg(target_os = "windows")]
const ENGINE_NAMES: [&str; 4] = [
    "yadiskreaper",
    "yadiskreaper.exe",
    "yadiskreaper-win.exe",
    "yadiskreaper-gui-engine.exe",
];

#[cfg(target_os = "macos")]
const ENGINE_NAMES: [&str; 4] = [
    "yadiskreaper",
    "yadiskreaper-macos",
    "yadiskreaper-macos-arm",
    "yadiskreaper-gui-engine",
];

#[cfg(all(unix, not(target_os = "macos")))]
const ENGINE_NAMES: [&str; 3] = [
    "yadiskreaper",
    "yadiskreaper-linux",
    "yadiskreaper-gui-engine",
];

/// Find the Go engine binary. Checked in order:
/// 1. an explicit path (env var or the Advanced field),
/// 2. next to the GUI executable,
/// 3. the crate directory and its parent (works for `cargo run` in a checkout),
/// 4. the current directory and its parent,
/// 5. an engine embedded at build time, extracted to the OS cache dir,
/// 6. every `PATH` entry.
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

    // Prefer an engine embedded into this binary (CI builds always have
    // one) over random PATH entries.
    if let Some(path) = super::embedded::extract() {
        return Some(path);
    }

    if let Some(paths) = std::env::var_os("PATH") {
        for dir in std::env::split_paths(&paths) {
            if let Some(path) = find_in(&dir) {
                return Some(path);
            }
        }
    }

    None
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
        if metadata.permissions().mode() & 0o111 == 0 {
            return false;
        }
    }

    native_binary(path)
}

/// Check that the file starts with the magic of a binary for this OS, so
/// foreign artifacts (e.g. a downloaded `yadiskreaper-linux` next to the
/// GUI on Windows) are rejected instead of failing with os error 193.
fn native_binary(path: &Path) -> bool {
    use std::io::Read;

    let Ok(mut file) = std::fs::File::open(path) else {
        return false;
    };
    let mut magic = [0u8; 4];
    if file.read_exact(&mut magic).is_err() {
        return false;
    }

    // PE image ("MZ") on Windows.
    #[cfg(target_os = "windows")]
    let native = &magic[..2] == b"MZ";
    // Mach-O (thin or fat) on macOS.
    #[cfg(target_os = "macos")]
    let native = matches!(
        magic,
        [0xce, 0xfa, 0xed, 0xfe] | [0xcf, 0xfa, 0xed, 0xfe] | [0xca, 0xfe, 0xba, 0xbe]
    );
    // ELF on other Unix systems.
    #[cfg(all(unix, not(target_os = "macos")))]
    let native = magic == [0x7f, b'E', b'L', b'F'];
    // Unknown platform: fall back to the plain file check.
    #[cfg(not(any(unix, windows)))]
    let native = {
        let _ = magic;
        true
    };
    native
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    #[test]
    fn rejects_foreign_binaries() {
        let dir = std::env::temp_dir().join(format!("ydr-discovery-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();

        // A foreign artifact (e.g. `yadiskreaper-linux` downloaded next to
        // the GUI on Windows) must never be picked up.
        let elf = dir.join("yadiskreaper-linux");
        std::fs::File::create(&elf)
            .unwrap()
            .write_all(&[0x7f, b'E', b'L', b'F'])
            .unwrap();
        make_executable(&elf);
        assert_eq!(is_executable(&elf), cfg!(any(target_os = "linux")));

        // Native magic must pass discovery.
        let native: &[u8] = if cfg!(target_os = "windows") {
            b"MZ\x90\x00"
        } else if cfg!(target_os = "macos") {
            &[0xcf, 0xfa, 0xed, 0xfe]
        } else {
            &[0x7f, b'E', b'L', b'F']
        };
        let native_path = dir.join("native-engine");
        std::fs::File::create(&native_path)
            .unwrap()
            .write_all(native)
            .unwrap();
        make_executable(&native_path);
        assert!(is_executable(&native_path));

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[cfg(unix)]
    fn make_executable(path: &Path) {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755)).unwrap();
    }

    #[cfg(windows)]
    fn make_executable(_path: &Path) {}
}
