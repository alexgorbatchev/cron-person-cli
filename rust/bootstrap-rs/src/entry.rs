//! Bootstrap implementation; generated FFI and fixture support are optional.
pub mod bootstrap;
#[cfg(feature = "bridge")]
#[path = "lib.rs"]
mod bridge;
mod quote;
#[cfg(feature = "bridge")]
pub use bridge::*;
