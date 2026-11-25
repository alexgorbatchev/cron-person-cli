//! Development override registration, preserving the Go helper's command order and bytes.
use crate::quote::quote;
use std::env;
use std::ffi::{OsStr, OsString};
use std::io::{self, Write};
use std::path::{Component, Path, PathBuf};
use std::process::{Command, Stdio};

/// Raw diagnostic bytes. Unix paths and child output need not be UTF-8.
#[derive(Debug, PartialEq, Eq)]
pub struct Error(pub Vec<u8>);
impl Error {
    fn text(message: &str) -> Self {
        Self(message.as_bytes().to_vec())
    }
    fn context(mut self, context: &[u8]) -> Self {
        let mut bytes = context.to_vec();
        bytes.append(&mut self.0);
        Self(bytes)
    }
}

fn bytes(value: &OsStr) -> Vec<u8> {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        value.as_bytes().to_vec()
    }
    #[cfg(not(unix))]
    {
        value.to_string_lossy().as_bytes().to_vec()
    }
}
fn os_string(value: Vec<u8>) -> OsString {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStringExt;
        OsString::from_vec(value)
    }
    #[cfg(not(unix))]
    {
        OsString::from(String::from_utf8_lossy(&value).into_owned())
    }
}
fn system_error(err: io::Error) -> Error {
    if err.kind() == io::ErrorKind::InvalidInput {
        return Error::text("invalid argument");
    }
    let text = err.to_string();
    Error::text(
        text.split(" (os error ")
            .next()
            .unwrap_or(&text)
            .to_lowercase()
            .as_str(),
    )
}
fn path_error(operation: &str, path: &Path, err: io::Error) -> Error {
    let mut prefix = operation.as_bytes().to_vec();
    prefix.extend(bytes(path.as_os_str()));
    prefix.extend(b": ");
    system_error(err).context(&prefix)
}
fn absolute(path: &Path) -> Result<PathBuf, Error> {
    let absolute = if path.is_absolute() {
        path.to_path_buf()
    } else {
        working_directory()?.join(path)
    };
    let mut clean = PathBuf::new();
    for component in absolute.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                clean.pop();
            }
            other => clean.push(other),
        }
    }
    Ok(clean)
}

fn working_directory() -> Result<PathBuf, Error> {
    #[cfg(unix)]
    if let Some(pwd) = env::var_os("PWD")
        .map(PathBuf::from)
        .filter(|p| p.is_absolute())
    {
        use std::os::unix::fs::MetadataExt;
        let dot = Path::new(".")
            .metadata()
            .map_err(|e| path_error("stat ", Path::new("."), e))?;
        if pwd
            .metadata()
            .is_ok_and(|m| m.dev() == dot.dev() && m.ino() == dot.ino())
        {
            return Ok(pwd);
        }
    }
    env::current_dir().map_err(|e| system_error(e).context(b"getwd: "))
}

fn trim_space(mut data: &[u8]) -> Vec<u8> {
    fn space(data: &[u8]) -> Option<usize> {
        for n in 1..=data.len().min(4) {
            if let Ok(text) = std::str::from_utf8(&data[..n]) {
                return text.chars().next().filter(|c| c.is_whitespace()).map(|_| n);
            }
        }
        None
    }
    while let Some(n) = space(data) {
        data = &data[n..];
    }
    loop {
        let n = (1..=data.len().min(4)).find(|&n| space(&data[data.len() - n..]) == Some(n));
        if let Some(n) = n {
            data = &data[..data.len() - n];
        } else {
            break;
        }
    }
    data.to_vec()
}

