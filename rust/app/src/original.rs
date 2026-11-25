use std::ffi::OsString;
use std::io::Write;
use std::path::Path;
use std::process::{Command, ExitCode, ExitStatus};

const ROOT: &str = match option_env!("RUST_ANYTHING_REPO") {
    Some(root) => root,
    None => concat!(env!("CARGO_MANIFEST_DIR"), "/../.."),
};
const RUN: &[&str] = &["bin/cron-person-go-reference"];
const FALLBACK_LOG: &str = "RUST_ANYTHING_FALLBACK_LOG";

fn log() -> Option<std::fs::File> {
    let path = std::env::var_os(FALLBACK_LOG)?;
    std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .ok()
}

/// Tells `rust-anything app replay` this build counts its fallbacks, even when it makes none.
pub fn report_fallbacks() {
    let _ = log();
}

/// Runs the original with the same arguments, stdin and environment, and passes its output and
/// exit code through. Each call is one fallback.
pub fn run(args: &[OsString]) -> ExitCode {
    if let Some(mut file) = log() {
        let line: Vec<String> = args
            .iter()
            .map(|a| a.to_string_lossy().into_owned())
            .collect();
        let _ = writeln!(file, "{}", line.join(" "));
    }
    let Some((program, rest)) = RUN.split_first() else {
        return ExitCode::from(127);
    };
    let mut command = Command::new(Path::new(ROOT).join(program));
    command.args(rest).args(args);
    let status = command.status();
    match status {
        Ok(status) => ExitCode::from(exit_code(status)),
        Err(e) => {
            eprintln!(
                "front door: cannot run the original `{}`: {e}",
                RUN.join(" ")
            );
            ExitCode::from(127)
        }
    }
}

fn exit_code(status: ExitStatus) -> u8 {
    if let Some(code) = status.code() {
        return (code & 0xff) as u8;
    }
    #[cfg(unix)]
    {
        use std::os::unix::process::ExitStatusExt;
        if let Some(signal) = status.signal() {
            return (128 + signal) as u8;
        }
    }
    1
}
