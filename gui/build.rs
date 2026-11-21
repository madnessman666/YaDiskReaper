//! Copies a pre-built Go engine binary into OUT_DIR so the GUI crate can
//! embed it with `include_bytes!`.
//!
//! Put the engine at `gui/engine-bin/yadiskreaper[.exe]` (CI does this with
//! `go build -o gui/engine-bin/yadiskreaper .`). When the file is missing an
//! empty payload is embedded instead and the GUI falls back to the usual
//! runtime discovery of an external engine.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};

fn main() {
    let manifest = PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR"));
    let dir = manifest.join("engine-bin");

    let target_os = env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();
    let ext = if target_os == "windows" { ".exe" } else { "" };
    let src = dir.join(format!("yadiskreaper{ext}"));

    let bytes = fs::read(&src).unwrap_or_default();
    let out = Path::new(&env::var("OUT_DIR").expect("OUT_DIR")).join("engine.bin");
    fs::write(&out, &bytes).expect("cannot write engine.bin to OUT_DIR");

    // The engine length feeds the build fingerprint: when it changes the
    // crate recompiles, so `include_bytes!` never picks up a stale payload.
    println!("cargo:rustc-env=YADISKREAPER_GUI_ENGINE_LEN={}", bytes.len());
    println!("cargo:rerun-if-changed={}", dir.display());
}
