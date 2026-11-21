//! Application state and the mapping from engine events to UI state.

use eframe::egui::{self, Color32};

use crate::engine::{Event, Mode};
use crate::format;
use crate::theme::{ACCENT, ERROR, SUCCESS, WARNING};

pub const MAX_LOG_LINES: usize = 300;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Phase {
    #[default]
    Idle,
    Scanning,
    Downloading,
    Done,
    Failed,
    Cancelled,
}

impl Phase {
    pub fn label(self) -> &'static str {
        match self {
            Phase::Idle => "IDLE",
            Phase::Scanning => "SCANNING",
            Phase::Downloading => "DOWNLOADING",
            Phase::Done => "DONE",
            Phase::Failed => "FAILED",
            Phase::Cancelled => "CANCELLED",
        }
    }

    pub fn color(self, ui: &egui::Ui) -> Color32 {
        match self {
            Phase::Idle => ui.visuals().weak_text_color(),
            Phase::Scanning | Phase::Downloading => ACCENT,
            Phase::Done => SUCCESS,
            Phase::Failed => ERROR,
            Phase::Cancelled => WARNING,
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub enum Level {
    Info,
    Success,
    Warn,
    Error,
}

impl Level {
    pub fn from_line(line: &str) -> Self {
        let lower = line.to_ascii_lowercase();
        if line.contains('❌') || lower.contains("error") || lower.contains("fatal") {
            Level::Error
        } else if line.contains('⚠') || lower.contains("warning") {
            Level::Warn
        } else if line.contains('✅') {
            Level::Success
        } else {
            Level::Info
        }
    }

    pub fn color(self, ui: &egui::Ui) -> Color32 {
        match self {
            Level::Info => ui.visuals().text_color(),
            Level::Success => SUCCESS,
            Level::Warn => WARNING,
            Level::Error => ERROR,
        }
    }
}

pub struct Notice {
    pub level: Level,
    pub text: String,
}

impl Notice {
    fn new(level: Level, text: impl Into<String>) -> Self {
        Self {
            level,
            text: text.into(),
        }
    }

    pub fn info(text: impl Into<String>) -> Self {
        Self::new(Level::Info, text)
    }

    pub fn success(text: impl Into<String>) -> Self {
        Self::new(Level::Success, text)
    }

    pub fn error(text: impl Into<String>) -> Self {
        Self::new(Level::Error, text)
    }
}

pub struct LogEntry {
    pub level: Level,
    pub text: String,
}

pub struct Failure {
    pub path: String,
    pub error: String,
}

/// Everything that describes the current (or last) run.
#[derive(Default)]
pub struct Transfer {
    pub mode: Option<Mode>,
    pub phase: Phase,
    pub folder_name: String,
    pub scan_files: u64,
    pub scan_bytes: u64,
    pub total_files: u64,
    pub done_files: u64,
    pub skipped: u64,
    pub failed: u64,
    pub total_bytes: u64,
    pub done_bytes: u64,
    pub speed: f64,
    pub active: u64,
    pub waiting: u64,
    pub activity: String,
    pub failures: Vec<Failure>,
    pub last_error: Option<String>,
    pub summary: Option<String>,
}

impl Transfer {
    /// Reset all counters and remember which mode the new run uses.
    pub fn start(&mut self, mode: Mode) {
        *self = Self {
            mode: Some(mode),
            ..Self::default()
        };
    }

    /// Apply one engine event and return the log lines it should produce.
    pub fn apply(&mut self, event: Event) -> Vec<Notice> {
        let mut notices = Vec::new();

        match event {
            Event::Info { name, kind } => {
                if !name.is_empty() {
                    self.folder_name = name.clone();
                    notices.push(Notice::info(format!("Folder: {name} ({kind})")));
                }
            }
            Event::Scanning => {
                self.phase = Phase::Scanning;
                self.scan_files = 0;
                self.scan_bytes = 0;
                notices.push(Notice::info("Scanning folder tree…"));
            }
            Event::ScanProgress { files, bytes } => {
                self.scan_files = files;
                self.scan_bytes = bytes;
            }
            Event::ScanDone { total, bytes } => {
                self.total_files = total;
                self.total_bytes = bytes;
                self.scan_files = total;
                self.scan_bytes = bytes;
                notices.push(Notice::info(format!(
                    "Found {total} files — {}.",
                    format::bytes(bytes)
                )));
                if self.mode == Some(Mode::Download) && self.phase == Phase::Scanning {
                    self.phase = Phase::Downloading;
                }
            }
            Event::Progress {
                done,
                total,
                skipped,
                failed,
                bytes_done,
                bytes_total,
                speed,
                active,
                waiting,
                activity,
            } => {
                // `done` may briefly lag behind file_done events, so never go back.
                self.done_files = self.done_files.max(done);
                if total > 0 {
                    self.total_files = total;
                }
                self.skipped = skipped;
                self.failed = failed;
                self.done_bytes = self.done_bytes.max(bytes_done);
                if bytes_total > 0 {
                    self.total_bytes = bytes_total;
                }
                self.speed = speed;
                self.active = active;
                self.waiting = waiting;
                self.activity = activity;

                if self.mode == Some(Mode::Download) && self.phase == Phase::Scanning {
                    self.phase = Phase::Downloading;
                }
            }
            Event::FileDone {
                path,
                size,
                skipped,
            } => {
                self.done_files = self.done_files.saturating_add(1);
                if self.total_files > 0 {
                    self.done_files = self.done_files.min(self.total_files);
                }
                let name = format::file_name(&path);
                notices.push(if skipped {
                    Notice::success(format!("Skipped (MD5 match): {name}"))
                } else {
                    Notice::success(format!("Downloaded: {name} ({})", format::bytes(size)))
                });
            }
            Event::FileFailed { path, error } => {
                let name = format::file_name(&path).to_string();
                let error = if error.is_empty() {
                    "download failed".to_string()
                } else {
                    error
                };
                self.failures.push(Failure {
                    path,
                    error: error.clone(),
                });
                notices.push(Notice::error(format!("Failed: {name} — {error}")));
            }
            Event::Finished {
                ok,
                total,
                skipped,
                failed,
                bytes,
                scan_only,
            } => {
                self.phase = if ok { Phase::Done } else { Phase::Failed };
                if ok {
                    self.last_error = None;
                }
                self.total_files = total;
                self.skipped = skipped;
                self.failed = failed;
                if bytes > 0 {
                    self.done_bytes = bytes;
                }

                let summary = if scan_only {
                    format!(
                        "Scan complete — {} files, {}.",
                        total,
                        format::bytes(self.total_bytes)
                    )
                } else if ok {
                    format!("Download complete — {total} files, {skipped} skipped.")
                } else {
                    format!("Finished with errors — {failed} failed out of {total} files.")
                };
                notices.push(if ok {
                    Notice::success(summary.clone())
                } else {
                    Notice::error(summary.clone())
                });
                self.summary = Some(summary);

                // The engine is shutting down.
                self.speed = 0.0;
                self.active = 0;
                self.waiting = 0;
            }
            Event::Fatal { message } => {
                self.phase = Phase::Failed;
                self.last_error = Some(message.clone());
                notices.push(Notice::error(message));
            }
            Event::Unknown => {}
        }

        notices
    }

    pub fn file_fraction(&self) -> f32 {
        if self.total_files == 0 {
            0.0
        } else {
            (self.done_files as f32 / self.total_files as f32).clamp(0.0, 1.0)
        }
    }

    pub fn byte_fraction(&self) -> f32 {
        if self.total_bytes == 0 {
            0.0
        } else {
            (self.done_bytes as f32 / self.total_bytes as f32).clamp(0.0, 1.0)
        }
    }
}
