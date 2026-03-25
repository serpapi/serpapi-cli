//! `serpapi-cli` — library interface for the SerpApi CLI.
//!
//! Exposes the core utility modules used by the CLI binary so they can be used
//! in integration tests and by library consumers.
//!
//! # Modules
//! - [`config`] — API key resolution and config file management
//! - [`error`] — CLI error types (`CliError`) and exit code mapping
//! - [`output`] — JSON printing helpers for terminal and pipeline output
//! - [`params`] — Query parameter parsing (`key=value` syntax)
//! - [`jq`] — Client-side jq filtering via the `jaq` library

pub mod commands;
pub mod config;
pub mod error;
pub mod output;
pub mod params;
pub mod jq;
