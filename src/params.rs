use std::collections::HashMap;
use std::str::FromStr;

#[derive(Debug, Clone)]
pub struct Param {
    pub key: String,
    pub value: String,
}

impl FromStr for Param {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let mut parts = s.splitn(2, '=');
        match (parts.next(), parts.next()) {
            (Some(""), Some(_)) => Err("Empty parameter key".to_string()),
            (Some("api_key"), Some(_)) => Err(
                "Use --api-key or SERPAPI_KEY instead of passing api_key as a parameter"
                    .to_string(),
            ),
            (Some(key), Some(value)) => Ok(Self {
                key: key.to_string(),
                value: value.to_string(),
            }),
            (Some(""), None) | (None, _) => Err("Empty parameter".to_string()),
            (Some(value), None) => Ok(Self {
                key: "q".to_string(),
                value: value.to_string(),
            }),
        }
    }
}

pub fn params_to_hashmap(params: Vec<Param>) -> HashMap<String, String> {
    params.into_iter().map(|p| (p.key, p.value)).collect()
}

pub fn apply_fields(params: &mut HashMap<String, String>, fields: Option<&str>) {
    if let Some(expr) = fields {
        params.insert("json_restrictor".to_string(), expr.to_string());
    }
}
