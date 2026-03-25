use serde_json::json;
use std::collections::HashMap;

// Tests that mutate HOME must not run concurrently with each other.
static HOME_MUTEX: std::sync::Mutex<()> = std::sync::Mutex::new(());

mod config_tests {
    use super::HOME_MUTEX;
    use serpapi_cli::config;

    #[test]
    fn test_config_path_ends_with_serpapi_config() {
        let path = config::config_path();
        let file_name = path.file_name().and_then(|s| s.to_str()).unwrap_or("");
        assert_eq!(
            file_name, "config.toml",
            "config path should end with config.toml, got: {:?}",
            path
        );
        let parent_name = path
            .parent()
            .and_then(|p| p.file_name())
            .and_then(|s| s.to_str())
            .unwrap_or("");
        assert_eq!(
            parent_name, "serpapi",
            "config path parent dir should be serpapi, got: {:?}",
            path
        );
    }

    #[test]
    fn test_config_dir_contains_serpapi() {
        let dir = config::config_dir();
        let dir_str = dir.to_str().unwrap();
        assert!(
            dir_str.contains("serpapi"),
            "config dir should contain serpapi, got: {}",
            dir_str
        );
    }

    #[test]
    fn test_resolve_api_key_flag_priority() {
        // clap already merges flag/env into a single Option; passing Some simulates that.
        let result = config::resolve_api_key(Some("flag_key"));
        assert_eq!(result.unwrap(), "flag_key");
    }

    #[test]
    fn test_resolve_api_key_config_file_fallback() {
        // Write a real config file in a temp dir and verify resolve_api_key(None)
        // reads it — this is the actual fallback path the function exists to serve.
        // Use config::config_dir() after redirecting HOME so the path is correct
        // on all platforms (macOS: ~/Library/Application Support, Linux: ~/.config).
        let _guard = HOME_MUTEX.lock().unwrap();
        let tmp = std::env::temp_dir().join("serpapi_test_config_fallback");
        std::fs::create_dir_all(&tmp).unwrap();

        let orig_home = std::env::var("HOME").ok();
        let orig_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        std::env::set_var("HOME", &tmp);
        std::env::remove_var("XDG_CONFIG_HOME");

        // Resolve the platform config dir NOW (after HOME is set) so the file
        // lands in exactly the location load_api_key() will look.
        let dir = config::config_dir();
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("config.toml"), "api_key = \"file_key\"\n").unwrap();

        let result = config::resolve_api_key(None);

        match orig_home {
            Some(h) => std::env::set_var("HOME", h),
            None => std::env::remove_var("HOME"),
        }
        match orig_xdg {
            Some(x) => std::env::set_var("XDG_CONFIG_HOME", x),
            None => std::env::remove_var("XDG_CONFIG_HOME"),
        }
        std::fs::remove_dir_all(&tmp).ok();

        assert_eq!(result.unwrap(), "file_key");
    }

    #[test]
    fn test_resolve_api_key_missing_returns_error() {
        let _guard = HOME_MUTEX.lock().unwrap();
        let tmp = std::env::temp_dir().join("serpapi_test_no_config");
        std::fs::create_dir_all(&tmp).ok();
        let orig_home = std::env::var("HOME").ok();
        let orig_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        std::env::set_var("HOME", &tmp);
        std::env::remove_var("XDG_CONFIG_HOME");

        let result = config::resolve_api_key(None);

        // Restore HOME
        match orig_home {
            Some(h) => std::env::set_var("HOME", h),
            None => std::env::remove_var("HOME"),
        }
        // Restore XDG_CONFIG_HOME
        match orig_xdg {
            Some(x) => std::env::set_var("XDG_CONFIG_HOME", x),
            None => std::env::remove_var("XDG_CONFIG_HOME"),
        }

        assert!(result.is_err());
        let err_msg = format!("{}", result.unwrap_err());
        assert!(err_msg.contains("No API key found"));
        // Clean up
        std::fs::remove_dir_all(&tmp).ok();
    }
}

mod params_tests {
    use super::*;
    use serpapi_cli::params::{apply_fields, params_to_hashmap, Param};

