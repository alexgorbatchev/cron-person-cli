//! Native lock library; generated bridge symbols exist only for bridge consumers.
pub mod filelock;

#[cfg(feature = "bridge")]
#[path = "lib.rs"]
mod bridge;
#[cfg(feature = "bridge")]
pub use bridge::*;
