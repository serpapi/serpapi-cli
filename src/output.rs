use serde_json::Value;
use std::io::IsTerminal;

/// Print JSON to stdout. Uses colored output when stdout is a terminal and
/// `plain` is false; otherwise prints plain pretty-printed JSON.
pub fn print_json(value: &Value, plain: bool) -> Result<(), Box<dyn std::error::Error>> {
    let use_color = !plain && std::io::stdout().is_terminal();
    if use_color {
        use colored_json::to_colored_json_auto;
        println!("{}", to_colored_json_auto(value)?);
    } else {
        println!("{}", serde_json::to_string_pretty(value)?);
    }
    Ok(())
}

/// Print a jq result value with raw scalar output (like gh --jq / jq -r).
/// Strings are printed unquoted, numbers as plain text, booleans as true/false,
/// null as empty string. Objects and arrays are JSON-encoded.
pub fn print_jq_value(value: &Value, writer: &mut impl std::io::Write) -> Result<(), Box<dyn std::error::Error>> {
    match value {
        Value::String(s) => writeln!(writer, "{s}")?,
        Value::Number(n) => writeln!(writer, "{n}")?,
        Value::Bool(b) => writeln!(writer, "{b}")?,
        Value::Null => writeln!(writer)?,
        _ => writeln!(writer, "{}", serde_json::to_string_pretty(value)?)?,
    }
    Ok(())
}
