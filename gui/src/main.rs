#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod app;
mod engine;
mod format;
mod state;
mod theme;
mod ui;

use eframe::egui;

fn main() -> eframe::Result<()> {
    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_title("YaDisk Reaper")
            .with_inner_size([780.0, 640.0])
            .with_min_inner_size([600.0, 540.0]),
        ..Default::default()
    };

    eframe::run_native(
        "YaDisk Reaper",
        options,
        Box::new(|cc| Ok(Box::new(app::App::new(cc)))),
    )
}
