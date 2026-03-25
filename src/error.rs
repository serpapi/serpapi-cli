use serde_json::json;
use thiserror::Error;

// Variant names intentionally include the "Error" suffix for clarity at call sites
// (e.g. `CliError::ApiError` reads naturally).
#[allow(clippy::enum_variant_names)]
#[derive(Debug, Error)]
pub enum CliError {
    #[error("API error: {message}")]
    ApiError { message: String },
    #[error("Usage error: {message}")]
    UsageError { message: String },
    #[error("Network error: {message}")]
    NetworkError { message: String },
}

fn redact_api_key(s: &str) -> std::borrow::Cow<'_, str> {
    if !s.contains("api_key=") {
        return std::borrow::Cow::Borrowed(s);
    }
    let mut out = s.to_string();
    let mut search_from = 0;
    while let Some(rel) = out[search_from..].find("api_key=") {
        let pos = search_from + rel;
        let value_start = pos + "api_key=".len();
        let value_end = out[value_start..]
            .find(['&', ' ', ')', '"', '\''])
            .map(|i| value_start + i)
            .unwrap_or(out.len());
        out.replace_range(value_start..value_end, "[REDACTED]");
        // Advance past "api_key=[REDACTED]" so we don't re-examine it.
        search_from = value_start + "[REDACTED]".len();
    }
    std::borrow::Cow::Owned(out)
}

pub fn print_error(err: &CliError) {
    let (code, raw_message) = match err {
        CliError::ApiError { message } => ("api_error", message.as_str()),
        CliError::UsageError { message } => ("usage_error", message.as_str()),
        CliError::NetworkError { message } => ("network_error", message.as_str()),
    };
    let message = redact_api_key(raw_message);
    let message = message.as_ref();

    let error_json = json!({
        "error": {
            "code": code,
            "message": message
        }
    });

    match serde_json::to_string_pretty(&error_json) {
        Ok(s) => eprintln!("{s}"),
        Err(_) => eprintln!("{{\"error\":{{\"code\":\"{code}\",\"message\":\"{}\"}}}}",
            message.replace('\\', "\\\\").replace('"', "\\\"")),
    }
}

pub fn exit_code(err: &CliError) -> i32 {
    match err {
        CliError::ApiError { .. } => 1,
        CliError::UsageError { .. } => 2,
        CliError::NetworkError { .. } => 1,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_redact_no_api_key_is_noop() {
        let s = "some error without credentials";
        assert!(matches!(redact_api_key(s), std::borrow::Cow::Borrowed(_)));
    }

    #[test]
    fn test_redact_single_occurrence() {
        let s = "https://serpapi.com/search.json?q=coffee&api_key=supersecret&engine=google";
        let result = redact_api_key(s);
        assert!(result.contains("api_key=[REDACTED]"));
        assert!(!result.contains("supersecret"));
    }

    #[test]
    fn test_redact_multiple_occurrences_no_infinite_loop() {
        // Two api_key params (e.g. malformed URL); must terminate and redact both.
        let s = "api_key=first&other=x&api_key=second";
        let result = redact_api_key(s);
        assert!(result.contains("api_key=[REDACTED]"));
        assert!(!result.contains("first"));
        assert!(!result.contains("second"));
        // Both occurrences replaced
        assert_eq!(result.matches("[REDACTED]").count(), 2);
    }

    #[test]
    fn test_redact_at_end_of_string() {
        let s = "request failed: api_key=abc123";
        let result = redact_api_key(s);
        assert_eq!(result, "request failed: api_key=[REDACTED]");
    }

    #[test]
    fn test_redact_already_redacted_does_not_loop() {
        // If somehow called twice, [REDACTED] contains no secret and loop terminates.
        let s = "api_key=[REDACTED]";
        let result = redact_api_key(s);
        assert_eq!(result.as_ref(), "api_key=[REDACTED]");
    }
}
