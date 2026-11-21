//! Small reusable building blocks.

use eframe::egui::{self, RichText};

pub(crate) fn section_label(ui: &mut egui::Ui, text: &str) {
    ui.label(RichText::new(text).small().weak());
}

pub(crate) fn hint(ui: &mut egui::Ui, text: &str) {
    ui.label(RichText::new(text).small().weak());
}

pub(crate) fn stat(ui: &mut egui::Ui, label: &str, value: impl Into<String>) {
    ui.label(RichText::new(label).small().weak());
    ui.label(RichText::new(value.into()).strong());
}