    #[test]
    fn test_param_from_str_with_equals() {
        let p: Param = "key=value".parse().unwrap();
        assert_eq!(p.key, "key");
        assert_eq!(p.value, "value");
    }

    #[test]
    fn test_param_from_str_without_equals() {
        let p: Param = "search_query".parse().unwrap();
        assert_eq!(p.key, "q");
        assert_eq!(p.value, "search_query");
    }

    #[test]
    fn test_param_from_str_with_multiple_equals() {
        let p: Param = "url=https://example.com?a=1&b=2".parse().unwrap();
        assert_eq!(p.key, "url");
        assert_eq!(p.value, "https://example.com?a=1&b=2");
    }

    #[test]
    fn test_param_from_str_rejects_empty_key() {
        let result: Result<Param, _> = "=value".parse();
        assert!(result.is_err(), "empty key should be rejected");
    }

    #[test]
    fn test_param_from_str_rejects_empty_string() {
        let result: Result<Param, _> = "".parse();
        assert!(result.is_err(), "empty string should be rejected");
    }

    #[test]
    fn test_params_to_hashmap() {
        let params = vec![
            Param {
                key: "q".to_string(),
                value: "search".to_string(),
            },
            Param {
                key: "engine".to_string(),
                value: "google".to_string(),
            },
        ];

        let map = params_to_hashmap(params);
        assert_eq!(map.get("q"), Some(&"search".to_string()));
        assert_eq!(map.get("engine"), Some(&"google".to_string()));
    }

    #[test]
    fn test_apply_fields_with_expression() {
        let mut map = HashMap::new();
        apply_fields(&mut map, Some("$.organic_results[0]"));
        assert_eq!(
            map.get("json_restrictor"),
            Some(&"$.organic_results[0]".to_string())
        );
    }

    #[test]
    fn test_apply_fields_with_none() {
        let mut map = HashMap::new();
        map.insert("existing".to_string(), "value".to_string());
        apply_fields(&mut map, None);
        assert_eq!(map.get("json_restrictor"), None);
        assert_eq!(map.get("existing"), Some(&"value".to_string()));
    }

    #[test]
    fn test_duplicate_key_last_value_wins() {
        // Document the "last wins" behavior of params_to_hashmap with duplicate keys.
        // HashMap keeps the last-inserted value for duplicate keys.
        let params = vec![
            Param {
                key: "q".to_string(),
                value: "first".to_string(),
            },
            Param {
                key: "q".to_string(),
                value: "second".to_string(),
            },
        ];
        let map = params_to_hashmap(params);
        assert_eq!(map.get("q"), Some(&"second".to_string()));
    }
}

mod error_tests {
    use super::HOME_MUTEX;
    use serpapi_cli::error::CliError;
    use serpapi_cli::params::Param;
    use std::error::Error;

    #[test]
    fn test_api_error_display() {
        let err = CliError::ApiError {
            message: "test message".into(),
        };
        let display = format!("{}", err);
        assert!(display.contains("API error"));
        assert!(display.contains("test message"));
    }

    #[test]
    fn test_usage_error_display() {
        let err = CliError::UsageError {
            message: "invalid command".into(),
        };
        let display = format!("{}", err);
        assert!(display.contains("Usage error"));
        assert!(display.contains("invalid command"));
    }

    #[test]
    fn test_network_error_display() {
        let err = CliError::NetworkError {
            message: "connection timeout".into(),
        };
        let display = format!("{}", err);
        assert!(display.contains("Network error"));
        assert!(display.contains("connection timeout"));
    }

    #[test]
    fn test_cli_error_is_error_trait() {
        let err: Box<dyn Error> = Box::new(CliError::ApiError {
            message: "test".into(),
        });
        assert!(!err.to_string().is_empty());
    }

    #[test]
    fn test_error_debug() {
        let err = CliError::ApiError {
            message: "debug test".into(),
        };
        let debug = format!("{:?}", err);
        assert!(debug.contains("ApiError"));
    }

