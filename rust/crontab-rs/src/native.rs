//! Byte-preserving operations; store-dependent generation and updates are not ported.
#![forbid(unsafe_code)]

use std::{error, fmt, io};

pub const BLOCK_START: &[u8] = b"# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>";
pub const BLOCK_END: &[u8] = b"# <<< cron-person managed block <<<";

/// Diagnostics can contain arbitrary stderr/path bytes. Display is a UTF-8 view.
#[derive(Debug)]
pub struct Error {
    pub bytes: Vec<u8>,
    pub source: Option<io::Error>,
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&String::from_utf8_lossy(&self.bytes))
    }
}
impl error::Error for Error {
    fn source(&self) -> Option<&(dyn error::Error + 'static)> {
        self.source.as_ref().map(|e| e as _)
    }
}
impl From<String> for Error {
    fn from(text: String) -> Self {
        Self {
            bytes: text.into_bytes(),
            source: None,
        }
    }
}

/// Match whole marker lines, allowing a single CR before LF or EOF.
pub fn remove_block(existing: &[u8]) -> Result<Vec<u8>, Error> {
    let (mut start, mut end) = (None, None);
    let mut offset = 0;
    for line in existing.split_inclusive(|&b| b == b'\n') {
        let content = line.strip_suffix(b"\n").unwrap_or(line);
        let content = content.strip_suffix(b"\r").unwrap_or(content);
        if content == BLOCK_START {
            if start.is_some() && end.is_none() {
                return Err(format!(
                    "malformed crontab: nested {:?} without matching end marker",
                    String::from_utf8_lossy(BLOCK_START)
                )
                .into());
            }
            if end.is_some() {
                return Err(
                    "ambiguous crontab: multiple cron-person managed blocks found"
                        .to_owned()
                        .into(),
                );
            }
            start = Some(offset);
        } else if content == BLOCK_END {
            if start.is_none() || end.is_some() {
                return Err(format!(
                    "malformed crontab: unexpected {:?} without matching start marker",
                    String::from_utf8_lossy(BLOCK_END)
                )
                .into());
            }
            end = Some(offset + line.len());
        }
        offset += line.len();
    }
    match (start, end) {
        (Some(_), None) => Err(format!(
            "malformed crontab: unclosed {:?} without matching end marker",
            String::from_utf8_lossy(BLOCK_START)
        )
        .into()),
        (Some(start), Some(end)) => Ok([&existing[..start], &existing[end..]].concat()),
        _ => Ok(existing.to_vec()),
    }
}

/// Remove the old block, trim only trailing LF bytes, then append without validating the new block.
pub fn merge_crontab(existing: &[u8], managed: &[u8]) -> Result<Vec<u8>, Error> {
    let mut cleaned = remove_block(existing)?;
    cleaned.truncate(
        cleaned
            .iter()
            .rposition(|&b| b != b'\n')
            .map_or(0, |i| i + 1),
    );
    if !managed.is_empty() {
        if !cleaned.is_empty() {
            cleaned.extend_from_slice(b"\n\n");
        }
        cleaned.extend_from_slice(managed);
    }
    if !cleaned.is_empty() {
        cleaned.push(b'\n');
    }
    Ok(cleaned)
}

#[cfg(unix)]
pub use process::SystemCrontab;

#[cfg(unix)]
mod process {
    use super::*;
    use std::{
        collections::BTreeMap,
        ffi::{OsStr, OsString},
        fs,
        io::Write,
        os::unix::{
            ffi::OsStrExt,
            process::{CommandExt, ExitStatusExt},
        },
        path::PathBuf,
        process::{Command, ExitStatus, Stdio},
        thread,
    };

