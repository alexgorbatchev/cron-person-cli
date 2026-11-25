use crontab_rs::native::{merge_crontab, remove_block};
use serde_json::Value;

#[test]
fn recorded_text_bytes_match_native() -> Result<(), Box<dyn std::error::Error>> {
    let mut count = 0;
    for (index, line) in include_str!("../cases.jsonl").lines().enumerate() {
        let case: Value = serde_json::from_str(line)?;
        if case["fn"] != "Text" {
            continue;
        }
        let operation = case["args"][0].as_str().ok_or("operation")?;
        let existing = case["args"][1].as_str().ok_or("existing")?;
        let managed = case["args"][2].as_str().ok_or("managed")?;
        let result = match (hex::decode(existing), hex::decode(managed)) {
            (Err(_), _) => Err("invalid existing hex".to_owned()),
            (_, Err(_)) => Err("invalid managed hex".to_owned()),
            (Ok(existing), Ok(managed)) => match operation {
                "remove" => remove_block(&existing)
                    .map(hex::encode)
                    .map_err(|e| e.to_string()),
                "merge" => merge_crontab(&existing, &managed)
                    .map(hex::encode)
                    .map_err(|e| e.to_string()),
                _ => Err("unknown text operation".to_owned()),
            },
        };
        let expected = expected(&case)?;
        assert_eq!(result, expected, "case {index}");
        count += 1;
    }
    assert_eq!(count, 1674);
    Ok(())
}

fn expected(case: &Value) -> Result<Result<String, String>, Box<dyn std::error::Error>> {
    Ok(if let Some(error) = case.get("error") {
        Err(error.as_str().ok_or("error")?.into())
    } else {
        Ok(case["ok"].as_str().ok_or("result")?.into())
    })
}

#[cfg(feature = "bridge")]
#[test]
fn complete_recorded_bridge_contract() -> Result<(), Box<dyn std::error::Error>> {
    for (index, line) in include_str!("../cases.jsonl").lines().enumerate() {
        let case: Value = serde_json::from_str(line)?;
        let function = case["fn"].as_str().ok_or("function")?;
        let result: Value =
            serde_json::from_str(&crontab_rs::call_json(function, &case["args"].to_string()))?;
        assert_eq!(expected(&result)?, expected(&case)?, "case {index}");
    }
    Ok(())
}
