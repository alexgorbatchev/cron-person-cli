//! Keep generated FFI exports out of Rust consumers while sharing the actual port.
#[cfg(feature = "bridge")]
#[path = "lib.rs"]
mod bridge;
#[cfg(feature = "bridge")]
pub use bridge::*;

#[cfg(not(feature = "bridge"))]
pub mod port;