#[cfg(unix)]
fn executable(path: &Path) -> bool {
    use rustix::fs::{Access, AtFlags, CWD, accessat};
    let Ok(meta) = path.metadata() else {
        return false;
    };
    if meta.is_dir() {
        return false;
    }
    match accessat(CWD, path, Access::EXEC_OK, AtFlags::EACCESS) {
        Ok(()) => true,
        Err(rustix::io::Errno::NOSYS | rustix::io::Errno::PERM) => {
            use std::os::unix::fs::PermissionsExt;
            meta.permissions().mode() & 0o111 != 0
        }
        Err(_) => false,
    }
}
fn lookup(program: &OsStr) -> Result<PathBuf, Error> {
    if program.is_empty() {
        return Err(Error::text("exec: no command"));
    }
    let path = Path::new(program);
    if path.components().count() > 1 || path.is_absolute() || bytes(program).contains(&b'/') {
        return Ok(path.to_path_buf());
    }
    let quote = format!("exec: {}: ", quote(&bytes(program)));
    let Some(search) = env::var_os("PATH") else {
        return Err(Error::text("executable file not found in $PATH").context(quote.as_bytes()));
    };
    if search.is_empty() {
        return Err(Error::text("executable file not found in $PATH").context(quote.as_bytes()));
    }
    for dir in env::split_paths(&search) {
        let dir = if dir.as_os_str().is_empty() {
            PathBuf::from(".")
        } else {
            dir
        };
        let candidate = dir.join(program);
        #[cfg(unix)]
        let found = executable(&candidate);
        #[cfg(not(unix))]
        let found = candidate.is_file();
        if found {
            let permit_relative = env::var_os("GODEBUG").is_some_and(|v| {
                v.to_string_lossy()
                    .split(',')
                    .rev()
                    .find(|v| v.starts_with("execerrdot="))
                    .is_some_and(|v| v == "execerrdot=0")
            });
            if candidate.is_relative() && !permit_relative {
                return Err(Error::text(
                    "cannot run executable found relative to current directory",
                )
                .context(quote.as_bytes()));
            }
            return Ok(candidate);
        }
    }
    Err(Error::text("executable file not found in $PATH").context(quote.as_bytes()))
}

/// Execute with inherited environment, closed stdin, captured stdout and Go-style errors.
pub fn command_output(
    directory: &Path,
    program: &OsStr,
    args: &[OsString],
) -> Result<Vec<u8>, Error> {
    let context = [b"running ".as_slice(), &bytes(program), b": "].concat();
    let executable = lookup(program).map_err(|e| e.context(&context))?;
    let mut command = Command::new(&executable);
    command.args(args).stdin(Stdio::null());
    if !directory.as_os_str().is_empty() {
        command.current_dir(directory);
        command.env("PWD", absolute(directory).map_err(|e| e.context(&context))?);
        directory
            .metadata()
            .map_err(|e| path_error("chdir ", directory, e).context(&context))?;
    }
    #[cfg(unix)]
    {
        use std::os::unix::process::CommandExt;
        command.arg0(program);
    }
    let output = command
        .output()
        .map_err(|err| path_error("fork/exec ", &executable, err).context(&context))?;
    if output.status.success() {
        return Ok(trim_space(&output.stdout));
    }
    let mut stderr = output.stderr;
    if stderr.len() > 65536 {
        stderr = [
            &stderr[..32768],
            format!("\n... omitting {} bytes ...\n", stderr.len() - 65536).as_bytes(),
            &stderr[stderr.len() - 32768..],
        ]
        .concat();
    }
    let status = if let Some(code) = output.status.code() {
        format!("exit status {code}")
    } else {
        #[cfg(unix)]
        {
            use std::os::unix::process::ExitStatusExt;
            let signal = output.status.signal().unwrap_or(0);
            let name = signal_name(signal);
            format!(
                "signal: {name}{}",
                if output.status.core_dumped() {
                    " (core dumped)"
                } else {
                    ""
                }
            )
        }
        #[cfg(not(unix))]
        {
            output.status.to_string()
        }
    };
    Err(Error(
        [
            bytes(program),
            b" failed: ".to_vec(),
            trim_space(&stderr),
            b": ".to_vec(),
            status.into_bytes(),
        ]
        .concat(),
    ))
}

