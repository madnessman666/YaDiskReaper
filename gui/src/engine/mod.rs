//! Thin process wrapper around the YaDiskReaper Go engine.
//!
//! The engine is started with `--json`, which makes it write newline-delimited
//! JSON events to stdout and human-readable logs to stderr. This keeps all the
//! download logic (aria2c, retries, MD5 checks) in Go while the GUI stays a
//! small, fast frontend.

mod discovery;
mod embedded;
mod events;
mod process;

pub use discovery::{expand_home, home_dir, resolve_engine};
pub use events::Event;
pub use process::{Msg, Run};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    /// Walk the folder tree and report totals without downloading anything.
    Scan,
    /// Scan and download everything.
    Download,
}
