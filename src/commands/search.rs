use crate::commands::{check_api_error, make_client, network_err, API_KEY_PARAM};
use crate::error::CliError;
use crate::params::{self, Param};
use serde_json::Value;
use std::collections::{HashMap, HashSet};
use std::time::Duration;
use url::Url;

/// Execute a SerpApi search, optionally accumulating all pages into a single result.
pub async fn run(
    params: Vec<Param>,
    api_key: Option<&str>,
    fields: Option<&str>,
    all_pages: bool,
    max_pages: Option<usize>,
) -> Result<Value, CliError> {
    let mut params_map = params::params_to_hashmap(params);
    params::apply_fields(&mut params_map, fields);

    if !all_pages && max_pages.is_none() {
        let client = make_client(api_key)?;
        let result = tokio::time::timeout(Duration::from_secs(30), client.search(params_map))
            .await
            .map_err(|_| CliError::NetworkError {
                message: "Request timed out after 30s".to_string(),
            })?
            .map_err(network_err)?;
        return check_api_error(result);
    }

    // Ensure api_key is in the initial params map so it survives page transitions.
    if let Some(key) = api_key {
        params_map.insert(API_KEY_PARAM.to_string(), key.to_string());
    }

    let client = make_client(api_key)?;
    let mut current_params = params_map;
    let mut accumulated: Option<Value> = None;
    let mut seen: HashSet<String> = HashSet::new();
    let mut pages_fetched: usize = 0;
    seen.insert(canonical_params_key(&current_params));

    loop {
        let result = tokio::time::timeout(
            Duration::from_secs(30),
            client.search(current_params.clone()),
        )
        .await
        .map_err(|_| CliError::NetworkError {
            message: "Request timed out after 30s".to_string(),
        })?
        .map_err(network_err)?;
        let page = check_api_error(result)?;
        pages_fetched += 1;

        let next_url = page
            .get("serpapi_pagination")
            .and_then(|p| p.get("next"))
            .and_then(|n| n.as_str())
            .map(String::from);

        match &mut accumulated {
            None => accumulated = Some(page),
            Some(acc) => {
                if let (Value::Object(acc_map), Value::Object(page_map)) = (acc, &page) {
                    for (key, val) in page_map {
                        if let Value::Array(new_items) = val {
                            // Intentionally only merge array fields across pages.
                            // Scalar/object fields (search_metadata, pagination info, etc.)
                            // are kept from the first page, as they describe the overall
                            // query rather than per-page state.
                            match acc_map.get_mut(key) {
                                Some(Value::Array(existing)) => {
                                    existing.extend(new_items.iter().cloned())
                                }
                                _ => {
                                    acc_map.insert(key.clone(), val.clone());
                                }
                            }
                        }
                    }
                }
            }
        }

        match next_url {
            None => break,
            Some(url) => {
                if max_pages.is_some_and(|limit| pages_fetched >= limit) {
                    break;
                }
                let mut next_params = parse_next_params(&url)?;
                if let Some(key) = api_key {
                    next_params.insert(API_KEY_PARAM.to_string(), key.to_string());
                }
                let canonical = canonical_params_key(&next_params);
                if !seen.insert(canonical) {
                    break;
                }
                current_params = next_params;
            }
        }
    }

    // Strip the pagination metadata — it's misleading in a merged result.
    if let Some(Value::Object(ref mut map)) = accumulated {
        map.remove("serpapi_pagination");
    }

    accumulated.ok_or_else(|| CliError::ApiError {
        message: "No results returned".to_string(),
    })
}

pub(crate) fn parse_next_params(next_url: &str) -> Result<HashMap<String, String>, CliError> {
    let parsed = Url::parse(next_url).map_err(|e| CliError::NetworkError {
        message: format!("Invalid pagination URL: {e}"),
    })?;
    Ok(parsed
        .query_pairs()
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect())
}

/// Produce a stable, unambiguous key from query params regardless of original URL order.
/// Values are percent-encoded so that `=` and `&` inside values cannot collide with
/// the key=value and pair-joining separators.
pub(crate) fn canonical_params_key(params: &HashMap<String, String>) -> String {
    let sorted: std::collections::BTreeMap<_, _> = params.iter().collect();
    url::form_urlencoded::Serializer::new(String::new())
        .extend_pairs(sorted)
        .finish()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_canonical_params_key_is_order_independent() {
        let mut a = HashMap::new();
        a.insert("q".to_string(), "test".to_string());
        a.insert("start".to_string(), "0".to_string());
        a.insert("api_key".to_string(), "abc".to_string());

        let mut b = HashMap::new();
        b.insert("start".to_string(), "0".to_string());
        b.insert("api_key".to_string(), "abc".to_string());
        b.insert("q".to_string(), "test".to_string());

        assert_eq!(canonical_params_key(&a), canonical_params_key(&b));
    }
}