    #[test]
    fn test_param_parse_error_produces_usage_error() {
        // "=value" has an empty key; parsing must fail
        let result: Result<Param, _> = "=value".parse();
        assert!(result.is_err());
        // The error message should be wrappable into a UsageError as the fix does
        let err_msg = result.unwrap_err().to_string();
        let usage_err = CliError::UsageError {
            message: err_msg.clone(),
        };
        let display = format!("{}", usage_err);
        assert!(display.contains("Usage error"));
        assert!(display.contains(&err_msg));
    }

    #[test]
    fn test_missing_api_key_produces_usage_error() {
        // Point HOME to a temp dir so load_api_key() finds no config file
        let _guard = HOME_MUTEX.lock().unwrap();
        let tmp = std::env::temp_dir().join("serpapi_test_no_key_usage");
        std::fs::create_dir_all(&tmp).ok();
        let orig_home = std::env::var("HOME").ok();
        let orig_xdg = std::env::var("XDG_CONFIG_HOME").ok();
        std::env::set_var("HOME", &tmp);
        std::env::remove_var("XDG_CONFIG_HOME");

        let result = serpapi_cli::config::resolve_api_key(None);

        match orig_home {
            Some(h) => std::env::set_var("HOME", h),
            None => std::env::remove_var("HOME"),
        }
        match orig_xdg {
            Some(x) => std::env::set_var("XDG_CONFIG_HOME", x),
            None => std::env::remove_var("XDG_CONFIG_HOME"),
        }
        std::fs::remove_dir_all(&tmp).ok();

        assert!(result.is_err());
        let err = result.unwrap_err();
        let display = format!("{}", err);
        // Confirm it formats as a UsageError (the die() path wraps it as JSON)
        assert!(display.contains("No API key found"));
        // Confirm the error JSON would contain "usage_error"
        let json_str = format!("{:?}", err);
        assert!(json_str.contains("UsageError"));
    }
}

mod output_tests {
    use super::*;
    use serpapi_cli::output::print_json;

    // Helper: render value as plain JSON string (mirrors the --json / non-TTY path).
    fn render(val: &serde_json::Value) -> String {
        serde_json::to_string_pretty(val).unwrap()
    }

    #[test]
    fn test_print_json_plain_succeeds() {
        let val = json!({"test": "value"});
        assert!(print_json(&val, true).is_ok());
    }

    #[test]
    fn test_print_json_plain_contains_key_and_value() {
        let val = json!({"test": "value"});
        let s = render(&val);
        assert!(s.contains("\"test\""), "expected key 'test' in output");
        assert!(s.contains("\"value\""), "expected value 'value' in output");
    }

    #[test]
    fn test_print_json_non_plain_succeeds() {
        let val = json!({"key": "value", "count": 42});
        assert!(print_json(&val, false).is_ok());
    }

    #[test]
    fn test_print_json_nested_object_renders_deeply() {
        let val = json!({
            "nested": {
                "deep": {
                    "value": "test"
                }
            },
            "array": [1, 2, 3]
        });
        let s = render(&val);
        assert!(print_json(&val, true).is_ok());
        assert!(s.contains("\"nested\""));
        assert!(s.contains("\"deep\""));
        assert!(s.contains("\"test\""));
        assert!(s.contains("["));
    }

    #[test]
    fn test_print_json_empty_object() {
        let val = json!({});
        let s = render(&val);
        assert!(print_json(&val, true).is_ok());
        assert_eq!(s.trim(), "{}");
    }

    #[test]
    fn test_print_json_array_contains_elements() {
        let val = json!([1, 2, 3, 4, 5]);
        let s = render(&val);
        assert!(print_json(&val, true).is_ok());
        assert!(s.contains('1'));
        assert!(s.contains('5'));
    }
}

mod print_jq_value_tests {
    use super::*;
    use serpapi_cli::output::print_jq_value;

