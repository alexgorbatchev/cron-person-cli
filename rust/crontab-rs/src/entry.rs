//! Independent native crontab text and Unix process behavior.
pub mod native;

#[cfg(feature = "bridge")]
#[path = "lib.rs"]
mod bridge;
#[cfg(feature = "bridge")]
pub use bridge::*;
