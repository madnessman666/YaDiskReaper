//! Link/destination form, action buttons and advanced settings.

use std::path::Path;

use eframe::egui::{self, Color32, RichText};

use crate::app::App;
use crate::engine::{self, Mode};
use crate::theme::ACCENT;

use super::widgets::{hint, section_label};

impl App {
    pub(crate) fn settings_ui(&mut self, ui: &mut egui::Ui) {
        section_label(ui, "PUBLIC LINK");
        ui.add(
            egui::TextEdit::singleline(&mut self.url)
                .hint_text("https://disk.yandex.ru/d/…")
                .desired_width(f32::INFINITY),
        );

        ui.add_space(6.0);
        section_label(ui, "DESTINATION");
        ui.horizontal(|ui| {
            let text_width = (ui.available_width() - 96.0).max(160.0);
            ui.add_sized(
                [text_width, 26.0],
                egui::TextEdit::singleline(&mut self.out_dir),
            );
            if ui.button("Browse…").clicked() {
                let current = engine::expand_home(self.out_dir.trim());
                let mut dialog = rfd::FileDialog::new().set_title("Choose destination folder");
                if current.is_dir() {
                    dialog = dialog.set_directory(&current);
                }
                if let Some(dir) = dialog.pick_folder() {
                    self.out_dir = dir.display().to_string();
                }
            }
        });

        ui.add_space(6.0);
        ui.horizontal(|ui| {
            section_label(ui, "PARALLEL DOWNLOADS");
            ui.add_space(4.0);
            ui.add(egui::Slider::new(&mut self.parallel, 1..=16));
            ui.add_space(4.0);
            hint(ui, "(1–16, higher is faster but may trigger rate limits)");
        });

        ui.add_space(10.0);
        self.buttons_ui(ui);

        ui.add_space(2.0);
        egui::CollapsingHeader::new(RichText::new("Advanced").small())
            .default_open(false)
            .show(ui, |ui| {
                section_label(ui, "ENGINE BINARY");
                ui.horizontal(|ui| {
                    let hint_text = match &self.engine_path {
                        Some(path) => path.display().to_string(),
                        None => "path to the yadiskreaper binary".to_string(),
                    };
                    let text_width = (ui.available_width() - 96.0).max(160.0);
                    ui.add_sized(
                        [text_width, 26.0],
                        egui::TextEdit::singleline(&mut self.engine_override).hint_text(hint_text),
                    );
                    if ui.button("Locate…").clicked()
                        && let Some(file) = rfd::FileDialog::new()
                            .set_title("Select the YaDiskReaper engine binary")
                            .pick_file()
                    {
                        self.engine_override = file.display().to_string();
                        self.engine_path = engine::resolve_engine(&self.engine_override);
                    }
                });
                hint(
                    ui,
                    "Leave empty to auto-detect. Build the engine with \
                     `go build -o yadiskreaper .` in the repository root.",
                );
            });
    }

    pub(crate) fn buttons_ui(&mut self, ui: &mut egui::Ui) {
        let idle = self.run.is_none();
        let out_dir = engine::expand_home(self.out_dir.trim());
        let can_open = idle && out_dir.is_dir();

        ui.horizontal(|ui| {
            let download =
                egui::Button::new(RichText::new("Download").strong().color(Color32::WHITE))
                    .fill(ACCENT);
            if ui.add_enabled(idle, download).clicked() {
                self.start(Mode::Download);
            }

            if ui
                .add_enabled(idle, egui::Button::new("Scan only"))
                .on_hover_text("List files and total size without downloading")
                .clicked()
            {
                self.start(Mode::Scan);
            }

            if ui.add_enabled(!idle, egui::Button::new("Cancel")).clicked() {
                self.cancel();
            }

            if ui
                .add_enabled(can_open, egui::Button::new("Open folder"))
                .clicked()
            {
                open_path(&out_dir);
            }
        });
    }
}

fn open_path(path: &Path) {
    #[cfg(target_os = "macos")]
    let result = std::process::Command::new("open").arg(path).spawn();
    #[cfg(target_os = "windows")]
    let result = std::process::Command::new("explorer").arg(path).spawn();
    #[cfg(all(unix, not(target_os = "macos")))]
    let result = std::process::Command::new("xdg-open").arg(path).spawn();

    let _ = result;
}
