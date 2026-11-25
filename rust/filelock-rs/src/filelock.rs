//! Exclusive OS-held locks, interoperable with the Go reference implementation.
use std::{
    error, fmt,
    fs::{File, OpenOptions},
    io,
    path::{Path, PathBuf},
};

/// Only `Locked` indicates contention; all other errors must reach the caller.
#[derive(Debug)]
pub enum Error {
    Locked,
    Os(io::Error),
    Closed,
    Path {
        operation: &'static str,
        path: PathBuf,
        error: Box<Error>,
    },
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&String::from_utf8_lossy(&self.error_bytes()))
    }
}
impl Error {
    /// Go error strings may contain non-UTF-8 Unix path bytes. Use this at byte
    /// output boundaries; Display necessarily replaces invalid UTF-8 characters.
    pub fn error_bytes(&self) -> Vec<u8> {
        match self {
            Self::Locked => b"file is locked".to_vec(),
            Self::Os(e) => platform::os_message(e).into_bytes(),
            Self::Closed => b"file already closed".to_vec(),
            Self::Path {
                operation,
                path,
                error,
            } => {
                let mut bytes = operation.as_bytes().to_vec();
                bytes.push(b' ');
                #[cfg(unix)]
                {
                    use std::os::unix::ffi::OsStrExt;
                    bytes.extend_from_slice(path.as_os_str().as_bytes());
                }
                #[cfg(windows)]
                bytes.extend_from_slice(path.to_string_lossy().as_bytes());
                bytes.extend_from_slice(b": ");
                bytes.extend(error.error_bytes());
                bytes
            }
        }
    }
}
impl error::Error for Error {
    fn source(&self) -> Option<&(dyn error::Error + 'static)> {
        match self {
            Self::Os(e) => Some(e),
            Self::Path { error, .. } => Some(error.as_ref()),
            _ => None,
        }
    }
}
impl Error {
    pub fn raw_os_error(&self) -> Option<i32> {
        match self {
            Self::Os(e) => e.raw_os_error(),
            Self::Path { error, .. } => error.raw_os_error(),
            _ => None,
        }
    }
    pub fn is_closed(&self) -> bool {
        match self {
            Self::Closed => true,
            Self::Path { error, .. } => error.is_closed(),
            _ => false,
        }
    }
    fn at(self, operation: &'static str, path: &Path) -> Self {
        Self::Path {
            operation,
            path: path.into(),
            error: Box::new(self),
        }
    }
}
impl From<io::Error> for Error {
    fn from(e: io::Error) -> Self {
        Self::Os(e)
    }
}

/// Owned file with an explicit, fallible close, including the closed-file state.
#[derive(Debug)]
pub struct LockFile {
    file: Option<File>,
    path: PathBuf,
}
impl LockFile {
    /// Open/create read-write without truncation, using the Go sharing contract.
    pub fn open(path: impl AsRef<Path>) -> Result<Self, Error> {
        // Go rejects embedded NUL before opening, returning syscall.EINVAL.
        if path.as_ref().as_os_str().as_encoded_bytes().contains(&0) {
            return Err(Error::Os(platform::invalid_argument()).at("open", path.as_ref()));
        }
        let mut options = OpenOptions::new();
        options.read(true).write(true).create(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600);
        }
        #[cfg(windows)]
        {
            use std::os::windows::fs::OpenOptionsExt;
            use windows_sys::Win32::Storage::FileSystem::{FILE_SHARE_READ, FILE_SHARE_WRITE};
            options.share_mode(FILE_SHARE_READ | FILE_SHARE_WRITE);
        }
        let file = options
            .open(path.as_ref())
            .map_err(|e| Error::Os(e).at("open", path.as_ref()))?;
        Ok(Self::from_file(file, path.as_ref()))
    }
    /// Adopt an already-open file; a fresh open must be used for liveness probes.
    pub fn from_file(file: File, path: impl Into<PathBuf>) -> Self {
        Self {
            file: Some(file),
            path: path.into(),
        }
    }
    pub fn path(&self) -> &Path {
        &self.path
    }
    pub fn file(&self) -> Result<&File, Error> {
        self.file
            .as_ref()
            .ok_or_else(|| Error::Os(platform::bad_handle()))
    }
    /// Close once, surfacing errors rather than losing them in `File::drop`.
    pub fn close(&mut self) -> Result<(), Error> {
        let result = match self.file.take() {
            Some(file) => platform::close(file).map_err(Error::Os),
            None => Err(Error::Closed),
        };
        result.map_err(|e| e.at("close", &self.path))
    }
}
pub fn lock(file: &LockFile) -> Result<(), Error> {
    platform::lock(file.file()?, false)
}
pub fn try_lock(file: &LockFile) -> Result<(), Error> {
    platform::lock(file.file()?, true)
}
pub fn unlock(file: &LockFile) -> Result<(), Error> {
    platform::unlock(file.file()?).map_err(Into::into)
}

