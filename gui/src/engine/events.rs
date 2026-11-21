//! Events emitted by the Go engine (`--json`).

use serde::Deserialize;

/// One event from the engine. Unknown events are ignored so the GUI keeps
/// working if the engine grows new events later.
#[derive(Debug, Clone, Deserialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum Event {
    Info {
        #[serde(default)]
        name: String,
        #[serde(default, rename = "type")]
        kind: String,
    },
    Scanning,
    ScanProgress {
        #[serde(default)]
        files: u64,
        #[serde(default)]
        bytes: u64,
    },
    ScanDone {
        #[serde(default)]
        total: u64,
        #[serde(default)]
        bytes: u64,
    },
    Progress {
        #[serde(default)]
        done: u64,
        #[serde(default)]
        total: u64,
        #[serde(default)]
        skipped: u64,
        #[serde(default)]
        failed: u64,
        #[serde(default)]
        bytes_done: u64,
        #[serde(default)]
        bytes_total: u64,
        #[serde(default)]
        speed: f64,
        #[serde(default)]
        active: u64,
        #[serde(default)]
        waiting: u64,
        #[serde(default)]
        activity: String,
    },
    FileDone {
        #[serde(default)]
        path: String,
        #[serde(default)]
        size: u64,
        #[serde(default)]
        skipped: bool,
    },
    FileFailed {
        #[serde(default)]
        path: String,
        #[serde(default)]
        error: String,
    },
    Finished {
        #[serde(default)]
        ok: bool,
        #[serde(default)]
        total: u64,
        #[serde(default)]
        skipped: u64,
        #[serde(default)]
        failed: u64,
        #[serde(default)]
        bytes: u64,
        #[serde(default)]
        scan_only: bool,
    },
    Fatal {
        #[serde(default)]
        message: String,
    },
    #[serde(other)]
    Unknown,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_progress_event() {
        let raw = r#"{"event":"progress","done":5,"total":10,"skipped":1,"failed":0,"bytes_done":2048,"bytes_total":4096,"speed":1024.5,"active":2,"waiting":3,"activity":"test.bin"}"#;
        let Event::Progress {
            done,
            total,
            skipped,
            active,
            waiting,
            speed,
            activity,
            ..
        } = serde_json::from_str(raw).unwrap()
        else {
            panic!("unexpected event");
        };

        assert_eq!((done, total, skipped, active, waiting), (5, 10, 1, 2, 3));
        assert!((speed - 1024.5).abs() < f64::EPSILON);
        assert_eq!(activity, "test.bin");
    }

    #[test]
    fn parses_scan_and_finished_events() {
        let scan: Event =
            serde_json::from_str(r#"{"event":"scan_done","total":937,"bytes":580903796}"#).unwrap();
        assert!(matches!(
            scan,
            Event::ScanDone {
                total: 937,
                bytes: 580903796
            }
        ));

        let finished: Event = serde_json::from_str(
            r#"{"event":"finished","ok":true,"total":2,"skipped":1,"failed":0,"bytes":42,"scan_only":true}"#,
        )
        .unwrap();
        assert!(matches!(
            finished,
            Event::Finished {
                ok: true,
                skipped: 1,
                scan_only: true,
                ..
            }
        ));
    }

    #[test]
    fn ignores_unknown_events() {
        let event: Event = serde_json::from_str(r#"{"event":"brand_new","x":1}"#).unwrap();
        assert!(matches!(event, Event::Unknown));
    }
}
