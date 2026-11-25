use filelock_rs::filelock::{self, Error, LockFile};
#[cfg(feature = "bridge")]
use filelock_rs::port;
use std::{error::Error as _, fs};

#[test]
#[cfg(feature = "bridge")]
fn recorded_operations_are_real_and_closed_files_are_errors() {
    for name in [
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
    ] {
        let result = port::scenario(name.into()).expect(name);
        let events: Vec<String> = serde_json::from_str(&result).expect("event list");
        assert!(!events.is_empty(), "{name}");
        if name == "contention" {
            assert_eq!(
                events,
                ["ok", "file is locked", "ok", "ok", "file is locked", "ok"]
            );
        }
        if name == "retained" {
            assert_eq!(events.last().unwrap(), "retained");
        }
        if name == "double-release" {
            assert_eq!(events.len(), 7);
            assert!(events[5].starts_with("os:"));
            assert_eq!(events[6], "file already closed");
        }
    }
    assert_eq!(
        port::scenario("not-a-scenario".into()).unwrap_err(),
        "unknown filelock scenario"
    );
    for name in [
        "text-missing-parent",
        "text-directory",
        "text-nul",
        "text-closed",
        "text-double-release",
    ] {
        assert!(!port::scenario(name.into()).unwrap().is_empty());
    }
    assert_eq!(
        port::scenario("text-close".into()).unwrap_err(),
        "unknown error-text scenario"
    );
    #[cfg(unix)]
    {
        use std::{ffi::OsString, os::unix::ffi::OsStringExt};
        let dir = tempfile::tempdir().unwrap();
        let path = dir
            .path()
            .join("missing")
            .join(OsString::from_vec(vec![0xff]));
        let error = filelock::lock_path(&path).unwrap_err();
        assert!(error.error_bytes().contains(&0xff));
        assert!(error.to_string().contains('\u{fffd}'));
        assert!(
            port::scenario("text-non-utf8".into())
                .unwrap()
                .contains("ff")
        );
    }
}

#[test]
fn guard_drop_releases_and_preserves_file_identity_and_content() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("retained");
    fs::write(&path, b"unchanged").unwrap();
    let probe = LockFile::open(&path).unwrap();
    assert_eq!(probe.path(), path);
    {
        let _guard = filelock::lock_path(&path).unwrap();
        assert!(matches!(filelock::try_lock(&probe), Err(Error::Locked)));
    }
    filelock::try_lock(&probe).unwrap();
    filelock::unlock(&probe).unwrap();
    assert_eq!(fs::read(path).unwrap(), b"unchanged");
}

#[test]
fn errors_remain_typed_and_release_reports_both_failures() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("lock");
    let mut release = filelock::lock_path(&path).unwrap();
    release.release().unwrap();
    let error = release.release().unwrap_err();
    assert_eq!(error.0.len(), 2);
    assert!(error.0[0].source().is_some());
    assert!(error.0[1].is_closed());
    assert!(error.to_string().contains('\n'));
    assert_eq!(Error::Locked.to_string(), "file is locked");
    assert!(Error::Locked.source().is_none());
    assert_eq!(
        Error::from(std::io::Error::other("other")).to_string(),
        "other"
    );
}

#[cfg(unix)]
#[test]
fn clones_share_lock_ownership_and_unlock() {
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("clone");
    let mut first = LockFile::open(&path).unwrap();
    let clone = LockFile::from_file(first.file().unwrap().try_clone().unwrap(), &path);
    let probe = LockFile::open(&path).unwrap();
    filelock::lock(&first).unwrap();
    first.close().unwrap();
    assert!(matches!(filelock::try_lock(&probe), Err(Error::Locked)));
    filelock::unlock(&clone).unwrap();
    filelock::try_lock(&probe).unwrap();
}

#[cfg(target_os = "linux")]
#[test]
fn live_but_unsuitable_descriptor_returns_os_errors_not_contention() {
    use std::os::unix::fs::OpenOptionsExt;
    let dir = tempfile::tempdir().unwrap();
    let path = dir.path().join("opath");
    fs::write(&path, "").unwrap();
    let file = fs::OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_PATH)
        .open(&path)
        .unwrap();
    let file = LockFile::from_file(file, path);
    for result in [
        filelock::lock(&file),
        filelock::try_lock(&file),
        filelock::unlock(&file),
    ] {
        assert!(matches!(result, Err(Error::Os(e)) if e.raw_os_error() == Some(libc::EBADF)));
    }
}
