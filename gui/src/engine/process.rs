//! Spawning the engine and reading its event/log streams.

use std::io::{BufRead, BufReader};
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{self, Receiver};

use super::{Event, Mode};

/// Messages delivered to the UI thread.
pub enum Msg {
    Event(Event),
    /// A line from the engine's stderr (or a non-JSON stdout line).
    Log(String),
    /// The engine process has exited.
    Exited(Option<i32>),
}

/// A running engine process plus the reader threads that feed the UI.
pub struct Run {
    child: Child,
    rx: Receiver<Msg>,
    exit_reported: bool,
}

impl Run {
    pub fn spawn(
        engine: &Path,
        mode: Mode,
        url: &str,
        out_dir: &Path,
        parallel: u8,
    ) -> Result<Self, String> {
        std::fs::create_dir_all(out_dir)
            .map_err(|e| format!("cannot create {}: {e}", out_dir.display()))?;

        let mut command = Command::new(engine);
        command
            .arg("-u")
            .arg(url)
            .arg("-o")
            .arg(out_dir)
            .arg("-p")
            .arg(parallel.to_string())
            .arg("--json")
            .current_dir(out_dir)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        if mode == Mode::Scan {
            command.arg("--scan-only");
        }

        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            const CREATE_NO_WINDOW: u32 = 0x0800_0000;
            command.creation_flags(CREATE_NO_WINDOW);
        }

        let mut child = command
            .spawn()
            .map_err(|e| format!("failed to start {}: {e}", engine.display()))?;

        let stdout = child
            .stdout
            .take()
            .ok_or_else(|| "engine stdout is not available".to_string())?;
        let stderr = child
            .stderr
            .take()
            .ok_or_else(|| "engine stderr is not available".to_string())?;

        let (tx, rx) = mpsc::channel();

        // Event stream (stdout).
        let tx_out = tx.clone();
        std::thread::spawn(move || {
            for line in BufReader::new(stdout).lines().map_while(Result::ok) {
                if line.trim().is_empty() {
                    continue;
                }
                match serde_json::from_str::<Event>(&line) {
                    Ok(event) => {
                        if tx_out.send(Msg::Event(event)).is_err() {
                            return;
                        }
                    }
                    // Never lose information: show non-JSON output as a log line.
                    Err(_) => {
                        if tx_out.send(Msg::Log(line)).is_err() {
                            return;
                        }
                    }
                }
            }
        });

        // Human-readable logs (stderr).
        std::thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                if !line.trim().is_empty() && tx.send(Msg::Log(line)).is_err() {
                    return;
                }
            }
        });

        Ok(Self {
            child,
            rx,
            exit_reported: false,
        })
    }

    /// Drain pending messages and report process exit exactly once.
    pub fn poll(&mut self) -> Vec<Msg> {
        let mut messages = Vec::new();
        while let Ok(msg) = self.rx.try_recv() {
            messages.push(msg);
        }

        if !self.exit_reported
            && let Ok(Some(status)) = self.child.try_wait()
        {
            self.exit_reported = true;
            messages.push(Msg::Exited(status.code()));
        }

        messages
    }

    /// Kill the engine. aria2c is configured with `--stop-with-process`, so it
    /// shuts down together with the engine process.
    pub fn cancel(&mut self) {
        let _ = self.child.kill();
    }
}

impl Drop for Run {
    fn drop(&mut self) {
        if !self.exit_reported {
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
    }
}
