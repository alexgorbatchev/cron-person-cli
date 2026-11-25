//! Native shell generation. Export, help and unported argument forms use Go.

use std::ffi::OsString;
use std::io::{self, Write};
use std::process::ExitCode;

use clap::ArgMatches;

pub fn run(_command: &str, matches: &ArgMatches) -> ExitCode {
    let args: Vec<&OsString> = matches
        .get_many::<OsString>("args")
        .into_iter()
        .flatten()
        .collect();
    if matches.get_count("help") == 0
        && let [shell] = args.as_slice()
        && let Some(shell @ ("bash" | "zsh" | "fish")) = shell.to_str()
    {
        match hook_rs::port::generate(shell.as_bytes(), b"cron-person") {
            Ok(script) => return write_script(&script),
            Err(error) => return report_error(error.as_bytes()),
        }
    }
    let argv: Vec<OsString> = std::env::args_os().skip(1).collect();
    crate::original::run(&argv)
}

fn write_script(script: &[u8]) -> ExitCode {
    match io::stdout().lock().write_all(script) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            #[cfg(unix)]
            if error.kind() == io::ErrorKind::BrokenPipe {
                // Go terminates with SIGPIPE on a failed stdout pipe write.
                // SAFETY: SIG_DFL is a valid disposition; no handler or borrowed
                // state is installed. SIGPIPE deliberately terminates this process.
                unsafe {
                    libc::signal(libc::SIGPIPE, libc::SIG_DFL);
                    libc::raise(libc::SIGPIPE);
                }
                return ExitCode::from(141);
            }
            let error = filelock_rs::filelock::Error::Path {
                operation: "write",
                path: "/dev/stdout".into(),
                error: Box::new(filelock_rs::filelock::Error::Os(error)),
            };
            report_error(&error.error_bytes())
        }
    }
}

fn report_error(error: &[u8]) -> ExitCode {
    let mut stderr = io::stderr().lock();
    let agent = std::env::var("AGENT").unwrap_or_default();
    let prefix = if matches!(agent.trim().to_lowercase().as_str(), "1" | "true" | "yes") {
        b"ERR: ".as_slice()
    } else {
        b"[ERROR] ".as_slice()
    };
    let _ = stderr.write_all(prefix);
    let _ = stderr.write_all(error);
    let _ = stderr.write_all(b"\n");
    ExitCode::FAILURE
}