/// All unlock and close failures, in that order, analogous to Go errors.Join.
#[derive(Debug)]
pub struct ReleaseError(pub Vec<Error>);
impl ReleaseError {
    /// Preserve every joined native error, including non-UTF-8 Unix path bytes.
    pub fn error_bytes(&self) -> Vec<u8> {
        self.0
            .iter()
            .map(Error::error_bytes)
            .collect::<Vec<_>>()
            .join(&b'\n')
    }
}
impl fmt::Display for ReleaseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for (i, error) in self.0.iter().enumerate() {
            if i != 0 {
                f.write_str("\n")?;
            }
            error.fmt(f)?;
        }
        Ok(())
    }
}
impl error::Error for ReleaseError {}
#[derive(Debug)]
pub struct PathLock {
    file: LockFile,
}
impl PathLock {
    /// Explicit unlock precedes close even if unlock fails. The pathname remains.
    pub fn release(&mut self) -> Result<(), ReleaseError> {
        join(unlock(&self.file), self.file.close())
    }
}
impl Drop for PathLock {
    fn drop(&mut self) {
        if self.file.file.is_some() {
            let _ = self.release();
        }
    }
}
fn join(first: Result<(), Error>, second: Result<(), Error>) -> Result<(), ReleaseError> {
    let errors: Vec<_> = first.err().into_iter().chain(second.err()).collect();
    if errors.is_empty() {
        Ok(())
    } else {
        Err(ReleaseError(errors))
    }
}
pub fn lock_path(path: impl AsRef<Path>) -> Result<PathLock, ReleaseError> {
    let mut file = LockFile::open(path).map_err(|e| ReleaseError(vec![e]))?;
    if let Err(error) = lock(&file) {
        join(Err(error), file.close())?;
    }
    Ok(PathLock { file })
}

#[cfg(unix)]
mod platform {
    use super::*;
    use std::{fs::TryLockError, os::fd::IntoRawFd};
    pub fn os_message(error: &io::Error) -> String {
        let text = error.to_string();
        let Some(code) = error.raw_os_error() else {
            return text;
        };
        let text = text
            .strip_suffix(&format!(" (os error {code})"))
            .unwrap_or(&text);
        let mut bytes = text.as_bytes().to_vec();
        if let Some(first) = bytes.first_mut() {
            first.make_ascii_lowercase();
        }
        String::from_utf8_lossy(&bytes).into_owned()
    }
    pub fn invalid_argument() -> io::Error {
        io::Error::from_raw_os_error(libc::EINVAL)
    }
    pub fn bad_handle() -> io::Error {
        io::Error::from_raw_os_error(libc::EBADF)
    }
    pub fn lock(file: &File, nonblocking: bool) -> Result<(), Error> {
        if !nonblocking {
            return file.lock().map_err(Into::into);
        }
        match file.try_lock() {
            Ok(()) => Ok(()),
            Err(TryLockError::WouldBlock) => Err(Error::Locked),
            Err(TryLockError::Error(e)) => Err(e.into()),
        }
    }
    pub fn unlock(file: &File) -> io::Result<()> {
        file.unlock()
    }
    pub fn close(file: File) -> io::Result<()> {
        // SAFETY: ownership is transferred out of File; close is called exactly once.
        // Never retry EINTR: the descriptor may already have been reused.
        if unsafe { libc::close(file.into_raw_fd()) } == 0 {
            Ok(())
        } else {
            Err(io::Error::last_os_error())
        }
    }
}

