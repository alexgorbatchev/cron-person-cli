#[test]
fn go_rust_native_process_contracts() {
    let crate_dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let fixture = tempfile::tempdir().expect("native fixture");
    let go = fixture.path().join(if cfg!(windows) {
        "go-filelock.exe"
    } else {
        "go-filelock"
    });
    let status = std::process::Command::new("go")
        .args(["test", "-c", "-o"])
        .arg(&go)
        .arg("./rust/reference/filelock")
        .current_dir(crate_dir.join("../.."))
        .status()
        .expect("build genuine Go helper");
    assert!(status.success());
    let status = std::process::Command::new("uv")
        .args(["run", "--no-project", "python"])
        .arg(crate_dir.join("../reference/filelock-native.py"))
        .arg(go)
        .arg(env!("CARGO_BIN_EXE_filelock-probe"))
        .env("FILELOCK_PROCESS", "1")
        .current_dir(crate_dir.join("../.."))
        .status()
        .expect("run native process contracts");
    assert!(status.success());
}
