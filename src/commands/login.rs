use crate::commands::{check_api_error, make_client, network_err};
use crate::config;
use crate::error::CliError;
use std::collections::HashMap;
use std::io::{self, IsTerminal, Write};

/// Prompt the user for their SerpApi API key, verify it, and persist it to the config file.
pub async fn run() -> Result<(), CliError> {
    let api_key = if io::stdin().is_terminal() {
        rpassword::prompt_password("Enter your SerpApi API key: ").map_err(|e| {
            CliError::UsageError {
                message: format!("Failed to read input: {e}"),
            }
        })?
    } else {
        // Non-interactive (piped/CI): print prompt to stderr and read from stdin
        eprint!("Enter your SerpApi API key: ");
        io::stderr().flush().map_err(|e| CliError::UsageError {
            message: format!("Failed to flush stderr: {e}"),
        })?;
        let mut line = String::new();
        io::stdin()
            .read_line(&mut line)
            .map_err(|e| CliError::UsageError {
                message: format!("Failed to read input: {e}"),
            })?;
        line
    };
    let api_key = api_key.trim();

    if api_key.is_empty() {
        return Err(CliError::UsageError {
            message: "API key cannot be empty.".to_string(),
        });
    }

    let client = make_client(Some(api_key))?;
    let result = client.account(HashMap::new()).await.map_err(network_err)?;

    let result = check_api_error(result)?;
    let email = result
        .get("account_email")
        .and_then(|v| v.as_str())
        .unwrap_or("unknown");
    config::save_config(api_key)?;
    eprintln!(
        "Logged in as {email}. API key saved to {:?}",
        config::config_path()
    );
    Ok(())
}
