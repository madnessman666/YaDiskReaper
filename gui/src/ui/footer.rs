//! Bottom status line with the resolved engine path.

use eframe::egui::{self, RichText};

use crate::app::App;
use crate::theme::ERROR;

impl App {
    pub(crate) fn footer_ui(&mut self, ui: &mut egui::Ui) {
        egui::Panel::bottom("footer").show(ui, |ui| {
            ui.add_space(2.0);
            ui.horizontal(|ui| {
                match &self.engine_path {
                    Some(path) => {
                        ui.label(RichText::new("Engine:").small().weak());
                        ui.label(
                            RichText::new(path.display().to_string())
                                .small()
                                .monospace(),
                        )
                        .on_hover_text(path.display().to_string());
                    }
                    None => {
                        ui.label(
                            RichText::new("Engine binary not found")
                                .small()
                                .color(ERROR),
                        );
                    }
                }

                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    ui.label(RichText::new("v0.1.0").small().weak());
                });
            });
            ui.add_space(2.0);
        });
    }
}
