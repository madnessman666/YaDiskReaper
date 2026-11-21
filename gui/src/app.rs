//! Application shell: state, lifecycle and event routing.

use std::collections::VecDeque;
use std::path::PathBuf;
use std::time::Duration;

use eframe::egui;

use crate::engine::{self, Mode, Msg, Run};
use crate::state::{Level, LogEntry, MAX_LOG_LINES, Phase, Transfer};

const ENGINE_MISSING: &str = "Engine binary not found. Build it with `go build -o yadiskreaper .` \
     in the repo root, or set its path under Advanced.";

pub struct App {
    pub(crate) url: String,
    pub(crate) out_dir: String,
    pub(crate) parallel: u8,
    pub(crate) engine_override: String,
    pub(crate) engine_path: Option<PathBuf>,
    pub(crate) dark: bool,

    pub(crate) run: Option<Run>,
    pub(crate) transfer: Transfer,
    pub(crate) log: VecDeque<LogEntry>,
}

impl App {
    pub fn new(cc: &eframe::CreationContext<'_>) -> Self {
        cc.egui_ctx.set_theme(egui::ThemePreference::Dark);
        cc.egui_ctx.all_styles_mut(|style| {
            style.spacing.item_spacing = egui::vec2(8.0, 8.0);
            style.spacing.button_padding = egui::vec2(12.0, 6.0);
        });

        let mut app = Self {
            url: String::new(),
            out_dir: default_out_dir().display().to_string(),
            parallel: 8,
            engine_override: String::new(),
            engine_path: engine::resolve_engine(""),
            dark: true,
            run: None,
            transfer: Transfer::default(),
            log: VecDeque::new(),
        };

        app.push(
            Level::Info,
            "Paste a public Yandex Disk link, choose a destination and press Download.",
        );
        match &app.engine_path {
            Some(path) => app.push(Level::Info, format!("Engine: {}", path.display())),
            None => app.push(Level::Error, ENGINE_MISSING),
        }

        app
    }

    pub(crate) fn push(&mut self, level: Level, text: impl Into<String>) {
        if self.log.len() >= MAX_LOG_LINES {
            self.log.pop_front();
        }
        self.log.push_back(LogEntry {
            level,
            text: text.into(),
        });
    }

    /// Put the app into a failed state and show the reason.
    fn fail(&mut self, message: impl Into<String>) {
        let message = message.into();
        self.transfer.phase = Phase::Failed;
        self.transfer.last_error = Some(message.clone());
        self.push(Level::Error, message);
    }

    pub(crate) fn start(&mut self, mode: Mode) {
        if self.run.is_some() {
            return;
        }

        let mut url = self.url.trim().to_string();
        if url.is_empty() {
            self.fail("Enter a public Yandex Disk link first.");
            return;
        }
        if !url.contains("://") {
            url = format!("https://{url}");
            self.url = url.clone();
        }
        if !(url.starts_with("https://") || url.starts_with("http://")) {
            self.fail(
                "The link must start with https:// (for example https://disk.yandex.ru/d/...).",
            );
            return;
        }

        let out_dir = if self.out_dir.trim().is_empty() {
            default_out_dir()
        } else {
            engine::expand_home(self.out_dir.trim())
        };
        let out_dir = absolute(out_dir);
        self.out_dir = out_dir.display().to_string();

        self.transfer.start(mode);

        self.engine_path = engine::resolve_engine(&self.engine_override);
        let Some(engine_path) = self.engine_path.clone() else {
            self.fail(ENGINE_MISSING);
            return;
        };

        match Run::spawn(&engine_path, mode, &url, &out_dir, self.parallel) {
            Ok(run) => {
                self.run = Some(run);
                self.transfer.phase = Phase::Scanning;
                let action = match mode {
                    Mode::Scan => "Scanning (no files will be downloaded)",
                    Mode::Download => "Starting download",
                };
                self.push(Level::Info, format!("{action} → {}", out_dir.display()));
            }
            Err(error) => self.fail(error),
        }
    }

    pub(crate) fn cancel(&mut self) {
        if let Some(run) = self.run.as_mut() {
            run.cancel();
            self.transfer.phase = Phase::Cancelled;
            self.push(Level::Warn, "Cancelled by user.");
        }
    }

    fn poll_engine(&mut self, ctx: &egui::Context) {
        let messages = match self.run.as_mut() {
            Some(run) => run.poll(),
            None => return,
        };

        let mut exit_code = None;
        for message in messages {
            match message {
                Msg::Exited(code) => exit_code = Some(code),
                Msg::Event(event) => {
                    for notice in self.transfer.apply(event) {
                        self.push(notice.level, notice.text);
                    }
                }
                Msg::Log(line) => {
                    let level = Level::from_line(&line);
                    self.push(level, line);
                }
            }
        }

        if let Some(code) = exit_code {
            self.run = None;

            if matches!(self.transfer.phase, Phase::Scanning | Phase::Downloading) {
                self.transfer.phase = if code == Some(0) {
                    Phase::Done
                } else {
                    Phase::Failed
                };
            }

            if code != Some(0) && self.transfer.phase != Phase::Cancelled {
                self.push(
                    Level::Warn,
                    format!("Engine exited with code {}.", code.unwrap_or(-1)),
                );
            }
        }

        if self.run.is_some() {
            ctx.request_repaint_after(Duration::from_millis(100));
        }
    }
}

impl eframe::App for App {
    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        let ctx = ui.ctx().clone();
        self.poll_engine(&ctx);
        self.header_ui(ui);
        self.footer_ui(ui);

        egui::CentralPanel::default().show(ui, |ui| {
            egui::ScrollArea::vertical()
                .auto_shrink([false, false])
                .show(ui, |ui| {
                    self.settings_ui(ui);
                    ui.add_space(14.0);
                    self.status_ui(ui);
                    ui.add_space(14.0);
                    self.activity_log_ui(ui);
                });
        });
    }
}

fn absolute(path: PathBuf) -> PathBuf {
    if path.is_absolute() {
        path
    } else {
        std::env::current_dir()
            .map(|cwd| cwd.join(&path))
            .unwrap_or(path)
    }
}

fn default_out_dir() -> PathBuf {
    match engine::home_dir() {
        Some(home) => {
            let downloads = home.join("Downloads");
            if downloads.is_dir() {
                downloads.join("YaDiskReaper")
            } else {
                home.join("YaDiskReaper")
            }
        }
        None => PathBuf::from("out"),
    }
}
