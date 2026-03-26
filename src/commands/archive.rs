use crate::commands::{check_api_error, make_client, network_err};
use crate::error::CliError;
use serde_json::Value;

/// Retrieve a previously cached search result from the SerpApi archive by its ID.
pub async fn run(id: &str, api_key: &str) -> Result<Value, CliError> {
    let client = make_client(api_key)?;
    let result = client.search_archive(id).await.map_err(network_err)?;
    check_api_error(result)
}
