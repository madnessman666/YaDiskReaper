//! Engine binary embedded into the GUI at build time.
//!
//! `build.rs` copies `engine-bin/yadiskreaper[.exe]` into OUT_DIR before the
//! crate compiles, and `include_bytes!` bakes the bytes into the GUI binary
//! (CI builds always provide it). Without that file an empty payload is
//! embedded and the usual runtime discovery is used instead.
//!
//! On first use the payload is extracted to the OS cache dir and executed
//! from there — the same trick the engine itself uses for its aria2c.

use std::fs;
use std::path::{Path, PathBuf};

/// Embedded engine bytes; empty when the GUI was built without an engine.
static ENGINE_BIN: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/engine.bin"));

/// Extract the embedded engine to the OS cache dir and return its path.
///
/// Returns `None` when the GUI was built without an embedded engine or the
/// cache dir is not writable.
pub(super) fn extract() -> Option<PathBuf> {
    if ENGINE_BIN.is_empty() {
        return None;
    }

    let dir = cache_dir()?.join("yadiskreaper-gui");
    fs::create_dir_all(&dir).ok()?;
    let path = dir.join("engine");

    // Same build already extracted: skip the write.
    if let Ok(meta) = fs::metadata(&path)
        && meta.len() == ENGINE_BIN.len() as u64
        && is_executable(&meta)
    {
        return Some(path);
    }

    // Write under a temp name first so a crash never leaves a half-written
    // engine behind, then move it into place.
    let tmp = path.with_file_name(format!("engine.tmp.{}", std::process::id()));
    if fs::write(&tmp, ENGINE_BIN).is_err() {
        let _ = fs::remove_file(&tmp);
        return None;
    }
    if !set_executable(&tmp) {
        let _ = fs::remove_file(&tmp);
        return None;
    }
    if fs::rename(&tmp, &path).is_err() {
        let _ = fs::remove_file(&tmp);
        return None;
    }

    Some(path)
}

fn cache_dir() -> Option<PathBuf> {
    #[cfg(target_os = "macos")]
    let base = std::env::var_os("HOME").map(|home| PathBuf::from(home).join("Library/Caches"));
    #[cfg(target_os = "windows")]
    let base = std::env::var_os("LOCALAPPDATA").map(PathBuf::from);
    #[cfg(all(unix, not(target_os = "macos")))]
    let base = match std::env::var_os("XDG_CACHE_HOME") {
        Some(dir) if !dir.is_empty() => Some(PathBuf::from(dir)),
        _ => std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".cache")),
    };
    base
}

#[cfg(unix)]
fn is_executable(meta: &fs::Metadata) -> bool {
    use std::os::unix::fs::PermissionsExt;
    meta.permissions().mode() & 0o111 != 0
}

#[cfg(windows)]
fn is_executable(_meta: &fs::Metadata) -> bool {
    true
}

#[cfg(unix)]
fn set_executable(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(path, fs::Permissions::from_mode(0o755)).is_ok()
}

#[cfg(windows)]
fn set_executable(_path: &Path) -> bool {
    true
}

#[cfg(all(test, target_os = "macos"))]
mod tests {
    use super::*;

    #[test]
    fn extracts_embedded_engine_to_cache_dir() {
        if ENGINE_BIN.is_empty() {
            return; // built without an embedded engine (e.g. the CI test job)
        }

        let home = std::env::temp_dir().join(format!("ydr-gui-test-{}", std::process::id()));
        std::fs::create_dir_all(&home).unwrap();
        let original = std::env::var_os("HOME");
        // cache_dir() reads HOME at call time; set_var is unsafe in edition 2024.
        unsafe { std::env::set_var("HOME", &home) };

        let extracted = extract();

        match original {
            Some(home) => unsafe { std::env::set_var("HOME", home) },
            None => unsafe { std::env::remove_var("HOME") },
        }

        let path = extracted.expect("engine should be extracted");
        let meta = fs::metadata(&path).unwrap();
        assert_eq!(meta.len(), ENGINE_BIN.len() as u64);
        assert!(is_executable(&meta));

        let _ = std::fs::remove_dir_all(&home);
    }
}
