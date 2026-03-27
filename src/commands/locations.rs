use crate::commands::{check_api_error, make_client, network_err};
use crate::error::CliError;
use crate::params::{self, Param};
use serde_json::Value;

/// Search the SerpApi locations index using the provided parameters.
pub async fn run(params: Vec<Param>) -> Result<Value, CliError> {
    let params_map = params::params_to_hashmap(params);
    // Locations endpoint is public – no API key needed.
    let client = make_client(None)?;
    let result = client.location(params_map).await.map_err(network_err)?;
    check_api_error(result)
}
