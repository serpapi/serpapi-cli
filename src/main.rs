use std::collections::HashMap;
use std::str::FromStr;

use clap::Parser;
use color_eyre::Result;
use colored_json::to_colored_json_auto;
use html2text::render::text_renderer::RichAnnotation;
use serpapi::Client;

#[derive(Parser, Debug)]
#[command(
    author,
    version,
    about = "The official CLI client for SerpApi",
    infer_subcommands = true
)]
struct Cli {
    /// Your private SerpApi API key
    #[arg(long, env = "SERPAPI_KEY")]
    api_key: String,
    // We'll add XML, and maybe other formats - human readable for instance?
    #[arg(long, conflicts_with_all = ["html"])]
    json: bool,
    #[arg(long, conflicts_with_all = ["json"])]
    html: bool,
    /// Verbose output, specify more than once for more
    #[arg(long, short, action = clap::ArgAction::Count)]
    verbose: u8,
    #[command(subcommand)]
    command: Command,
}

#[derive(Parser, Debug)]
enum Command {
    /// Perform a search
    Search(Search),
    /// Retrieve a previously made search
    Archive(ArchiveLookup),
    /// Perform a SerpApi location API lookup
    Location(LocationLookup),
    /// Retrieve SerpApi account data
    Account,
    /// Search SerpApi documentation
    Docs(Docs),
}

#[derive(Parser, Debug)]
struct Search {
    /// A key=value parameter pair, or a value for the q parameter
    params: Vec<Param>,
}

#[derive(Parser, Debug)]
struct LocationLookup {
    /// Search for the given location
    location: String,
}

#[derive(Parser, Debug)]
struct ArchiveLookup {
    /// Retrieve the given search by ID
    id: String,
}

#[derive(Parser, Debug)]
struct AccountLookup;

#[derive(Parser, Debug)]
struct Docs {
    query: String,
}

#[derive(Debug, Clone)]
struct Param {
    key: String,
    value: String,
}

impl FromStr for Param {
    type Err = &'static str;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let mut parts = s.splitn(2, '=');
        match (parts.next(), parts.next()) {
            (Some(key), Some(value)) => Ok(Self {
                key: key.to_string(),
                value: value.to_string(),
            }),
            (Some(value), None) => Ok(Self {
                key: "q".to_string(),
                value: value.to_string(),
            }),
            _ => Err("Empty parameter"),
        }
    }
}

// from rust-html2text
fn default_colour_map(annotation: &RichAnnotation) -> (String, String) {
    use termion::color::*;
    use RichAnnotation::*;
    match annotation {
        Default => ("".into(), "".into()),
        Link(_) => (
            format!("{}", termion::style::Underline),
            format!("{}", termion::style::Reset),
        ),
        Image(_) => (format!("{}", Fg(Blue)), format!("{}", Fg(Reset))),
        Emphasis => (
            format!("{}", termion::style::Bold),
            format!("{}", termion::style::Reset),
        ),
        Strong => (format!("{}", Fg(LightYellow)), format!("{}", Fg(Reset))),
        Strikeout => (format!("{}", Fg(LightBlack)), format!("{}", Fg(Reset))),
        Code => (format!("{}", Fg(Blue)), format!("{}", Fg(Reset))),
        Preformat(_) => (format!("{}", Fg(Blue)), format!("{}", Fg(Reset))),
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args = Cli::parse();
    let client = Client::new(HashMap::from([(
        "api_key".to_string(),
        args.api_key.clone(),
    )]));

    match args.command {
        Command::Search(search) => {
            let params = search
                .params
                .iter()
                .map(|param| (param.key.to_string(), param.value.to_string()))
                .collect::<HashMap<_, _>>();

            if args.html {
                let result = client.html(params).await?;
                println!("{}", result);
            } else {
                let result = client.search(params).await?;
                println!("{}", to_colored_json_auto(&result)?);
            }
        }
        Command::Location(location) => {
            let result = client
                .location(HashMap::from([(
                    "q".to_string(),
                    location.location.clone(),
                )]))
                .await
                .unwrap();
            println!("{}", to_colored_json_auto(&result)?);
        }
        Command::Archive(lookup) => {
            client.search_archive(&lookup.id).await?;
        }
        Command::Account => {
            let result = client.account(HashMap::new()).await?;
            println!("{}", to_colored_json_auto(&result)?);
        }
        Command::Docs(query) => {
            let body = reqwest::get(format!("https://serpapi.com/{}", query.query))
                .await?
                .bytes()
                .await?;

            println!(
                "{}",
                html2text::from_read_coloured(&body[..], 80, default_colour_map)?
            );
        }
    }

    Ok(())
}
