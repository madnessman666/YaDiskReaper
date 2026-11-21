//! Small formatting helpers shared by the UI.

const UNITS: [&str; 5] = ["B", "KiB", "MiB", "GiB", "TiB"];

pub fn bytes(value: u64) -> String {
    let mut value = value as f64;
    let mut unit = 0;

    while value >= 1024.0 && unit < UNITS.len() - 1 {
        value /= 1024.0;
        unit += 1;
    }

    if unit == 0 {
        format!("{value:.0} B")
    } else {
        format!("{value:.1} {}", UNITS[unit])
    }
}

pub fn speed(bytes_per_second: f64) -> String {
    if !bytes_per_second.is_finite() || bytes_per_second <= 0.0 {
        return "0 B/s".to_string();
    }
    format!("{}/s", bytes(bytes_per_second as u64))
}

pub fn eta(done: u64, total: u64, bytes_per_second: f64) -> Option<String> {
    if bytes_per_second < 1.0 || total <= done {
        return None;
    }
    let seconds = ((total - done) as f64 / bytes_per_second) as u64;
    Some(duration(seconds))
}

fn duration(seconds: u64) -> String {
    if seconds >= 3600 {
        format!("{}h {:02}m", seconds / 3600, (seconds % 3600) / 60)
    } else if seconds >= 60 {
        format!("{}m {:02}s", seconds / 60, seconds % 60)
    } else {
        format!("{seconds}s")
    }
}

/// Keep only the file name for compact log lines.
pub fn file_name(path: &str) -> &str {
    path.rsplit(['/', '\\']).next().unwrap_or(path)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn formats_bytes() {
        assert_eq!(bytes(0), "0 B");
        assert_eq!(bytes(999), "999 B");
        assert_eq!(bytes(1024), "1.0 KiB");
        assert_eq!(bytes(1536), "1.5 KiB");
        assert_eq!(bytes(580_903_796), "554.0 MiB");
    }

    #[test]
    fn formats_speed_and_eta() {
        assert_eq!(speed(0.0), "0 B/s");
        assert_eq!(speed(-5.0), "0 B/s");
        assert_eq!(speed(1024.0), "1.0 KiB/s");
        assert_eq!(eta(100, 100, 10.0), None);
        assert_eq!(eta(0, 1024, 1024.0).as_deref(), Some("1s"));
        assert_eq!(eta(0, 70 * 1024, 1024.0).as_deref(), Some("1m 10s"));
    }

    #[test]
    fn extracts_file_name() {
        assert_eq!(file_name("/tmp/some/file.pdf"), "file.pdf");
        assert_eq!(file_name(r"C:\tmp\file.pdf"), "file.pdf");
        assert_eq!(file_name("file.pdf"), "file.pdf");
    }
}
