use clap::Parser;
use serpapi_cli::{commands, config, error, jq, output, params};

#[derive(Parser, Debug)]
#[command(
    author,
    version,
    about = "The official CLI client for SerpApi",
    infer_subcommands = true
)]
struct Cli {
    /// API key. Clap also reads this from the SERPAPI_KEY env var automatically.
    #[arg(long, env = "SERPAPI_KEY", hide_env_values = true)]
    api_key: Option<String>,

    /// Plain JSON output without ANSI color (for AI agents and pipelines)
    #[arg(long)]
    json: bool,

    /// Server-side field filtering (SerpApi json_restrictor parameter)
    #[arg(long)]
    fields: Option<String>,

    /// Client-side jq filter applied to JSON output (like gh --jq)
    #[arg(long)]
    jq: Option<String>,

    #[command(subcommand)]
    command: Command,
}

#[derive(clap::Subcommand, Debug)]
enum Command {
    Search {
        params: Vec<String>,
        /// Fetch all pages and merge array results
        #[arg(long)]
        all_pages: bool,
        /// Maximum number of pages to fetch when paginating with --all-pages (default: unlimited)
        #[arg(long)]
        max_pages: Option<usize>,
    },
    Account,
    Locations {
        params: Vec<String>,
    },
    Archive {
        id: String,
    },
    Login,
}

/// Print a structured error to stderr and exit with the appropriate code.
fn die(e: error::CliError) -> ! {
    error::print_error(&e);
    std::process::exit(error::exit_code(&e));
}

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let cli = Cli::parse();

    // clap already merges --api-key and SERPAPI_KEY into cli.api_key.
    // resolve_api_key falls back to the saved config file if neither is set.
    let resolve_api_key = || config::resolve_api_key(cli.api_key.as_deref());

    let result = match cli.command {
        Command::Search {
            params,
            all_pages,
            max_pages,
        } => {
            let api_key = resolve_api_key().unwrap_or_else(|e| die(e));
            let parsed_params = params
                .iter()
                .map(|s| s.parse::<params::Param>())
                .collect::<Result<Vec<_>, _>>()
                .unwrap_or_else(|e| die(e));
            commands::search::run(
                parsed_params,
                &api_key,
                cli.fields.as_deref(),
                all_pages,
                max_pages,
            )
            .await
        }
        Command::Account => {
            let api_key = resolve_api_key().unwrap_or_else(|e| die(e));
            commands::account::run(&api_key).await
        }
        Command::Locations { params } => {
            let parsed_params = params
                .iter()
                .map(|s| s.parse::<params::Param>())
                .collect::<Result<Vec<_>, _>>()
                .unwrap_or_else(|e| die(e));
            commands::locations::run(parsed_params).await
        }
        Command::Archive { id } => {
            let api_key = resolve_api_key().unwrap_or_else(|e| die(e));
            commands::archive::run(&id, &api_key).await
        }
        Command::Login => {
            if let Err(e) = commands::login::run().await {
                die(e);
            }
            return;
        }
    };

    match result {
        Ok(value) => {
            if let Some(expr) = cli.jq.as_deref() {
                let results = jq::apply(expr, value).unwrap_or_else(|e| {
                    die(error::CliError::UsageError {
                        message: e.to_string(),
                    })
                });
                let stdout = std::io::stdout();
                let mut out = stdout.lock();
                for v in &results {
                    if let Err(e) = output::print_jq_value(v, &mut out) {
                        die(error::CliError::ApiError {
                            message: format!("Output error: {e}"),
                        });
                    }
                }
            } else if let Err(e) = output::print_json(&value, cli.json) {
                die(error::CliError::ApiError {
                    message: format!("Output error: {e}"),
                });
            }
        }
        Err(e) => die(e),
    }
}
