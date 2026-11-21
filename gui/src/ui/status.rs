//! Status card: phase, progress bars, live counters and failures.

use eframe::egui::{self, RichText};

use crate::app::App;
use crate::engine::Mode;
use crate::format;
use crate::state::Phase;
use crate::theme::{ACCENT, ERROR, SUCCESS, WARNING};

use super::widgets::{hint, stat};

impl App {
    pub(crate) fn status_ui(&mut self, ui: &mut egui::Ui) {
        egui::Frame::group(ui.style()).show(ui, |ui| {
            ui.set_width(ui.available_width());

            ui.horizontal(|ui| {
                let phase = self.transfer.phase;
                ui.label(
                    RichText::new(phase.label())
                        .small()
                        .strong()
                        .color(phase.color(ui)),
                );
                if !self.transfer.folder_name.is_empty() {
                    ui.label(RichText::new(&self.transfer.folder_name).strong());
                }
            });

            ui.add_space(6.0);
            self.phase_body(ui);
            self.failures_ui(ui);
        });
    }

    fn phase_body(&self, ui: &mut egui::Ui) {
        match self.transfer.phase {
            Phase::Idle => hint(
                ui,
                "Ready. Paste a public link and press Download, or use Scan only to preview the folder.",
            ),
            Phase::Scanning => {
                ui.horizontal(|ui| {
                    ui.add(egui::Spinner::new());
                    ui.label(format!(
                        "Scanning… {} files, {} found so far",
                        self.transfer.scan_files,
                        format::bytes(self.transfer.scan_bytes)
                    ));
                });
            }
            Phase::Downloading => self.progress_ui(ui),
            Phase::Done => {
                ui.label(
                    RichText::new(self.transfer.summary.as_deref().unwrap_or("Done."))
                        .color(SUCCESS)
                        .strong(),
                );
                if self.transfer.mode == Some(Mode::Download) {
                    ui.add_space(4.0);
                    self.progress_ui(ui);
                }
            }
            Phase::Failed => {
                let text = self
                    .transfer
                    .last_error
                    .as_deref()
                    .unwrap_or("Finished with errors.");
                ui.label(RichText::new(text).color(ERROR).strong());
                if self.transfer.total_files > 0 && self.transfer.failed > 0 {
                    hint(
                        ui,
                        &format!(
                            "{} of {} files failed.",
                            self.transfer.failed, self.transfer.total_files
                        ),
                    );
                }
            }
            Phase::Cancelled => {
                ui.label(RichText::new("Cancelled.").color(WARNING).strong());
            }
        }
    }

    fn progress_ui(&self, ui: &mut egui::Ui) {
        ui.add(
            egui::ProgressBar::new(self.transfer.file_fraction())
                .text(format!(
                    "{} / {} files ({:.1}%)",
                    self.transfer.done_files,
                    self.transfer.total_files,
                    self.transfer.file_fraction() * 100.0
                ))
                .fill(ACCENT),
        );

        let mut byte_text = format!(
            "{} / {}",
            format::bytes(self.transfer.done_bytes),
            format::bytes(self.transfer.total_bytes)
        );
        if let Some(eta) = format::eta(
            self.transfer.done_bytes,
            self.transfer.total_bytes,
            self.transfer.speed,
        ) {
            byte_text.push_str(&format!(" — ETA {eta}"));
        }
        ui.add(
            egui::ProgressBar::new(self.transfer.byte_fraction())
                .text(byte_text)
                .fill(SUCCESS),
        );

        ui.add_space(4.0);
        ui.horizontal_wrapped(|ui| {
            stat(ui, "Speed", format::speed(self.transfer.speed));
            ui.separator();
            stat(ui, "Active", self.transfer.active.to_string());
            ui.separator();
            stat(ui, "Queued", self.transfer.waiting.to_string());
            ui.separator();
            stat(ui, "Skipped", self.transfer.skipped.to_string());
            ui.separator();
            stat(ui, "Failed", self.transfer.failed.to_string());
        });

        if !self.transfer.activity.is_empty() {
            ui.add_space(2.0);
            hint(ui, &format!("Current: {}", self.transfer.activity));
        }
    }

    fn failures_ui(&self, ui: &mut egui::Ui) {
        if self.transfer.failures.is_empty() {
            return;
        }

        ui.add_space(6.0);
        egui::CollapsingHeader::new(
            RichText::new(format!("Failed files ({})", self.transfer.failures.len()))
                .small()
                .color(ERROR),
        )
        .default_open(false)
        .show(ui, |ui| {
            for failure in &self.transfer.failures {
                ui.label(
                    RichText::new(format!("{} — {}", failure.path, failure.error))
                        .small()
                        .monospace(),
                );
            }
        });
    }
}
