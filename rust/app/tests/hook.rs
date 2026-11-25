use std::{
    fs, io,
    path::Path,
    process::{Command, Output, Stdio},
};

fn original() -> std::path::PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../bin/cron-person-go-reference")
}

fn command(executable: &Path, args: &[&str], agent: &str, log: &Path) -> Command {
    let mut command = Command::new(executable);
    command
        .args(args)
        .env("AGENT", agent)
        .env("RUST_ANYTHING_FALLBACK_LOG", log)
        .env_remove("RUST_ANYTHING_UNIT_HOOK")
        .stdin(Stdio::null());
    command
}

fn same_output(expected: &Output, actual: &Output) {
    assert_eq!(actual.status, expected.status);
    assert_eq!(actual.stdout, expected.stdout);
    assert_eq!(actual.stderr, expected.stderr);
}

#[test]
fn scripts_are_native_and_unported_forms_fall_back() -> io::Result<()> {
    let fixture = tempfile::tempdir()?;
    let rust = Path::new(env!("CARGO_BIN_EXE_cron-person"));
    for args in [
        vec!["hook", "bash"],
        vec!["hook", "zsh"],
        vec!["hook", "fish"],
        vec!["hook", "--", "bash"],
    ] {
        let log = fixture.path().join("fallback.log");
        let expected = command(&original(), &args, "0", &log).output()?;
        let actual = command(rust, &args, "0", &log).output()?;
        same_output(&expected, &actual);
        assert!(fs::read(&log)?.is_empty(), "native generation fell back");
        fs::remove_file(log)?;
    }
    for args in [
        vec!["hook", "--help"],
        vec!["hook", "unknown"],
        vec!["hook"],
        vec!["hook", "bash", "extra"],
    ] {
        let log = fixture.path().join("fallback.log");
        let expected = command(&original(), &args, "1", &log).output()?;
        let actual = command(rust, &args, "1", &log).output()?;
        same_output(&expected, &actual);
        assert!(
            !fs::read(&log)?.is_empty(),
            "unported form did not fall back"
        );
        fs::remove_file(log)?;
    }
    let log = fixture.path().join("fallback.log");
    let expected = command(&original(), &["hook", "bash"], "0", &log).output()?;
    let actual = command(rust, &["hook", "bash"], "0", &log)
        .env("RUST_ANYTHING_UNIT_HOOK", "original")
        .output()?;
    same_output(&expected, &actual);
    assert!(!fs::read(log)?.is_empty(), "kill switch did not fall back");
    Ok(())
}

#[cfg(target_os = "linux")]
#[test]
fn native_write_errors_preserve_go_diagnostics() -> io::Result<()> {
    let fixture = tempfile::tempdir()?;
    for agent in ["", "0", "1", "true", "yes", " TRUE ", " YES ", "no"] {
        let log = fixture.path().join("fallback.log");
        let expected = command(&original(), &["hook", "bash"], agent, &log)
            .stdout(fs::File::options().write(true).open("/dev/full")?)
            .output()?;
        let actual = command(
            Path::new(env!("CARGO_BIN_EXE_cron-person")),
            &["hook", "bash"],
            agent,
            &log,
        )
        .stdout(fs::File::options().write(true).open("/dev/full")?)
        .output()?;
        same_output(&expected, &actual);
        assert!(!actual.status.success());
        assert!(fs::read(&log)?.is_empty(), "write error fell back");
        fs::remove_file(log)?;
    }
    Ok(())
}

#[cfg(unix)]
#[test]
fn native_broken_stdout_preserves_sigpipe() -> io::Result<()> {
    use std::os::{
        fd::OwnedFd,
        unix::{net::UnixStream, process::ExitStatusExt},
    };
    let fixture = tempfile::tempdir()?;
    for executable in [
        original(),
        Path::new(env!("CARGO_BIN_EXE_cron-person")).to_path_buf(),
    ] {
        let (writer, reader) = UnixStream::pair()?;
        drop(reader);
        let fd: OwnedFd = writer.into();
        let output = command(
            &executable,
            &["hook", "bash"],
            "0",
            &fixture.path().join("fallback.log"),
        )
        .stdout(Stdio::from(fd))
        .output()?;
        assert_eq!(output.status.signal(), Some(libc::SIGPIPE));
        assert!(output.stderr.is_empty());
    }
    Ok(())
}
