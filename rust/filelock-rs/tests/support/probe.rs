//! Test-only native process endpoint; never linked into the application.
use filelock_rs::filelock::{self, Error, LockFile};
use std::{
    io::{self, BufRead, Write},
    process::{Child, Command, Stdio},
};

struct ChildGuard(Child);
impl Drop for ChildGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
fn outcome(result: Result<(), Error>) -> String {
    match result {
        Ok(()) => "ok".into(),
        Err(e) if e.raw_os_error().is_some() => format!("os:{}", e.raw_os_error().unwrap_or(-1)),
        Err(e) => e.to_string(),
    }
}
fn say(message: &str) -> io::Result<()> {
    println!("{message}");
    io::stdout().flush()
}
fn run() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<_> = std::env::args_os().collect();
    let mode = args.get(1).ok_or("mode required")?;
    let path = args.get(2).ok_or("path required")?;
    let mut lines = io::stdin().lock().lines();
    if mode == "idle" {
        say("idle")?;
        lines.next().transpose()?;
        return Ok(());
    }
    if mode == "block" {
        say("waiting")?;
        let mut guard = filelock::lock_path(path)?;
        say("acquired")?;
        lines.next().transpose()?;
        guard.release()?;
        return Ok(());
    }
    let mut file = match LockFile::open(path) {
        Ok(file) => file,
        Err(error) => {
            say(&outcome(Err(error)))?;
            return Ok(());
        }
    };
    if mode == "probe" {
        say(&outcome(filelock::try_lock(&file)))?;
        return Ok(());
    }
    filelock::lock(&file)?;
    say("held")?;
    let mut child = None;
    for line in lines {
        match line?.as_str() {
            "unlock" => say(&outcome(filelock::unlock(&file)))?,
            "close" => say(&outcome(file.close()))?,
            command @ ("spawn" | "spawn-inherit") => {
                let mut command_builder = Command::new(std::env::current_exe()?);
                command_builder
                    .args([std::ffi::OsStr::new("idle"), path])
                    .stdin(Stdio::piped())
                    .stdout(Stdio::piped());
                if command == "spawn-inherit" {
                    command_builder.stderr(Stdio::from(file.file()?.try_clone()?));
                }
                let mut spawned = ChildGuard(command_builder.spawn()?);
                let output = spawned.0.stdout.take().ok_or("missing child output")?;
                let mut reader = io::BufReader::new(output);
                let mut line = String::new();
                reader.read_line(&mut line)?;
                if line.trim() != "idle" {
                    return Err("child did not start".into());
                }
                child = Some(spawned);
                say("spawned")?;
            }
            "exit" => break,
            _ => return Err("unknown process command".into()),
        }
    }
    drop(child);
    Ok(())
}
fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        std::process::exit(2);
    }
}