#[cfg(windows)]
mod platform {
    use super::*;
    use std::os::windows::io::{AsRawHandle, IntoRawHandle};
    pub fn os_message(error: &io::Error) -> String {
        use windows_sys::Win32::System::Diagnostics::Debug::{
            FORMAT_MESSAGE_ARGUMENT_ARRAY, FORMAT_MESSAGE_FROM_SYSTEM,
            FORMAT_MESSAGE_IGNORE_INSERTS, FormatMessageW,
        };
        let Some(code) = error.raw_os_error() else {
            return error.to_string();
        };
        if code == (1 << 29) + 39 {
            return "invalid argument".into();
        }
        let flags = FORMAT_MESSAGE_FROM_SYSTEM
            | FORMAT_MESSAGE_ARGUMENT_ARRAY
            | FORMAT_MESSAGE_IGNORE_INSERTS;
        let mut buffer = [0u16; 300];
        for language in [0x409, 0] {
            // SAFETY: writable fixed buffer, null source and arguments with IGNORE_INSERTS,
            // matching Go's English-first FormatMessageW fallback contract.
            let count = unsafe {
                FormatMessageW(
                    flags,
                    std::ptr::null(),
                    code as u32,
                    language,
                    buffer.as_mut_ptr(),
                    buffer.len() as u32,
                    std::ptr::null(),
                )
            };
            if count != 0 {
                return String::from_utf16_lossy(&buffer[..count as usize])
                    .trim_end_matches(['\r', '\n'])
                    .to_string();
            }
        }
        format!("winapi error #{code}")
    }
    // Go syscall/zerrors_windows.go: APPLICATION_ERROR + EINVAL's iota (39).
    pub fn invalid_argument() -> io::Error {
        io::Error::from_raw_os_error((1 << 29) + 39)
    }
    use windows_sys::Win32::{
        Foundation::{CloseHandle, ERROR_INVALID_HANDLE, ERROR_LOCK_VIOLATION},
        Storage::FileSystem::{
            LOCKFILE_EXCLUSIVE_LOCK, LOCKFILE_FAIL_IMMEDIATELY, LockFileEx, UnlockFileEx,
        },
        System::IO::OVERLAPPED,
    };
    pub fn bad_handle() -> io::Error {
        io::Error::from_raw_os_error(ERROR_INVALID_HANDLE as i32)
    }
    pub fn lock(file: &File, nonblocking: bool) -> Result<(), Error> {
        let flags = LOCKFILE_EXCLUSIVE_LOCK
            | if nonblocking {
                LOCKFILE_FAIL_IMMEDIATELY
            } else {
                0
            };
        let mut overlapped = OVERLAPPED::default();
        // SAFETY: live synchronous handle and valid zero-offset OVERLAPPED, covering
        // exactly byte 0 with length 1, matching the Go reference.
        if unsafe { LockFileEx(file.as_raw_handle(), flags, 0, 1, 0, &mut overlapped) } != 0 {
            return Ok(());
        }
        let error = io::Error::last_os_error();
        if nonblocking && error.raw_os_error() == Some(ERROR_LOCK_VIOLATION as i32) {
            Err(Error::Locked)
        } else {
            Err(error.into())
        }
    }
    pub fn unlock(file: &File) -> io::Result<()> {
        let mut overlapped = OVERLAPPED::default();
        // SAFETY: live handle and matching one-byte range.
        if unsafe { UnlockFileEx(file.as_raw_handle(), 0, 1, 0, &mut overlapped) } != 0 {
            Ok(())
        } else {
            Err(io::Error::last_os_error())
        }
    }
    pub fn close(file: File) -> io::Result<()> {
        // SAFETY: consuming File transfers the sole responsibility to close this handle.
        if unsafe { CloseHandle(file.into_raw_handle()) } != 0 {
            Ok(())
        } else {
            Err(io::Error::last_os_error())
        }
    }
}