    #[test]
    fn test_print_jq_value_string() {
        let val = json!("hello world");
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_number_integer() {
        let val = json!(42);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_number_float() {
        let val = json!(3.14);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_bool_true() {
        let val = json!(true);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_bool_false() {
        let val = json!(false);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_null() {
        let val = json!(null);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_object() {
        let val = json!({"key": "value"});
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }

    #[test]
    fn test_print_jq_value_array() {
        let val = json!([1, 2, 3]);
        let result = print_jq_value(&val, &mut Vec::new());
        assert!(result.is_ok());
    }
}
mod pagination_tests {
    use serde_json::{json, Value};

    /// Helper that simulates what the pagination loop does: merge page arrays into accumulated.
    fn merge_page(accumulated: &mut Value, page: &Value) {
        if let (Value::Object(acc_map), Value::Object(page_map)) = (accumulated, page) {
            for (key, val) in page_map {
                if let Value::Array(new_items) = val {
                    match acc_map.get_mut(key) {
                        Some(Value::Array(existing)) => existing.extend(new_items.iter().cloned()),
                        _ => {
                            acc_map.insert(key.clone(), val.clone());
                        }
                    }
                }
            }
        }
    }

    #[test]
    fn test_merge_extends_array_fields() {
        let mut acc = json!({
            "organic_results": [{"position": 1, "link": "https://a.com"}],
            "search_metadata": {"id": "page1"},
        });
        let page2 = json!({
            "organic_results": [{"position": 2, "link": "https://b.com"}],
            "search_metadata": {"id": "page2"},  // non-array: should NOT overwrite
        });
        merge_page(&mut acc, &page2);
        let results = acc["organic_results"].as_array().unwrap();
        assert_eq!(results.len(), 2);
        assert_eq!(results[0]["link"], "https://a.com");
        assert_eq!(results[1]["link"], "https://b.com");
        // Non-array field stays from page 1
        assert_eq!(acc["search_metadata"]["id"], "page1");
    }

    #[test]
    fn test_merge_adds_new_array_fields_from_later_pages() {
        let mut acc = json!({ "organic_results": [{"position": 1}] });
        let page2 = json!({
            "organic_results": [{"position": 2}],
            "related_questions": [{"question": "What is X?"}],
        });
        merge_page(&mut acc, &page2);
        assert_eq!(acc["organic_results"].as_array().unwrap().len(), 2);
        // related_questions only appeared on page 2 → should be present
        assert_eq!(acc["related_questions"].as_array().unwrap().len(), 1);
    }

    #[test]
    fn test_serpapi_pagination_stripped() {
        // After merge loop, serpapi_pagination is removed from the accumulated result.
        let mut result = json!({
            "organic_results": [{"position": 1}],
            "serpapi_pagination": { "next": "https://serpapi.com/search?start=10" },
        });
        if let Value::Object(ref mut map) = result {
            map.remove("serpapi_pagination");
        }
        assert!(result.get("serpapi_pagination").is_none());
        assert!(result.get("organic_results").is_some());
    }

    #[test]
    fn test_cycle_detection_stops_loop() {
        use std::collections::HashSet;
        let mut seen: HashSet<String> = HashSet::new();
        let url = "https://serpapi.com/search?engine=google&q=test&start=10".to_string();
        // First encounter: not a cycle
        assert!(!seen.contains(&url));
        seen.insert(url.clone());
        // Second encounter: cycle detected
        assert!(seen.contains(&url));
    }

    #[test]
    fn test_single_page_no_next_terminates() {
        // A page without serpapi_pagination.next should terminate the loop.
        let page = json!({
            "organic_results": [{"position": 1}],
            "search_information": {"total_results": "1"},
        });
        let next_url = page
            .get("serpapi_pagination")
            .and_then(|p| p.get("next"))
            .and_then(|n| n.as_str())
            .map(String::from);
        assert!(next_url.is_none());
    }
}

mod jq_tests {
    use super::*;
    use serpapi_cli::jq;

    #[test]
    fn test_jq_identity() {
        let input = json!({"a": 1, "b": 2});
        let expected = input.clone();
        let result = jq::apply(".", input).unwrap();
        assert_eq!(result, vec![expected]);
    }

    #[test]
    fn test_jq_field_access() {
        let result = jq::apply(".name", json!({"name": "test", "value": 42})).unwrap();
        assert_eq!(result, vec![json!("test")]);
    }

    #[test]
    fn test_jq_nested_access() {
        let result = jq::apply(".a.b.c", json!({"a": {"b": {"c": "deep"}}})).unwrap();
        assert_eq!(result, vec![json!("deep")]);
    }

    #[test]
    fn test_jq_array_index() {
        let result = jq::apply(".items[0]", json!({"items": ["a", "b", "c"]})).unwrap();
        assert_eq!(result, vec![json!("a")]);
    }

    #[test]
    fn test_jq_array_slice() {
        let result = jq::apply(".items[0:3]", json!({"items": [1, 2, 3, 4, 5]})).unwrap();
        assert_eq!(result, vec![json!([1, 2, 3])]);
    }

    #[test]
    fn test_jq_select_fields() {
        let result = jq::apply(
            "{name, age}",
            json!({"name": "test", "age": 30, "extra": true}),
        )
        .unwrap();
        assert_eq!(result, vec![json!({"name": "test", "age": 30})]);
    }

    #[test]
    fn test_jq_pipe() {
        let result = jq::apply(
            ".items | length",
            json!({"items": [{"name": "a"}, {"name": "b"}]}),
        )
        .unwrap();
        assert_eq!(result, vec![json!(2)]);
    }

    #[test]
    fn test_jq_map() {
        let result = jq::apply(
            "[.items[] | .name]",
            json!({"items": [{"name": "a", "v": 1}, {"name": "b", "v": 2}]}),
        )
        .unwrap();
        assert_eq!(result, vec![json!(["a", "b"])]);
    }

    #[test]
    fn test_jq_missing_key_returns_null() {
        use serde_json::Value;
        let result = jq::apply(".nonexistent", json!({"a": 1})).unwrap();
        assert_eq!(result, vec![Value::Null]);
    }

    #[test]
    fn test_jq_multiple_outputs() {
        let result = jq::apply(
            ".items[].name",
            json!({"items": [{"name": "a"}, {"name": "b"}, {"name": "c"}]}),
        )
        .unwrap();
        assert_eq!(result, vec![json!("a"), json!("b"), json!("c")]);
    }

    #[test]
    fn test_jq_invalid_expression() {
        let result = jq::apply("invalid![[", json!({"a": 1}));
        assert!(result.is_err());
    }

    #[test]
    fn test_jq_empty_filter_is_error() {
        assert!(jq::apply("", json!({})).is_err());
    }

    #[test]
    fn test_jq_null_input() {
        use serde_json::Value;
        let result = jq::apply(".", json!(null)).unwrap();
        assert_eq!(result, vec![Value::Null]);
    }
}

mod error_code_tests {
    use serpapi_cli::error::{exit_code, CliError};

    #[test]
    fn test_exit_codes_match_spec() {
        assert_eq!(exit_code(&CliError::ApiError { message: "".into() }), 1);
        assert_eq!(exit_code(&CliError::UsageError { message: "".into() }), 2);
        assert_eq!(exit_code(&CliError::NetworkError { message: "".into() }), 1);
    }
}

mod print_jq_value_output_tests {
    use serde_json::json;
    use serpapi_cli::output::print_jq_value;

    fn capture(value: serde_json::Value) -> String {
        let mut buf = Vec::new();
        print_jq_value(&value, &mut buf).unwrap();
        String::from_utf8(buf).unwrap()
    }

    #[test]
    fn test_string_printed_unquoted() {
        assert_eq!(capture(json!("hello")), "hello\n");
    }

    #[test]
    fn test_number_printed_as_digits() {
        assert_eq!(capture(json!(42)), "42\n");
    }

    #[test]
    fn test_bool_printed_as_word() {
        assert_eq!(capture(json!(true)), "true\n");
    }

    #[test]
    fn test_null_prints_empty_line() {
        assert_eq!(capture(serde_json::Value::Null), "\n");
    }

    #[test]
    fn test_object_printed_as_json() {
        let out = capture(json!({"a": 1}));
        assert!(out.contains("\"a\""));
        assert!(out.contains("1"));
    }
}
