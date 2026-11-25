//! Shell hook generation. The optional bridge exists only for migration checks.
#[cfg(not(feature = "bridge"))]
pub mod port;

#[cfg(feature = "bridge")]
#[path = "lib.rs"]
mod bridge;
#[cfg(feature = "bridge")]
pub use bridge::*;
