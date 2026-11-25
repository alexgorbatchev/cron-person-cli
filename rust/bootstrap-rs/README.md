# bootstrap-rs

Native Rust library and isolated development-bootstrap helper. Root recipes still use Go. Linux parity passes 169 genuine observations and all twelve migration gates; Windows behavior and native macOS/Windows execution remain incomplete. See the [API, validation and platform ledger](../units/bootstrap.md).

Use `bootstrap-rs` with `default-features = false` for native library/executable consumption. The default `bridge` feature enables genuine Go/Rust fixture validation and generated FFI; it is not required by the native helper.

## Functions

- `Scenario` (`rust/reference/bootstrap/bootstrap.go:84`)

## Loop

```sh
export BOOTSTRAP_EVIDENCE="$PWD/.state/bootstrap-validation"
uv run --no-project python rust/reference/bootstrap-tools.py prepare
uv run --no-project python rust/reference/bootstrap-tools.py run build rust-anything build rust/bootstrap-rs
uv run --no-project python rust/reference/bootstrap-tools.py run diff rust-anything diff rust/bootstrap-rs --json
uv run --no-project python rust/reference/bootstrap-tools.py run gate rust-anything gate rust/bootstrap-rs --json
```

Run from the repository root. Fixtures use fake tools and isolated state; they never register an actual development override. Recording is reserved for deliberate input/source changes, with two actual original runs and preservation of existing observations.
