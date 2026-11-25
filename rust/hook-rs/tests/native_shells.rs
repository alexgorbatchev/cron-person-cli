#[cfg(unix)]
#[test]
fn native_shell_lifecycle_contracts() -> Result<(), Box<dyn std::error::Error>> {
    let fixture = tempfile::tempdir()?;
    for shell in ["bash", "zsh", "fish"] {
        for (kind, binary) in [
            (
                "explicit",
                fixture.path().join("tool with spaces").into_os_string(),
            ),
            ("default", "".into()),
            ("expansion", "$HOOK_BINARY".into()),
        ] {
            use std::os::unix::ffi::OsStrExt;
            let script = hook_rs::port::generate(shell.as_bytes(), binary.as_bytes())?;
            std::fs::write(fixture.path().join(format!("{shell}-{kind}")), script)?;
        }
    }
    let crate_dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let status = std::process::Command::new("uv")
        .args(["run", "--no-project", "python"])
        .arg(std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/shell_contracts.py"))
        .arg(fixture.path())
        .current_dir(crate_dir.join("../.."))
        .status()?;
    assert!(status.success(), "native shell contract failed");
    Ok(())
}