#[cfg(unix)]
fn signal_name(signal: i32) -> String {
    // These are syscall.Signal.String's names, which differ between Linux and Darwin.
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
    names
        .get(signal as usize)
        .map_or_else(|| format!("signal {signal}"), |s| (*s).to_owned())
}

/// Register a build, then ask dotfiles for its path; completed registration is not rolled back.
pub fn bootstrap(source: &Path, dotfiles: &Path) -> Result<Vec<u8>, Error> {
    let directory = absolute(dotfiles).map_err(|e| e.context(b"resolving dotfiles directory: "))?;
    let help = command_output(
        &directory,
        OsStr::new("dotfiles"),
        &["tool", "dev", "use", "--help"].map(OsString::from),
    );
    if !help.is_ok_and(|s| s.windows(5).any(|s| s == b"--bin")) {
        return Err(Error::text(
            "update dotfiles to a version supporting 'tool dev use' before bootstrapping",
        ));
    }
    let source = absolute(source).map_err(|e| e.context(b"resolving build path: "))?;
    let info = source
        .metadata()
        .map_err(|e| path_error("stat ", &source, e).context(b"reading build: "))?;
    if info.len() == 0 {
        return Err(Error::text("build output is empty"));
    }
    let repository =
        working_directory().map_err(|e| e.context(b"resolving source repository: "))?;
    let version = command_output(
        &repository,
        source.as_os_str(),
        &[OsString::from("--version")],
    )?;
    let mut bin = OsString::from("cron-person=");
    bin.push(&source);
    command_output(
        &directory,
        OsStr::new("dotfiles"),
        &[
            OsString::from("tool"),
            "dev".into(),
            "use".into(),
            "cron-person".into(),
            "--bin".into(),
            bin,
            "--source-repo".into(),
            repository.into_os_string(),
            "--version".into(),
            os_string(version),
        ],
    )
    .map_err(|e| e.context(b"registering development build: "))?;
    command_output(
        &directory,
        OsStr::new("dotfiles"),
        &["tool", "which", "cron-person", "--bin"].map(OsString::from),
    )
}

/// Resolve the recipe environment, register bin/cron-person, and write the success line.
pub fn run(stdout: &mut impl Write) -> Result<(), Error> {
    let directory = env::var_os("dotfiles_dir")
        .filter(|s| !s.is_empty())
        .ok_or_else(|| Error::text("run through just dev-bootstrap"))?;
    let destination = bootstrap(Path::new("bin/cron-person"), Path::new(&directory))?;
    // fmt.Println in the original ignores output errors.
    let _ = stdout.write_all(
        &[
            b"Registered local cron-person build at ".as_slice(),
            &destination,
            b"\n",
        ]
        .concat(),
    );
    Ok(())
}

/// Main's stream and exit-status contract. Arguments are intentionally ignored by the helper.
pub fn main_status(stdout: &mut impl Write, stderr: &mut impl Write) -> i32 {
    match run(stdout) {
        Ok(()) => 0,
        Err(err) => {
            let _ = stderr.write_all(&[err.0, b"\n".to_vec()].concat());
            1
        }
    }
}

struct StandardStream<W>(W);
impl<W: Write> Write for StandardStream<W> {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        let result = self.0.write(bytes);
        // Go treats EPIPE on the standard streams as SIGPIPE even though Println ignores errors.
        #[cfg(unix)]
        if let Err(ref error) = result
            && error.kind() == io::ErrorKind::BrokenPipe
        {
            signal_hook::low_level::emulate_default_handler(signal_hook::consts::SIGPIPE)?;
        }
        result
    }
    fn flush(&mut self) -> io::Result<()> {
        self.0.flush()
    }
}

/// Standalone main, including Go's native broken-standard-pipe termination behavior.
pub fn process_main() -> i32 {
    main_status(
        &mut StandardStream(io::stdout()),
        &mut StandardStream(io::stderr()),
    )
}
