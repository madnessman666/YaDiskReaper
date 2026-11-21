//! Title bar with the theme switch.

use eframe::egui::{self, RichText};

use crate::app::App;

impl App {
    pub(crate) fn header_ui(&mut self, ui: &mut egui::Ui) {
        egui::Panel::top("top_bar").show(ui, |ui| {
            ui.add_space(4.0);
            ui.horizontal(|ui| {
                ui.label(RichText::new("YaDisk Reaper").size(22.0).strong());
                ui.add_space(6.0);
                ui.label(
                    RichText::new("Yandex Disk folder downloader")
                        .small()
                        .weak(),
                );

                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    let (icon, label, theme) = if self.dark {
                        ("🌞", "Light", egui::ThemePreference::Light)
                    } else {
                        ("🌙", "Dark", egui::ThemePreference::Dark)
                    };

                    if ui.button(format!("{icon} {label}")).clicked() {
                        self.dark = !self.dark;
                        ui.ctx().set_theme(theme);
                        ui.ctx().request_repaint();
                    }
                });
            });
            ui.add_space(4.0);
        });
    }
}
