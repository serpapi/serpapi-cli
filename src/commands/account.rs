use crate::commands::{check_api_error, make_client, network_err};
use crate::error::CliError;
use serde_json::Value;
use std::collections::HashMap;

/// Fetch and return the account information for the given API key.
pub async fn run(api_key: &str) -> Result<Value, CliError> {
    let client = make_client(api_key)?;
    let result = client.account(HashMap::new()).await.map_err(network_err)?;
    check_api_error(result)
}
