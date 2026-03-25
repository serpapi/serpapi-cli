use serde_json::Value;
use serpapi::serpapi::Client;
use std::collections::HashMap;

use crate::error::CliError;

pub mod search;
pub mod account;
pub mod locations;
pub mod archive;
pub mod login;

/// The query-parameter name used to pass the SerpApi key to every request.
pub(crate) const API_KEY_PARAM: &str = "api_key";

/// Convert a `Box<dyn Error>` from the serpapi client into a [`CliError::NetworkError`].
pub(crate) fn network_err(e: Box<dyn std::error::Error>) -> CliError {
    CliError::NetworkError { message: e.to_string() }
}

/// Build a `serpapi::Client` authenticated with the given API key.
pub fn make_client(api_key: &str) -> Result<Client, CliError> {
    let params = HashMap::from([(API_KEY_PARAM.to_string(), api_key.to_string())]);
    Client::new(params).map_err(|e: Box<dyn std::error::Error>| CliError::NetworkError {
        message: e.to_string(),
    })
}

/// Inspect a successful API response and surface any embedded error field
/// as a `CliError::ApiError`, passing through all other values unchanged.
pub fn check_api_error(result: Value) -> Result<Value, CliError> {
    if let Value::Object(ref map) = result {
        if let Some(error_val) = map.get("error") {
            let message = error_val
                .as_str()
                .unwrap_or("Unknown API error")
                .to_string();
            return Err(CliError::ApiError { message });
        }
    }
    Ok(result)
}
