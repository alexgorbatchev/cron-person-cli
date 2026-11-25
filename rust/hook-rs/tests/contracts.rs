use hook_rs::port::generate;
use serde_json::Value;

#[test]
fn recorded_original_cases_match_native_generation() -> Result<(), Box<dyn std::error::Error>> {
    for (index, line) in include_str!("../cases.jsonl").lines().enumerate() {
        let case: Value = serde_json::from_str(line)?;
        let function = case["fn"].as_str().ok_or("missing function")?;
        let shell = case["args"][0].as_str().ok_or("missing shell")?;
        let binary = case["args"][1].as_str().ok_or("missing binary")?;
        let actual = if function == "GenerateHookBytes" {
            match (hex::decode(shell), hex::decode(binary)) {
                (Err(_), _) => Err("invalid shell hex".to_string()),
                (_, Err(_)) => Err("invalid binary hex".to_string()),
                (Ok(shell), Ok(binary)) => generate(&shell, &binary).map(hex::encode),
            }
        } else {
            generate(shell.as_bytes(), binary.as_bytes())
                .and_then(|bytes| String::from_utf8(bytes).map_err(|e| e.to_string()))
        };
        let expected = match case.get("error") {
            Some(error) => Err(error.as_str().ok_or("invalid error")?.to_string()),
            None => Ok(case["ok"].as_str().ok_or("invalid result")?.to_string()),
        };
        assert_eq!(actual, expected, "original case {index}");
        #[cfg(feature = "bridge")]
        {
            let bridged = if function == "GenerateHookBytes" {
                hook_rs::port::generate_hook_bytes(shell.into(), binary.into())
            } else {
                hook_rs::port::generate_hook_script(shell.into(), binary.into())
            };
            assert_eq!(bridged, expected, "bridge case {index}");
        }
    }
    Ok(())
}

#[cfg(feature = "bridge")]
#[test]
fn generated_dispatch_reaches_real_implementation() -> Result<(), Box<dyn std::error::Error>> {
    let value: Value = serde_json::from_str(&hook_rs::call_json(
        "GenerateHookBytes",
        r#"["66697368", "ff"]"#,
    ))?;
    assert_eq!(value["ok"], hex::encode(generate(b"fish", &[0xff])?));
    let value: Value =
        serde_json::from_str(&hook_rs::call_json("GenerateHookScript", r#"["sh", ""]"#))?;
    assert_eq!(
        value["error"].as_str(),
        generate(b"sh", b"").err().as_deref()
    );
    Ok(())
}
