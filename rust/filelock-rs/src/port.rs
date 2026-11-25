//! Test-only data boundary; every observation comes from real filesystem locks.
use crate::filelock::{self, Error, LockFile};
use std::{fs, path::Path};

fn outcome(result: Result<(), Error>) -> String {
    match result {
        Ok(()) => "ok".into(),
        Err(e) if e.is_closed() => "file already closed".into(),
        Err(e) if e.raw_os_error().is_some() => {
            format!("os:{}", e.raw_os_error().unwrap_or_default())
        }
        Err(e) => e.to_string(),
    }
}

pub fn scenario(name: String) -> Result<String, String> {
    let text = name.starts_with("text-");
    let name = name.strip_prefix("text-").unwrap_or(&name);
    if ![
        "contention",
        "close",
        "release",
        "double-release",
        "missing-parent",
        "directory",
        "nul",
        "closed",
        "read-only",
        "retained",
        "clone",
        "non-utf8",
    ]
    .contains(&name)
    {
        return Err("unknown filelock scenario".into());
    }
    let fixture = tempfile::Builder::new()
        .prefix("filelock-rs-")
        .tempdir()
        .map_err(|e| e.to_string())?;
    if text {
        return error_text(name, fixture.path()).map_err(|e| e.to_string());
    }
    run_scenario(name, &fixture.path().join("lock")).map_err(|e| e.to_string())
}

fn error_text(name: &str, dir: &Path) -> Result<String, Box<dyn std::error::Error>> {
    let path = dir.join("lock");
    let observed = match name {
        "missing-parent" => filelock::lock_path(dir.join("missing/lock")).err(),
        "directory" => filelock::lock_path(dir).err(),
        "nul" => filelock::lock_path(dir.join("lock\0")).err(),
        "non-utf8" => {
            #[cfg(unix)]
            {
                use std::{ffi::OsString, os::unix::ffi::OsStringExt};
                filelock::lock_path(dir.join("missing").join(OsString::from_vec(vec![0xff]))).err()
            }
            #[cfg(windows)]
            return Err("requires Unix path bytes".into());
        }
        "double-release" => {
            let mut guard = filelock::lock_path(&path)?;
            guard.release()?;
            guard.release().err()
        }
        "closed" => {
            let mut file = LockFile::open(&path)?;
            file.close()?;
            filelock::lock(&file)
                .err()
                .map(|e| filelock::ReleaseError(vec![e]))
        }
        _ => return Err("unknown error-text scenario".into()),
    }
    .ok_or("expected native error")?;
    if name == "non-utf8" {
        let bytes = observed.error_bytes();
        let directory = dir.as_os_str().as_encoded_bytes();
        let position = bytes
            .windows(directory.len())
            .position(|s| s == directory)
            .ok_or("missing fixture path")?;
        let mut normalized = bytes[..position].to_vec();
        normalized.extend_from_slice(b"<fixture>");
        normalized.extend_from_slice(&bytes[position + directory.len()..]);
        return Ok(normalized.iter().map(|b| format!("{b:02x}")).collect());
    }
    Ok(observed
        .to_string()
        .replace(&dir.to_string_lossy().to_string(), "<fixture>"))
}

fn run_scenario(name: &str, path: &Path) -> Result<String, Box<dyn std::error::Error>> {
    let mut events = Vec::new();
    if ["missing-parent", "directory", "nul"].contains(&name) {
        let invalid = match name {
            "missing-parent" => path.join("missing/lock"),
            "directory" => path.parent().ok_or("missing fixture parent")?.to_path_buf(),
            _ => path.with_file_name("lock\0"),
        };
        match filelock::lock_path(invalid) {
            Ok(mut release) => {
                events.push("ok".into());
                release.release()?;
                events.push("ok".into());
            }
            Err(e) => events.extend(e.0.into_iter().map(|e| outcome(Err(e)))),
        }
    } else if ["release", "double-release", "retained"].contains(&name) {
        if name == "retained" {
            fs::write(path, "retained")?;
        }
        let mut release = filelock::lock_path(path)?;
        events.push("ok".into());
        let probe = LockFile::open(path)?;
        events.push(outcome(filelock::try_lock(&probe)));
        release.release()?;
        events.push("ok".into());
        events.push(outcome(filelock::try_lock(&probe)));
        events.push(outcome(filelock::unlock(&probe)));
        if name == "double-release"
            && let Err(e) = release.release()
        {
            events.extend(e.0.into_iter().map(|e| outcome(Err(e))));
        }
        if name == "retained" {
            events.push(fs::read_to_string(path)?);
        }
    } else {
        let mut holder = LockFile::open(path)?;
        let probe = LockFile::open(path)?;
        match name {
            "closed" => {
                events.push(outcome(holder.close()));
                events.push(outcome(filelock::lock(&holder)));
                events.push(outcome(filelock::try_lock(&holder)));
                events.push(outcome(filelock::unlock(&holder)));
            }
            "read-only" => {
                let reader = LockFile::from_file(fs::File::open(path)?, path);
                events.push(outcome(filelock::try_lock(&reader)));
                events.push(outcome(filelock::unlock(&reader)));
            }
            "clone" => {
                events.push(outcome(filelock::lock(&holder)));
                events.push(outcome(filelock::unlock(&holder)));
                events.push(outcome(filelock::try_lock(&probe)));
            }
            _ => {
                events.push(outcome(filelock::lock(&holder)));
                events.push(outcome(filelock::try_lock(&probe)));
                events.push(outcome(if name == "close" {
                    holder.close()
                } else {
                    filelock::unlock(&holder)
                }));
                events.push(outcome(filelock::try_lock(&probe)));
                events.push(outcome(filelock::try_lock(&holder)));
                events.push(outcome(filelock::unlock(&probe)));
            }
        }
    }
    Ok(serde_json::to_string(&events)?)
}
