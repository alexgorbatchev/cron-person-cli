#![cfg(all(unix, feature = "bridge"))]
use serde_json::{Value, json};
use std::{
    ffi::OsString,
    fs,
    io::Write,
    os::unix::{ffi::OsStringExt, fs::PermissionsExt},
    path::{Path, PathBuf},
    process::{Command, Stdio},
    sync::atomic::{AtomicU64, Ordering},
};

type Result<T> = std::result::Result<T, Box<dyn std::error::Error>>;

struct Fixture(PathBuf);
impl Fixture {
    fn new() -> Result<Self> {
        static SERIAL: AtomicU64 = AtomicU64::new(0);
        let mut raw = format!(
            "bootstrap-native-{}-{}-",
            std::process::id(),
            SERIAL.fetch_add(1, Ordering::Relaxed)
        )
        .into_bytes();
        raw.push(0xff);
        let root = std::env::temp_dir().join(OsString::from_vec(raw));
        fs::create_dir(&root)?;
        for name in ["repo", "commands", "home", "child"] {
            if let Err(error) = fs::create_dir(root.join(name)) {
                fs::remove_dir_all(&root)?;
                return Err(error.into());
            }
        }
        if let Err(error) = std::os::unix::fs::symlink(root.join("repo"), root.join("link")) {
            fs::remove_dir_all(&root)?;
            return Err(error.into());
        }
        Ok(Self(root))
    }
    fn write(&self, path: &str, content: &[u8]) -> Result<()> {
        fs::create_dir_all(self.0.join(path).parent().ok_or("missing parent")?)?;
        fs::write(self.0.join(path), content)?;
        fs::set_permissions(self.0.join(path), fs::Permissions::from_mode(0o700))?;
        Ok(())
    }
    fn observe(
        &self,
        binary: &Path,
        request: &Value,
        search: &str,
        debug: &str,
    ) -> Result<Vec<u8>> {
        let mut command = Command::new(binary);
        command
            .env_clear()
            .env("HOME", self.0.join("home"))
            .env("PWD", self.0.join("link"))
            .env("PATH", search)
            .env("GODEBUG", debug)
            .env("BOOTSTRAP_PROBE", "1")
            .envs(std::env::var_os("LLVM_PROFILE_FILE").map(|v| ("LLVM_PROFILE_FILE", v)))
            .current_dir(self.0.join("repo"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if search == "<unset>" {
            command.env_remove("PATH");
        }
        let mut child = command.spawn()?;
        let send = child
            .stdin
            .take()
            .ok_or("missing stdin")?
            .write_all(&serde_json::to_vec(request)?);
        if let Err(error) = send {
            let kill = child.kill();
            let wait = child.wait();
            kill?;
            wait?;
            return Err(error.into());
        }
        let output = child.wait_with_output()?;
        if !output.status.success() || !output.stderr.is_empty() {
            return Err(format!("probe failed: {output:?}").into());
        }
        Ok(output.stdout)
    }
    fn compare(&self, request: Value, search: &str, debug: &str) -> Result<()> {
        let go =
            PathBuf::from(std::env::var_os("BOOTSTRAP_GO_PROBE").ok_or("missing original probe")?);
        let first = self.observe(&go, &request, search, debug)?;
        let second = self.observe(&go, &request, search, debug)?;
        if first != second {
            return Err(format!("original instability: {request}").into());
        }
        let native = self.observe(
            Path::new(env!("CARGO_BIN_EXE_bootstrap-probe")),
            &request,
            search,
            debug,
        )?;
        let summary = |v: &[u8]| {
            format!(
                "{} bytes: {}",
                v.len(),
                String::from_utf8_lossy(&v[..v.len().min(300)])
            )
        };
        if native != first {
            return Err(format!(
                "{request}; PATH={search:?}; GODEBUG={debug:?}\nRust {}\nGo {}",
                summary(&native),
                summary(&first)
            )
            .into());
        }
        Ok(())
    }
    fn finish(self, result: Result<()>) -> Result<()> {
        let cleanup = fs::remove_dir_all(&self.0);
        cleanup?;
        result
    }
}

#[test]
fn main_broken_standard_pipes_match_go() -> Result<()> {
    use std::os::unix::process::ExitStatusExt;
    let f = Fixture::new()?;
    let result = (|| {
        f.write("repo/bin/cron-person", b"#!/bin/sh\nprintf 'v1\\n'\n")?;
        f.write("commands/dotfiles", b"#!/bin/sh\nif [ \"$4\" = '--help' ]; then printf '%s' '--bin'; elif [ \"$2\" = 'which' ]; then printf '%s\\n' \"$PWD\"; fi\n")?;
        let go =
            PathBuf::from(std::env::var_os("BOOTSTRAP_GO_MAIN").ok_or("missing original main")?);
        for broken in ["stdout", "stderr"] {
            let observe = |binary: &Path| -> Result<_> {
                let mut command = Command::new(binary);
                command
                    .env_clear()
                    .env("HOME", f.0.join("home"))
                    .env("PATH", "../commands")
                    .env("GODEBUG", "execerrdot=0")
                    .current_dir(f.0.join("repo"))
                    .stdout(Stdio::piped())
                    .stderr(Stdio::piped());
                if broken == "stdout" {
                    command.env("dotfiles_dir", "../commands");
                }
                let mut child = command.spawn()?;
                if broken == "stdout" {
                    drop(child.stdout.take());
                } else {
                    drop(child.stderr.take());
                }
                let status = child.wait_with_output()?.status;
                Ok((status.code(), status.signal()))
            };
            let first = observe(&go)?;
            let second = observe(&go)?;
            let native = observe(Path::new(env!("CARGO_BIN_EXE_dev-bootstrap")))?;
            if first != second || first != native {
                return Err(format!(
                    "closed {broken}: original {first:?}, repeated {second:?}, Rust {native:?}"
                )
                .into());
            }
        }
        Ok(())
    })();
    f.finish(result)
}
fn request(executable: &str, directory: &str) -> Value {
    json!({"Op":"command", "Directory":directory, "Executable":executable, "Args":[], "Source":"", "Dotfiles":"", "RemoveCwd":false})
}

#[test]
fn command_lookup_quoting_and_failure_precedence() -> Result<()> {
    let f = Fixture::new()?;
    let result = (|| {
        for name in [
            "",
            "a\"\\\x07\x08\x0c\n\r\t\x0b\x01\x7f",
            "snow雪\u{a0}\u{2028}\u{200b}\u{e000}\u{100000}",
        ] {
            f.compare(request(name, ""), "", "")?;
        }
        f.write("commands/tool", b"#!/bin/sh\nprintf ' valid \\n'\n")?;
        f.write("repo/tool", b"#!/bin/sh\nprintf ' local \\n'\n")?;
        f.write("child/tool", b"#!/bin/sh\nprintf ' child \\n'\n")?;
        // Lookup happens before chdir, but a permitted relative result is executed after chdir.
        for debug in [
            "",
            "execerrdot=0",
            "execerrdot=1,execerrdot=0",
            "execerrdot=0,execerrdot=1",
        ] {
            for search in ["../commands", ":../commands", "", "../commands:."] {
                f.compare(request("tool", "../child"), search, debug)?;
            }
        }
        f.compare(request("./tool", "missing"), "", "")?;
        f.compare(request("./tool", "tool"), "", "")?;
        f.compare(request("bad\0program", "missing"), "", "")?;
        f.compare(request("./bad\0program", "missing"), "", "")?;
        f.compare(request("missing", ""), "<unset>", "")?;
        Ok(())
    })();
    f.finish(result)
}

#[test]
fn bounded_stderr_signal_and_raw_path_errors() -> Result<()> {
    let f = Fixture::new()?;
    let result = (|| {
        for size in [32768, 65536, 65537, 131073] {
            let mut script = b"#!/bin/sh\nprintf '%s' '".to_vec();
            script.extend(vec![b'x'; size]);
            script.extend(b"' >&2\nexit 7\n");
            f.write("repo/tool", &script)?;
            f.compare(request("./tool", ""), "", "")?;
        }
        // Terminating signals only. Stopping a child would require unrelated job control.
        for signal in [
            "HUP", "INT", "QUIT", "ILL", "ABRT", "FPE", "KILL", "SEGV", "USR1", "USR2", "PIPE",
            "ALRM", "TERM",
        ] {
            f.write(
                "repo/tool",
                format!("#!/bin/sh\nulimit -c 0\nkill -{signal} $$\n").as_bytes(),
            )?;
            f.compare(request("./tool", ""), "", "")?;
        }
        #[cfg(target_os = "linux")]
        {
            f.write("repo/tool", b"#!/bin/sh\nkill -34 $$\n")?;
            f.compare(request("./tool", ""), "", "")?;
        }
        f.compare(request("./missing", ""), "", "")?;
        Ok(())
    })();
    f.finish(result)
}

#[test]
fn bootstrap_preserves_non_utf8_and_symbolic_working_paths() -> Result<()> {
    let f = Fixture::new()?;
    let result = (|| {
        f.write("commands/dotfiles", b"#!/bin/sh\nif [ \"$4\" = '--help' ]; then printf '%s' '--bin'; elif [ \"$2\" = 'which' ]; then printf '%s\\n' \"$PWD\"; fi\n")?;
        f.write("repo/tool", b"#!/bin/sh\nprintf 'v1\\n'\n")?;
        for source in ["tool", "missing"] {
            let mut req = request("", "");
            req["Op"] = json!("bootstrap");
            req["Source"] = json!(source);
            req["Dotfiles"] = json!("../commands");
            f.compare(req, "../commands", "execerrdot=0")?;
        }
        Ok(())
    })();
    f.finish(result)
}
