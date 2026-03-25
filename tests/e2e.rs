use assert_cmd::cargo_bin_cmd;
use predicates::prelude::*;
use std::env;

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_search_basic() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("search")
        .arg("engine=google")
        .arg("q=coffee")
        .assert()
        .success()
        .stdout(predicate::str::contains("search_metadata"));
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_search_with_fields() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("--fields")
        .arg("organic_results[0:2]")
        .arg("search")
        .arg("engine=google")
        .arg("q=coffee")
        .assert()
        .success()
        .stdout(predicate::str::contains("[").or(predicate::str::contains("{")));
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_search_with_jq() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("--jq")
        .arg(".search_metadata.id")
        .arg("search")
        .arg("engine=google")
        .arg("q=coffee")
        .assert()
        .success()
        // --jq uses raw scalar output (no quotes on strings), so just verify non-empty
        .stdout(predicate::str::is_empty().not());
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_account() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("account")
        .assert()
        .success()
        .stdout(predicate::str::contains("account_email").or(predicate::str::contains("plan")));
}

#[test]
#[ignore = "requires live SerpAPI locations API (network access)"]
fn test_locations() {
    cargo_bin_cmd!("serpapi")
        .arg("--json")
        .arg("locations")
        .arg("q=austin")
        .arg("num=3")
        .assert()
        .success()
        .stdout(predicate::str::contains("[").or(predicate::str::contains("canonical_name")));
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_archive() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");

    let search_output = cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("search")
        .arg("engine=google")
        .arg("q=rust")
        .output()
        .unwrap();

    if search_output.status.success() {
        let output_str = String::from_utf8_lossy(&search_output.stdout);
        if let Ok(json) = serde_json::from_str::<serde_json::Value>(&output_str) {
            let search_id = json
                .get("search_metadata")
                .and_then(|m| m.get("id"))
                .and_then(|id| id.as_str())
                .unwrap_or("");
            if !search_id.is_empty() {
                cargo_bin_cmd!("serpapi")
                    .arg("--api-key")
                    .arg(&api_key)
                    .arg("--json")
                    .arg("archive")
                    .arg(search_id)
                    .assert()
                    .success()
                    .stdout(
                        predicate::str::contains("search_metadata")
                            .or(predicate::str::contains("search_parameters")),
                    );
            }
        }
    }
}

#[test]
#[ignore = "requires SERPAPI_KEY"]
fn test_search_all_pages() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg(&api_key)
        .arg("--json")
        .arg("search")
        .arg("engine=google")
        .arg("q=coffee")
        .arg("num=1")
        .arg("--all-pages")
        .arg("--max-pages").arg("2")
        .assert()
        .success()
        .stdout(predicate::str::contains("organic_results"));
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_invalid_api_key() {
    cargo_bin_cmd!("serpapi")
        .arg("--api-key")
        .arg("invalid")
        .arg("--json")
        .arg("search")
        .arg("engine=google")
        .arg("q=test")
        .assert()
        .failure()
        .code(predicate::eq(1));
}

#[test]
fn test_no_args() {
    cargo_bin_cmd!("serpapi")
        .assert()
        .failure()
        .code(predicate::eq(2));
}

#[test]
#[ignore = "requires live SERPAPI_KEY env var"]
fn test_login_flow() {
    let api_key = env::var("SERPAPI_KEY").expect("SERPAPI_KEY not set");
    let config_dir = serpapi_cli::config::config_dir();

    let _ = std::fs::remove_file(config_dir.join("config.toml"));

    cargo_bin_cmd!("serpapi")
        .arg("login")
        .write_stdin(format!("{}\n", api_key))
        .assert()
        .success();

    assert!(
        config_dir.join("config.toml").exists(),
        "Config file should be created after login"
    );
}
