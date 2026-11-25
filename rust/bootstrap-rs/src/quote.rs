//! Go diagnostic quoting, including byte escapes for invalid UTF-8.
use unicode_general_category::{GeneralCategory as C, get_general_category};
pub(super) fn quote(bytes: &[u8]) -> String {
    let mut out = String::from("\"");
    for chunk in bytes.utf8_chunks() {
        for c in chunk.valid().chars() {
            let escape = match c {
                '"' => Some("\\\""),
                '\\' => Some("\\\\"),
                '\x07' => Some("\\a"),
                '\x08' => Some("\\b"),
                '\x0c' => Some("\\f"),
                '\n' => Some("\\n"),
                '\r' => Some("\\r"),
                '\t' => Some("\\t"),
                '\x0b' => Some("\\v"),
                _ => None,
            };
            if let Some(escape) = escape {
                out.push_str(escape);
            } else if c == ' '
                || !matches!(
                    get_general_category(c),
                    C::Control
                        | C::Format
                        | C::Unassigned
                        | C::PrivateUse
                        | C::Surrogate
                        | C::SpaceSeparator
                        | C::LineSeparator
                        | C::ParagraphSeparator
                )
            {
                out.push(c);
            } else {
                let number = u32::from(c);
                out.push_str(&if number < 128 {
                    format!("\\x{number:02x}")
                } else if number < 65536 {
                    format!("\\u{number:04x}")
                } else {
                    format!("\\U{number:08x}")
                });
            }
        }
        for b in chunk.invalid() {
            out.push_str(&format!("\\x{b:02x}"));
        }
    }
    out.push('"');
    out
}
