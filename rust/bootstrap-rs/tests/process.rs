#![cfg(all(unix, feature = "bridge"))]
use serde_json::Value;
use std::{
    io::Write,
    process::{Command, Stdio},
};

// The fixture driver owns a bounded process group and removes its system-temp tree.
fn observe(input: &str, binary: &str) -> Result<Value, Box<dyn std::error::Error>> {
    let driver = std::env::var_os("BOOTSTRAP_FIXTURE_DRIVER")
        .ok_or("run through bootstrap-tools.py for the isolated fixture driver")?;
    let mut child = Command::new(driver)
        .env("BOOTSTRAP_FIXTURE_RUN", "1")
        .env("BOOTSTRAP_TARGET", binary)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()?;
    child
        .stdin
        .take()
        .ok_or("missing stdin")?
        .write_all(input.as_bytes())?;
    let output = child.wait_with_output()?;
    assert!(output.status.success(), "{:?}", output);
    Ok(serde_json::from_slice(&output.stdout)?)
}

#[test]
fn native_process_and_persistent_effects_match_recorded_go()
-> Result<(), Box<dyn std::error::Error>> {
    for (i, line) in include_str!("../cases.jsonl").lines().enumerate() {
        let case: Value = serde_json::from_str(line)?;
        let input = case["args"][0].as_str().ok_or("missing input")?;
        let req = serde_json::from_str::<Value>(input).ok();
        let binary = if req.as_ref().is_some_and(|r| r["op"] == "main") {
            env!("CARGO_BIN_EXE_dev-bootstrap")
        } else {
            env!("CARGO_BIN_EXE_bootstrap-probe")
        };
        let reply = observe(input, binary)?;
        if let Some(error) = case["error"].as_str() {
            assert_eq!(reply["error"], error, "case {i}: {input}");
            assert_eq!(reply["value"], "", "case {i}: {input}");
        } else {
            assert_eq!(reply["error"], "", "case {i}: {input}");
            assert_eq!(reply["value"], case["ok"], "case {i}: {input}");
        }
    }
    Ok(())
}

#[test]
fn actual_bridge_calls_native_implementation() -> Result<(), String> {
    for input in [
        r#"{"op":"bootstrap","out":"762e310a"}"#,
        r#"{"op":"bootstrap","which_status":4}"#,
    ] {
        let result = bootstrap_rs::port::scenario(input.into())?;
        assert!(result.contains("6d616e616765642072656c6561736500ff"));
    }
    Ok(())
}
