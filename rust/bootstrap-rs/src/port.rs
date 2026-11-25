//! Data-only parity boundary; the fixture driver invokes the actual Rust executable.

#![forbid(unsafe_code)]

use std::io::Write;
use std::process::{Command, Stdio};

pub fn scenario(input: String) -> Result<String, String> {
    let driver = std::env::var_os("BOOTSTRAP_FIXTURE_DRIVER")
        .ok_or("BOOTSTRAP_FIXTURE_DRIVER is required")?;
    let target =
        std::env::var_os("BOOTSTRAP_RUST_PROBE").ok_or("BOOTSTRAP_RUST_PROBE is required")?;
    invoke(driver, target, input)
}

fn invoke(
    driver: std::ffi::OsString,
    target: std::ffi::OsString,
    input: String,
) -> Result<String, String> {
    let mut child = Command::new(driver)
        .env("BOOTSTRAP_FIXTURE_RUN", "1")
        .env("BOOTSTRAP_TARGET", target)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| e.to_string())?;
    if let Some(mut stdin) = child.stdin.take()
        && let Err(error) = stdin.write_all(input.as_bytes())
    {
        let killed = child.kill();
        let waited = child.wait();
        return Err(format!("{error}; child cleanup: {killed:?}, {waited:?}"));
    }
    let output = child.wait_with_output().map_err(|e| e.to_string())?;
    if !output.status.success() {
        return Err(String::from_utf8_lossy(&output.stderr).into_owned());
    }
    #[derive(serde::Deserialize)]
    struct Reply {
        value: String,
        error: String,
    }
    let reply: Reply = serde_json::from_slice(&output.stdout).map_err(|e| e.to_string())?;
    if reply.error.is_empty() {
        Ok(reply.value)
    } else {
        Err(reply.error)
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;

    #[test]
    fn adapter_reports_real_driver_and_protocol_failures() -> Result<(), String> {
        let driver =
            std::env::var_os("BOOTSTRAP_FIXTURE_DRIVER").ok_or("missing fixture driver")?;
        let target = std::env::var_os("BOOTSTRAP_RUST_PROBE").ok_or("missing Rust probe")?;
        assert!(invoke(driver, target, "invalid JSON".into()).is_err());
        for driver in ["/missing-bootstrap-driver", "/bin/true", "/bin/false"] {
            assert!(invoke(driver.into(), "/unused".into(), "{}".into()).is_err());
        }
        assert!(invoke("/bin/true".into(), "/unused".into(), "x".repeat(1_048_576)).is_err());
        Ok(())
    }
}
