use std::fs;
use std::path::PathBuf;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct Config {
    api_key: String,
}

pub fn config_dir() -> PathBuf {
    dirs::config_dir()
        .or_else(|| dirs::home_dir().map(|h| h.join(".config")))
        .unwrap_or_else(|| PathBuf::from(".config"))
        .join("serpapi")
}

pub fn config_path() -> PathBuf {
    config_dir().join("config.toml")
}

pub fn load_api_key() -> Option<String> {
    let content = fs::read_to_string(config_path()).ok()?;
    let config: Config = toml::from_str(&content).ok()?;
    Some(config.api_key)
}

pub fn save_config(api_key: &str) -> Result<(), crate::error::CliError> {
    let to_cli_err = |e: &dyn std::fmt::Display| crate::error::CliError::UsageError {
        message: e.to_string(),
    };

    let dir = config_dir();
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        std::fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(&dir)
            .map_err(|e| crate::error::CliError::UsageError {
                message: format!("Failed to create config dir: {e}"),
            })?;
    }
    #[cfg(not(unix))]
    {
        std::fs::create_dir_all(&dir)
            .map_err(|e| crate::error::CliError::UsageError {
                message: format!("Failed to create config dir: {e}"),
            })?;
    }

    let config = Config {
        api_key: api_key.to_string(),
    };

    let content = toml::to_string(&config).map_err(|e| to_cli_err(&e))?;
    let final_path = config_path();
    let tmp_path = final_path.with_extension("toml.tmp");

    // Create tmp file with restricted permissions from the start (no readable window).
    #[cfg(unix)]
    {
        use std::io::Write;
        use std::os::unix::fs::OpenOptionsExt;
        let mut file = fs::OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(0o600)
            .open(&tmp_path)
            .map_err(|e| to_cli_err(&e))?;
        file.write_all(content.as_bytes()).map_err(|e| to_cli_err(&e))?;
    }
    #[cfg(not(unix))]
    {
        fs::write(&tmp_path, &content).map_err(|e| to_cli_err(&e))?;
    }

    if let Err(rename_err) = std::fs::rename(&tmp_path, &final_path) {
        // Best-effort cleanup of the temp file.
        let _ = std::fs::remove_file(&tmp_path);
        return Err(crate::error::CliError::UsageError {
            message: format!("Failed to save config: {rename_err}"),
        });
    }

    Ok(())
}

/// Resolve the API key from the already-merged clap value (flag or env var), then
/// fall back to the saved config file. The env-var lookup is handled by clap upstream.
pub fn resolve_api_key(
    from_clap: Option<&str>,
) -> Result<String, crate::error::CliError> {
    if let Some(key) = from_clap {
        return Ok(key.to_string());
    }
    
    if let Some(key) = load_api_key() {
        return Ok(key);
    }
    
    Err(crate::error::CliError::UsageError {
        message: "No API key found. Run 'serpapi login' or set SERPAPI_KEY.".to_string(),
    })
}
