#![cfg(unix)]
use crontab_rs::native::{Error, SystemCrontab};
use std::{
    collections::BTreeMap,
    error::Error as _,
    ffi::OsString,
    fs,
    os::unix::{ffi::OsStringExt, fs::PermissionsExt},
    path::Path,
    sync::Mutex,
};

static FIXTURE_WRITES: Mutex<()> = Mutex::new(());

fn fixture(dir: &Path, script: &[u8]) -> Result<SystemCrontab, Box<dyn std::error::Error>> {
    let executable = dir.join("crontab");
    fs::write(&executable, script)?;
    fs::set_permissions(executable, fs::Permissions::from_mode(0o700))?;
    Ok(SystemCrontab {
        environment: BTreeMap::from([
            ("PATH".into(), dir.into()),
            ("CUSTOM_ENV".into(), "inherited value".into()),
        ]),
        directory: dir.into(),
    })
}

#[test]
fn native_roundtrip_isolated_parallel_byte_paths() -> Result<(), Box<dyn std::error::Error>> {
    let _guard = FIXTURE_WRITES
        .lock()
        .map_err(|_| "fixture mutex poisoned")?;
    let mut fixtures = Vec::new();
    for i in 0..8 {
        let dir = tempfile::tempdir()?;
        let path = dir
            .path()
            .join(OsString::from_vec(vec![b'x', b' ', b'\'', 0xff, i + 1]));
        fs::create_dir(&path)?;
        let crontab = fixture(&path, b"#!/bin/sh\nprintf '%s\\n' \"$#\" \"$@\" >> calls\nif [ \"$1\" = '-' ]; then /bin/cat > data; else /bin/cat data; fi\n")?;
        fixtures.push((dir, path, crontab, i));
    }
    std::thread::scope(|scope| {
        let workers: Vec<_> = fixtures
            .into_iter()
            .map(|(dir, path, crontab, i)| {
                scope.spawn(move || -> Result<(), String> {
                    let _keep_fixture = dir;
                    let content: Vec<u8> = (0..=255).chain([b'\r', b'\n', i]).collect();
                    crontab.write(&content).map_err(|e| e.to_string())?;
                    assert_eq!(crontab.read().map_err(|e| e.to_string())?, content);
                    assert_eq!(
                        fs::read(path.join("calls")).map_err(|e| e.to_string())?,
                        b"1\n-\n1\n-l\n"
                    );
                    Ok(())
                })
            })
            .collect();
        for worker in workers {
            worker.join().map_err(|_| "worker panicked")??;
        }
        Ok::<_, Box<dyn std::error::Error>>(())
    })
}

#[test]
fn native_environment_lookup_and_failure_sources() -> Result<(), Box<dyn std::error::Error>> {
    let _guard = FIXTURE_WRITES
        .lock()
        .map_err(|_| "fixture mutex poisoned")?;
    let dir = tempfile::tempdir()?;
    let mut crontab = fixture(dir.path(), b"#!/bin/sh\nprintf '%s' \"$CUSTOM_ENV\"\n")?;
    assert_eq!(crontab.read()?, b"inherited value");
    crontab.environment.insert("PATH".into(), ".".into());
    assert!(
        crontab
            .read()
            .err()
            .ok_or("expected ErrDot")?
            .to_string()
            .contains("relative to current directory")
    );
    crontab
        .environment
        .insert("GODEBUG".into(), "execerrdot=0,execerrdot=1".into());
    assert!(crontab.read().is_err());
    crontab
        .environment
        .insert("GODEBUG".into(), "execerrdot=1,execerrdot=0".into());
    assert_eq!(crontab.read()?, b"inherited value");
    crontab
        .environment
        .insert("GODEBUG".into(), "execerrdot=0".into());
    assert_eq!(crontab.read()?, b"inherited value");
    crontab.environment.remove(&OsString::from("PATH"));
    let err = crontab.read().err().ok_or("expected missing")?;
    assert_eq!(err.bytes, b"reading system crontab (crontab -l): exec: \"crontab\": executable file not found in $PATH: ");
    assert!(err.source().is_none());
    crontab.environment.insert("PATH".into(), dir.path().into());
    let format_dir = tempfile::tempdir()?;
    let mut format_crontab = fixture(format_dir.path(), b"not an executable\n")?;
    format_crontab.environment.insert("PATH".into(), ".".into());
    format_crontab
        .environment
        .insert("GODEBUG".into(), "execerrdot=0".into());
    let error = format_crontab
        .read()
        .err()
        .ok_or("expected exec format error")?;
    assert!(error.source().is_some());
    assert!(
        error
            .bytes
            .windows(b"fork/exec ./crontab:".len())
            .any(|part| part == b"fork/exec ./crontab:")
    );
    assert!(error.bytes.ends_with(b"exec format error: "), "{error}");
    let text: Error = "diagnostic".to_owned().into();
    assert_eq!(text.to_string(), "diagnostic");
    let inherited = SystemCrontab::inherited()?;
    assert!(inherited.directory.is_absolute()); // Construction does not execute a command.
    Ok(())
}

#[test]
fn native_pipe_pressure_and_exit_precedence() -> Result<(), Box<dyn std::error::Error>> {
    let _guard = FIXTURE_WRITES
        .lock()
        .map_err(|_| "fixture mutex poisoned")?;
    let dir = tempfile::tempdir()?;
    let crontab = fixture(dir.path(), b"#!/bin/sh\n{ /bin/dd if=/dev/zero bs=131072 count=1 2>/dev/null; } >&2\n/bin/cat > content\nprintf discarded\nexit 23\n")?;
    let error = crontab
        .write(&vec![0xff; 1048576])
        .err()
        .ok_or("expected status 23")?;
    assert!(
        error
            .bytes
            .starts_with(b"installing crontab: exit status 23: ")
    );
    assert_eq!(fs::read(dir.path().join("content"))?, vec![0xff; 1048576]);
    assert_eq!(
        error.bytes.len(),
        b"installing crontab: exit status 23: ".len() + 131072
    );
    let closed_dir = tempfile::tempdir()?;
    let crontab = fixture(
        closed_dir.path(),
        b"#!/bin/sh\nexec 0<&-\nprintf 'no crontab for test' >&2\nexit 0\n",
    )?;
    crontab.write(&vec![1; 1048576])?;
    let failed_dir = tempfile::tempdir()?;
    let crontab = fixture(
        failed_dir.path(),
        b"#!/bin/sh\nprintf 'no crontab for test' >&2\nprintf ignored\nexit 255\n",
    )?;
    assert_eq!(crontab.read()?, b"");
    assert!(crontab.write(b"input").is_err());
    Ok(())
}
