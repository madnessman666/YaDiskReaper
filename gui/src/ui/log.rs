//! Scrollable activity log with copy/clear buttons.

use eframe::egui::{self, RichText};

use crate::app::App;

impl App {
    pub(crate) fn activity_log_ui(&mut self, ui: &mut egui::Ui) {
        ui.horizontal(|ui| {
            ui.label(RichText::new("Activity").strong());
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if ui.small_button("Clear").clicked() {
                    self.log.clear();
                }
                if ui.small_button("Copy").clicked() {
                    let text = self
                        .log
                        .iter()
                        .map(|entry| entry.text.as_str())
                        .collect::<Vec<_>>()
                        .join("\n");
                    ui.ctx().copy_text(text);
                }
            });
        });

        egui::Frame::group(ui.style()).show(ui, |ui| {
            ui.set_width(ui.available_width());
            egui::ScrollArea::vertical()
                .max_height(190.0)
                .stick_to_bottom(true)
                .show(ui, |ui| {
                    for entry in &self.log {
                        ui.label(
                            RichText::new(&entry.text)
                                .monospace()
                                .size(12.0)
                                .color(entry.level.color(ui)),
                        );
                    }
                });
        });
    }
}
