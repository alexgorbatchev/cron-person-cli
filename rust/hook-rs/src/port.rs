//! Byte-preserving shell integration generation, including Go's diagnostic quoting.
#![forbid(unsafe_code)]

use unicode_general_category::{GeneralCategory as Category, get_general_category};

const BASH: &str = r#"_cron_person_hook() {
  local previous_exit_status=$?;
  "%s" hook export bash;
  return $previous_exit_status;
};
if [[ ";${PROMPT_COMMAND:-};" != *";_cron_person_hook;"* ]]; then
  PROMPT_COMMAND="_cron_person_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
"#;
const ZSH: &str = r#"_cron_person_hook() {
  local previous_exit_status=$?;
  "%s" hook export zsh;
  return $previous_exit_status;
}
typeset -ag precmd_functions;
if (( ! ${precmd_functions[(I)_cron_person_hook]} )); then
  precmd_functions=(_cron_person_hook $precmd_functions);
fi
typeset -ag chpwd_functions;
if (( ! ${chpwd_functions[(I)_cron_person_hook]} )); then
  chpwd_functions=(_cron_person_hook $chpwd_functions);
fi
"#;
const FISH: &str = r#"function __cron_person_export_eval --on-event fish_prompt;
  set -l previous_exit_status $status;
  "%s" hook export fish;
  return $previous_exit_status;
end
"#;

// Go strconv.IsPrint uses Unicode 15 L/M/N/P/S plus ASCII space, excluding Zs.
fn go_printable(value: char) -> bool {
    value == ' '
        || !matches!(
            get_general_category(value),
            Category::Control
                | Category::Format
                | Category::Unassigned
                | Category::PrivateUse
                | Category::Surrogate
                | Category::SpaceSeparator
                | Category::LineSeparator
                | Category::ParagraphSeparator
        )
}

fn quote(value: &[u8]) -> String {
    let mut out = String::from("\"");
    for chunk in value.utf8_chunks() {
        for value in chunk.valid().chars() {
            match value {
                '"' => out.push_str("\\\""),
                '\\' => out.push_str("\\\\"),
                '\x07' => out.push_str("\\a"),
                '\x08' => out.push_str("\\b"),
                '\x0c' => out.push_str("\\f"),
                '\n' => out.push_str("\\n"),
                '\r' => out.push_str("\\r"),
                '\t' => out.push_str("\\t"),
                '\x0b' => out.push_str("\\v"),
                value if go_printable(value) => out.push(value),
                value if value < ' ' || value == '\x7f' => {
                    out.push_str(&format!("\\x{:02x}", u32::from(value)));
                }
                value if value < '\u{10000}' => {
                    out.push_str(&format!("\\u{:04x}", u32::from(value)));
                }
                value => out.push_str(&format!("\\U{:08x}", u32::from(value))),
            }
        }
        for byte in chunk.invalid() {
            out.push_str(&format!("\\x{byte:02x}"));
        }
    }
    out.push('"');
    out
}

/// Generate the exact bash, zsh or fish script, accepting arbitrary Go string bytes.
/// Empty binary names select `cron-person`. Binary bytes are interpolated verbatim,
/// preserving the original shell quoting behavior. Unsupported shells return Go text.
pub fn generate(shell: &[u8], binary: &[u8]) -> Result<Vec<u8>, String> {
    let template = match shell {
        b"bash" => BASH,
        b"zsh" => ZSH,
        b"fish" => FISH,
        _ => {
            return Err(format!(
                "unsupported shell {}: supported shells are bash, zsh, fish",
                quote(shell)
            ));
        }
    };
    let binary = if binary.is_empty() {
        b"cron-person"
    } else {
        binary
    };
    let mut script = Vec::with_capacity(template.len() + binary.len());
    for (index, part) in template.split("%s").enumerate() {
        if index > 0 {
            script.extend_from_slice(binary);
        }
        script.extend_from_slice(part.as_bytes());
    }
    Ok(script)
}

/// UTF-8 reference boundary; native consumers use `generate`.
#[cfg(feature = "bridge")]
pub fn generate_hook_script(shell: String, binary: String) -> Result<String, String> {
    let script = generate(shell.as_bytes(), binary.as_bytes())?;
    // The templates and both input strings are valid UTF-8.
    match String::from_utf8(script) {
        Ok(script) => Ok(script),
        Err(error) => Err(error.to_string()),
    }
}

/// Hex reference boundary, preserving arbitrary bytes in input and returned scripts.
#[cfg(feature = "bridge")]
pub fn generate_hook_bytes(shell: String, binary: String) -> Result<String, String> {
    let shell = hex::decode(shell).map_err(|_| "invalid shell hex".to_string())?;
    let binary = hex::decode(binary).map_err(|_| "invalid binary hex".to_string())?;
    generate(&shell, &binary).map(hex::encode)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bytes_and_go_quote_are_lossless() -> Result<(), String> {
        let script = generate(b"bash", b"\x00\xff\"$HOME")?;
        assert!(script.windows(8).any(|part| part == b"\x00\xff\"$HOME"));
        assert_eq!(quote(b"\xff\xed\xa0\x80"), r#""\xff\xed\xa0\x80""#);
        assert_eq!(
            quote("雪\u{a0}\u{f0000}".as_bytes()),
            "\"雪\\u00a0\\U000f0000\""
        );
        assert_eq!(unicode_general_category::UNICODE_VERSION, (15, 0, 0));
        Ok(())
    }
}
