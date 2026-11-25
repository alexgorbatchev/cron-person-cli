//! Test-only scalar adapters around the native byte APIs.
#![forbid(unsafe_code)]
use crate::native;

pub fn text(operation: String, existing: String, managed: String) -> Result<String, String> {
    let existing = hex::decode(existing).map_err(|_| "invalid existing hex")?;
    let managed = hex::decode(managed).map_err(|_| "invalid managed hex")?;
    let result = match operation.as_str() {
        "remove" => native::remove_block(&existing),
        "merge" => native::merge_crontab(&existing, &managed),
        _ => return Err("unknown text operation".to_owned()),
    };
    result.map(hex::encode).map_err(|e| e.to_string())
}

#[derive(serde::Deserialize, Default)]
#[serde(default)]
struct Request {
    operation: String,
    mode: String,
    stdout: String,
    stderr: String,
    content: String,
    status: i64,
}

pub fn process(input: String) -> Result<String, String> {
    let request: Request = serde_json::from_str(&input).map_err(|_| "invalid process request")?;
    if !["read", "write"].contains(&request.operation.as_str()) {
        return Err("unknown process operation".to_owned());
    }
    if !(0..=255).contains(&request.status) {
        return Err("invalid process status".to_owned());
    }
    #[cfg(windows)]
    {
        return Err("process fixtures require Unix".to_owned());
    }
    #[cfg(unix)]
    fixture(request).map_err(|e| e.to_string())
}

#[cfg(unix)]
fn fixture(req: Request) -> Result<String, Box<dyn std::error::Error>> {
    use std::{collections::BTreeMap, ffi::OsString, fs, os::unix::fs::PermissionsExt};
    let stdout = hex::decode(&req.stdout).map_err(|_| "invalid stdout hex")?;
    let stderr = hex::decode(&req.stderr).map_err(|_| "invalid stderr hex")?;
    let content = hex::decode(&req.content).map_err(|_| "invalid content hex")?;
    if ![
        "normal",
        "missing",
        "denied",
        "format",
        "interpreter",
        "relative",
        "closed",
        "signal",
    ]
    .contains(&req.mode.as_str())
    {
        return Err("unknown process mode".into());
    }
    let dir = tempfile::Builder::new().prefix("crontab-rust-").tempdir()?;
    let mut script = format!(
        "#!/bin/sh\nprintf '%s\\n' \"$#\" \"$@\" > calls\nif [ \"$1\" = '-' ]; then /bin/cat > installed; fi\nprintf '%b' '{}'\nprintf '%b' '{}' >&2\nexit {}\n",
        octal(&stdout),
        octal(&stderr),
        req.status
    );
    match req.mode.as_str() {
        "closed" => script = script.replace("/bin/cat > installed", "exec 0<&-"),
        "signal" => script = script.replace(&format!("exit {}", req.status), "kill -TERM $$"),
        "format" => script = "invalid executable\n".into(),
        "interpreter" => script = "#!/nonexistent-crontab-interpreter\n".into(),
        _ => (),
    }
    if req.mode != "missing" {
        let executable = dir.path().join("crontab");
        fs::write(&executable, script)?;
        fs::set_permissions(
            executable,
            fs::Permissions::from_mode(if req.mode == "denied" { 0o600 } else { 0o700 }),
        )?;
    }
    let path = if req.mode == "relative" {
        OsString::from(".")
    } else {
        dir.path().as_os_str().to_owned()
    };
    let crontab = native::SystemCrontab {
        environment: BTreeMap::from([("PATH".into(), path), ("HOME".into(), dir.path().into())]),
        directory: dir.path().into(),
    };
    let result = if req.operation == "read" {
        crontab.read()
    } else {
        crontab.write(&content).map(|_| Vec::new())
    };
    let (data, error) = match result {
        Ok(data) => (data, Vec::new()),
        Err(error) => (
            Vec::new(),
            replace_prefix(&error.bytes, dir.path().as_os_str().as_encoded_bytes()),
        ),
    };
    let mut files = Vec::new();
    for name in ["calls", "installed"] {
        let data = match fs::read(dir.path().join(name)) {
            Ok(data) => data,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Vec::new(),
            Err(e) => return Err(e.into()),
        };
        files.push(hex::encode(data));
    }
    Ok(serde_json::to_string(&[
        hex::encode(data),
        hex::encode(error),
        files[0].clone(),
        files[1].clone(),
    ])?)
}

#[cfg(unix)]
fn octal(data: &[u8]) -> String {
    data.iter().map(|b| format!("\\{b:03o}")).collect()
}
#[cfg(unix)]
fn replace_prefix(bytes: &[u8], prefix: &[u8]) -> Vec<u8> {
    let mut output = Vec::new();
    let mut remaining = bytes;
    while let Some(offset) = remaining.windows(prefix.len()).position(|w| w == prefix) {
        output.extend_from_slice(&remaining[..offset]);
        output.extend_from_slice(b"<fixture>");
        remaining = &remaining[offset + prefix.len()..];
    }
    output.extend_from_slice(remaining);
    output
}
