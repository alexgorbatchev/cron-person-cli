//! Test-only scalar protocol for the actual bootstrap APIs; no fixture logic lives here.
use bootstrap_rs::bootstrap;
use std::{
    ffi::OsString,
    io::{self, Write},
    path::Path,
};
#[derive(serde::Deserialize)]
#[serde(rename_all = "PascalCase")]
struct Request {
    op: String,
    directory: String,
    executable: String,
    args: Option<Vec<String>>,
    source: String,
    dotfiles: String,
    remove_cwd: bool,
}
fn probe() -> Result<(), Box<dyn std::error::Error>> {
    let req: Request = serde_json::from_reader(io::stdin())?;
    if req.remove_cwd {
        std::fs::remove_dir(std::env::current_dir()?)?;
    }
    let result = match req.op.as_str() {
        "command" => bootstrap::command_output(
            Path::new(&req.directory),
            req.executable.as_ref(),
            &req.args
                .unwrap_or_default()
                .into_iter()
                .map(OsString::from)
                .collect::<Vec<_>>(),
        ),
        "bootstrap" => bootstrap::bootstrap(Path::new(&req.source), Path::new(&req.dotfiles)),
        "run" => bootstrap::run(&mut io::stdout()).map(|()| Vec::new()),
        _ => return Err("invalid operation".into()),
    };
    let (value, error) = match result {
        Ok(value) => (value, Vec::new()),
        Err(error) => (Vec::new(), error.0),
    };
    writeln!(
        io::stdout(),
        "{}",
        serde_json::to_string(&[hex::encode(value), hex::encode(error)])?
    )?;
    Ok(())
}
fn main() {
    if std::env::var_os("BOOTSTRAP_PROBE").is_none() {
        std::process::exit(bootstrap::process_main());
    }
    if let Err(error) = probe() {
        eprintln!("{error}");
        std::process::exit(2);
    }
}
