//! The Beskos sync engine, as a **library** -- to run inside the desktop app (Beskos.app) rather
//! than as a second application: the app hosts the engine, the tray and the UI.
//!
//! The embedding entry point is [`engine::Engine`]: it starts a background thread that syncs,
//! reacts to the filesystem watcher and publishes its state for the UI to read.

pub mod api;
pub mod config;
pub mod engine;
pub mod index;
pub mod parallel;
pub mod safepath;
pub mod sync;
pub mod throttle;
pub mod tree;
pub mod watch;
