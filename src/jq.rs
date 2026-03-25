use jaq_interpret::{Ctx, FilterT, ParseCtx, RcIter, Val};
use serde_json::Value;

pub fn apply(expression: &str, input: Value) -> Result<Vec<Value>, Box<dyn std::error::Error>> {
    let mut defs = ParseCtx::new(Vec::new());
    defs.insert_natives(jaq_core::core());
    defs.insert_defs(jaq_std::std());

    let (filter, errs) = jaq_parse::parse(expression, jaq_parse::main());
    if !errs.is_empty() {
        let msgs: Vec<String> = errs.iter().map(ToString::to_string).collect();
        return Err(format!("jq parse error: {}", msgs.join("; ")).into());
    }

    let Some(filter) = filter else {
        return Err("jq parse error: empty filter".into());
    };

    let filter = defs.compile(filter);
    if !defs.errs.is_empty() {
        return Err(format!("jq compile error: {} error(s)", defs.errs.len()).into());
    }

    let inputs = RcIter::new(std::iter::empty());
    let out: Vec<Value> = filter
        .run((Ctx::new(Vec::new(), &inputs), Val::from(input)))
        .map(|v| v.map(Value::from).map_err(|e| format!("jq runtime error: {e}")))
        .collect::<Result<Vec<_>, _>>()?;

    Ok(out)
}