    /// A per-invocation environment and directory, never process-global mutations.
    pub struct SystemCrontab {
        pub environment: BTreeMap<OsString, OsString>,
        pub directory: PathBuf,
    }
    impl SystemCrontab {
        pub fn inherited() -> io::Result<Self> {
            Ok(Self {
                environment: std::env::vars_os().collect(),
                directory: std::env::current_dir()?,
            })
        }
        fn executable(&self) -> Result<PathBuf, Error> {
            let path = self
                .environment
                .get(OsStr::new("PATH"))
                .map_or(OsStr::new(""), OsString::as_os_str);
            if !path.is_empty() {
                for dir in std::env::split_paths(path) {
                    let file = dir.join("crontab");
                    let absolute = if file.is_absolute() {
                        file.clone()
                    } else {
                        self.directory.join(&file)
                    };
                    if fs::metadata(&absolute).is_ok_and(|m| !m.is_dir())
                        && rustix::fs::accessat(
                            rustix::fs::CWD,
                            &absolute,
                            rustix::fs::Access::EXEC_OK,
                            rustix::fs::AtFlags::EACCESS,
                        )
                        .is_ok()
                    {
                        let godebug = self
                            .environment
                            .get(OsStr::new("GODEBUG"))
                            .map_or(&b""[..], |v| v.as_bytes());
                        if !file.is_absolute()
                            && !godebug
                                .split(|&b| b == b',')
                                .rev()
                                .find(|v| v.starts_with(b"execerrdot="))
                                .is_some_and(|v| v == b"execerrdot=0")
                        {
                            return Err("exec: \"crontab\": cannot run executable found relative to current directory".to_owned().into());
                        }
                        return Ok(file);
                    }
                }
            }
            Err("exec: \"crontab\": executable file not found in $PATH"
                .to_owned()
                .into())
        }
        /// `crontab -l`: return raw stdout; suppress a failed read containing the exact no-crontab substring.
        pub fn read(&self) -> Result<Vec<u8>, Error> {
            self.run(None)
        }
        /// `crontab -`: send exact content; discard stdout; preserve failure diagnostics.
        pub fn write(&self, content: &[u8]) -> Result<(), Error> {
            self.run(Some(content)).map(|_| ())
        }
        fn run(&self, content: Option<&[u8]>) -> Result<Vec<u8>, Error> {
            let context: &[u8] = if content.is_some() {
                b"installing crontab: "
            } else {
                b"reading system crontab (crontab -l): "
            };
            let result = (|| {
                let executable = self.executable()?;
                let mut cmd = Command::new(&executable);
                cmd.arg0("crontab")
                    .arg(if content.is_some() { "-" } else { "-l" })
                    .env_clear()
                    .envs(&self.environment)
                    .current_dir(&self.directory)
                    .stdin(if content.is_some() {
                        Stdio::piped()
                    } else {
                        Stdio::null()
                    })
                    .stdout(if content.is_some() {
                        Stdio::null()
                    } else {
                        Stdio::piped()
                    })
                    .stderr(Stdio::piped());
                let mut child = cmd.spawn().map_err(|e| {
                    let mut bytes = b"fork/exec ".to_vec();
                    bytes.extend_from_slice(executable.as_os_str().as_bytes());
                    bytes.extend_from_slice(b": ");
                    bytes.extend(os_message(&e));
                    Error {
                        bytes,
                        source: Some(e),
                    }
                })?;
                // Concurrent stdin and output draining avoids deadlock when both pipes exceed capacity.
                let (output, copied) = thread::scope(|scope| {
                    let writer = child.stdin.take().map(|mut stdin| {
                        scope.spawn(move || {
                            // cgo hosts may terminate on SIGPIPE. Block it only in this short-lived
                            // writer thread; pending pipe signals disappear when the thread exits.
                            let mut signals = nix::sys::signal::SigSet::empty();
                            signals.add(nix::sys::signal::Signal::SIGPIPE);
                            signals.thread_block().map_err(io::Error::from)?;
                            stdin.write_all(content.unwrap_or_default())
                        })
                    });
                    let output = child.wait_with_output();
                    let copied = writer.map(|w| w.join());
                    (output, copied)
                });
                let output = output.map_err(|e| Error {
                    bytes: os_message(&e),
                    source: Some(e),
                })?;
                if !output.status.success() {
                    if content.is_none()
                        && output
                            .stderr
                            .windows(b"no crontab for".len())
                            .any(|s| s == b"no crontab for")
                    {
                        return Ok(Vec::new());
                    }
                    let mut bytes = status_text(output.status).into_bytes();
                    bytes.extend_from_slice(b": ");
                    bytes.extend(output.stderr);
                    return Err(Error {
                        bytes,
                        source: None,
                    });
                }
                if let Some(Ok(Err(e))) = copied
                    && e.kind() != io::ErrorKind::BrokenPipe
                {
                    return Err(Error {
                        bytes: os_message(&e),
                        source: Some(e),
                    });
                }
                Ok(output.stdout)
            })();
            result.map_err(|mut error: Error| {
                // Lookup/spawn failures still include the empty stderr suffix from Go's wrapper.
                if error.bytes.starts_with(b"exec:") || error.bytes.starts_with(b"fork/exec") {
                    error.bytes.extend_from_slice(b": ");
                }
                error.bytes.splice(..0, context.iter().copied());
                error
            })
        }
    }
    fn os_message(error: &io::Error) -> Vec<u8> {
        // Preserve the actual errno; ErrorKind alone loses the platform diagnostic.
        if let Some(code) = error.raw_os_error() {
            filelock_rs::filelock::Error::Os(io::Error::from_raw_os_error(code)).error_bytes()
        } else {
            error.to_string().into_bytes()
        }
    }
    fn status_text(status: ExitStatus) -> String {
        if let Some(code) = status.code() {
            return format!("exit status {code}");
        }
        let signal = status.signal().unwrap_or_default();
        #[cfg(target_os = "linux")]
        let names = [
            "",
            "hangup",
            "interrupt",
            "quit",
            "illegal instruction",
            "trace/breakpoint trap",
            "aborted",
            "bus error",
            "floating point exception",
            "killed",
            "user defined signal 1",
            "segmentation fault",
            "user defined signal 2",
            "broken pipe",
            "alarm clock",
            "terminated",
            "stack fault",
            "child exited",
            "continued",
            "stopped (signal)",
            "stopped",
            "stopped (tty input)",
            "stopped (tty output)",
            "urgent I/O condition",
            "CPU time limit exceeded",
            "file size limit exceeded",
            "virtual timer expired",
            "profiling timer expired",
            "window changed",
            "I/O possible",
            "power failure",
            "bad system call",
        ];
        #[cfg(not(target_os = "linux"))]
        let names = [
            "",
            "hangup",
            "interrupt",
            "quit",
            "illegal instruction",
            "trace/BPT trap",
            "abort trap",
            "EMT trap",
            "floating point exception",
            "killed",
            "bus error",
            "segmentation fault",
            "bad system call",
            "broken pipe",
            "alarm clock",
            "terminated",
            "urgent I/O condition",
            "suspended (signal)",
            "suspended",
            "continued",
            "child exited",
            "stopped (tty input)",
            "stopped (tty output)",
            "I/O possible",
            "cputime limit exceeded",
            "filesize limit exceeded",
            "virtual timer expired",
            "profiling timer expired",
            "window size changes",
            "information request",
            "user defined signal 1",
            "user defined signal 2",
        ];
        let name = names
            .get(signal as usize)
            .map_or_else(|| format!("signal {signal}"), |s| (*s).to_owned());
        format!(
            "signal: {name}{}",
            if status.core_dumped() {
                " (core dumped)"
            } else {
                ""
            }
        )
    }
}
