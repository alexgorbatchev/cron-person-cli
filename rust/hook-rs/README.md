`hook-rs` generates shell integration scripts for the native Rust CLI. It preserves the Go generator's output and errors, including arbitrary input bytes.

# What It Does

- **Shell scripts:** Generate bash, zsh, and fish prompt hooks.
- **Byte parity:** Preserve binary names verbatim and format unsupported shell names with Go's diagnostic quoting.
- **Independent generation:** Return script bytes without reading files, consulting authorization, or running commands.

# How It Works

Pass the shell name and binary name as byte slices. The library returns the script or an unsupported-shell error. An empty binary name selects `cron-person`.

# How it Really Works

The generator accepts only the exact shell names `bash`, `zsh`, and `fish`. Each script invokes the selected binary with `hook export <shell>` and preserves the preceding command's exit status. Binary names are inserted inside the original double quotes without further escaping, so shell expansion and embedded quoting retain the Go behavior.

Generation uses no filesystem, environment, process, network, or persistent state. Authorization warnings and `.cronrc` discovery through `HandleHookExport` are incomplete; see the [boundary inventory and integration requirements](../units/hook.md).

# Installation

This unpublished crate requires Rust 1.100 or newer. For a sibling crate in this repository's `rust/` directory, add:

```toml
[dependencies]
hook-rs = { path = "../hook-rs", default-features = false }
```

# Quick Start

```rust
use std::io::Write;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let script = hook_rs::port::generate(b"bash", b"cron-person")?;
    std::io::stdout().write_all(&script)?;
    Ok(())
}
```

# Configuration

| Option | Default | Effect |
| :--- | :--- | :--- |
| Cargo feature `bridge` | Enabled | Includes the migration JSON dispatcher, C exports, and optional serialization/hex dependencies. Disable default features for native consumers. |

# API

| Export | Signature | Description |
| :--- | :--- | :--- |
| `port::generate` | `(&[u8], &[u8]) -> Result<Vec<u8>, String>` | Generate exact script bytes or the Go-compatible unsupported-shell diagnostic. |

The [boundary inventory](../units/hook.md) records migration adapters, validation, and remaining acceptance work.

# License

[MIT](../../LICENSE).
