//! Diagnostic formatting, preserving Go's byte boundaries and lexical home cleaning.
// Path cleaning is derived from Go's internal/filepathlite (Go Authors, BSD-3-Clause).
// See THIRD_PARTY_NOTICES.txt for the source license.
#![forbid(unsafe_code)]

use base64::{
    Engine, alphabet,
    engine::general_purpose::{GeneralPurpose, GeneralPurposeConfig},
};

const BASE64: GeneralPurpose = GeneralPurpose::new(
    &alphabet::STANDARD,
    GeneralPurposeConfig::new().with_decode_allow_trailing_bits(true),
);

fn home() -> Option<Vec<u8>> {
    let key = if cfg!(windows) { "USERPROFILE" } else { "HOME" };
    let value = std::env::var_os(key)?;
    #[cfg(unix)]
    let bytes = {
        use std::os::unix::ffi::OsStringExt;
        value.into_vec()
    };
    #[cfg(not(unix))]
    let bytes = value.to_string_lossy().as_bytes().to_vec();
    (!bytes.is_empty()).then_some(bytes)
}

fn path_separator(byte: u8, windows: bool) -> bool {
    byte == b'/' || windows && byte == b'\\'
}

fn windows_prefix(path: &[u8], prefix: &[u8]) -> bool {
    path.len() >= prefix.len()
        && path.iter().zip(prefix).all(|(&a, &b)| {
            if path_separator(b, true) {
                path_separator(a, true)
            } else {
                a.eq_ignore_ascii_case(&b)
            }
        })
        && (path.len() == prefix.len() || path_separator(path[prefix.len()], true))
}

fn unc_volume(path: &[u8], start: usize) -> usize {
    path.iter()
        .enumerate()
        .skip(start)
        .filter(|(_, byte)| path_separator(**byte, true))
        .nth(1)
        .map_or(path.len(), |(index, _)| index)
}

fn volume_len(path: &[u8], windows: bool) -> usize {
    if !windows {
        return 0;
    }
    if path.get(1) == Some(&b':') {
        return 2;
    }
    if path.first().is_none_or(|byte| !path_separator(*byte, true)) {
        return 0;
    }
    if windows_prefix(path, br"\\.\UNC") {
        return unc_volume(path, 8);
    }
    if [br"\\.".as_slice(), br"\\?", br"\??"]
        .iter()
        .any(|prefix| windows_prefix(path, prefix))
    {
        return if path.len() == 3 {
            3
        } else {
            path[4..]
                .iter()
                .position(|byte| path_separator(*byte, true))
                .map_or(path.len(), |index| index + 4)
        };
    }
    if path.get(1).is_some_and(|byte| path_separator(*byte, true)) {
        return unc_volume(path, 2);
    }
    0
}

// Preserve filepath.Clean's lexical volume and relative-path rules, including Windows.
fn clean_path(home: &[u8], windows: bool) -> Vec<u8> {
    let separator = if windows { b'\\' } else { b'/' };
    let volume = volume_len(home, windows);
    let input = &home[volume..];
    if input.is_empty() {
        let mut result = home.to_vec();
        if !(volume > 1 && path_separator(home[0], windows) && path_separator(home[1], windows)) {
            result.push(b'.');
        }
        if windows {
            result
                .iter_mut()
                .filter(|byte| **byte == b'/')
                .for_each(|byte| *byte = separator);
        }
        return result;
    }
    let rooted = path_separator(input[0], windows);
    let mut out = Vec::new();
    let mut changed = false;
    let append = |out: &mut Vec<u8>, byte, changed: &mut bool| {
        *changed |= input.get(out.len()) != Some(&byte);
        out.push(byte);
    };
    let mut index = 0;
    let mut dotdot = 0;
    if rooted {
        append(&mut out, separator, &mut changed);
        index = 1;
        dotdot = 1;
    }
    while index < input.len() {
        if path_separator(input[index], windows)
            || input[index] == b'.'
                && (index + 1 == input.len() || path_separator(input[index + 1], windows))
        {
            index += 1;
        } else if input[index..].starts_with(b"..")
            && (index + 2 == input.len() || path_separator(input[index + 2], windows))
        {
            index += 2;
            if out.len() > dotdot {
                out.pop();
                while out.len() > dotdot && !path_separator(out[out.len() - 1], windows) {
                    out.pop();
                }
                // The separator belongs to the removed element unless it is the root.
                if out.len() > dotdot {
                    out.pop();
                }
            } else if !rooted {
                if !out.is_empty() {
                    append(&mut out, separator, &mut changed);
                }
                append(&mut out, b'.', &mut changed);
                append(&mut out, b'.', &mut changed);
                dotdot = out.len();
            }
        } else {
            if rooted && out.len() != 1 || !rooted && !out.is_empty() {
                append(&mut out, separator, &mut changed);
            }
            while index < input.len() && !path_separator(input[index], windows) {
                append(&mut out, input[index], &mut changed);
                index += 1;
            }
        }
    }
    if out.is_empty() {
        append(&mut out, b'.', &mut changed);
    }
    if windows && volume == 0 && changed {
        if out
            .iter()
            .take_while(|byte| !path_separator(**byte, true))
            .any(|byte| *byte == b':')
        {
            out.splice(..0, [b'.', separator]);
        } else if out.starts_with(br"\??") {
            out.splice(..0, [separator, b'.']);
        }
    }
    let mut result = home[..volume].to_vec();
    result.extend(out);
    if windows {
        result
            .iter_mut()
            .filter(|byte| **byte == b'/')
            .for_each(|byte| *byte = separator);
    }
    result
}

fn clean_home(home: &[u8]) -> Vec<u8> {
    clean_path(home, cfg!(windows))
}

/// Fold the exact home directory or its descendants without resolving filesystem paths.
pub fn path(value: &[u8], home: Option<&[u8]>) -> Vec<u8> {
    let Some(home) = home.filter(|home| !home.is_empty()) else {
        return value.to_vec();
    };
    let mut home = clean_home(home);
    if value == home {
        return b"~".to_vec();
    }
    let separator = std::path::MAIN_SEPARATOR as u8;
    if home.last() != Some(&separator) {
        home.push(separator);
    }
    if let Some(rest) = value.strip_prefix(home.as_slice()) {
        let mut result = vec![b'~', separator];
        result.extend_from_slice(rest);
        result
    } else {
        value.to_vec()
    }
}

// The Go source converts each adjacent byte to a rune; it does not decode Unicode here.
fn delimiter(byte: u8) -> bool {
    matches!(byte, b'\t'..=b'\r' | b' ' | 0x85 | 0xa0) || b"\"'`:=()[]{};,".contains(&byte)
}

/// Fold home occurrences in diagnostics only when both byte boundaries permit it.
pub fn error_text(value: &[u8], home: Option<&[u8]>) -> Vec<u8> {
    let Some(home) = home.filter(|home| !home.is_empty()) else {
        return value.to_vec();
    };
    let home = clean_home(home);
    let mut message = value;
    let mut out = Vec::with_capacity(value.len());
    while let Some(index) = message
        .windows(home.len())
        .position(|window| window == home)
    {
        let end = index + home.len();
        let left = index == 0 || delimiter(message[index - 1]);
        let right = end == message.len()
            || message[end] == std::path::MAIN_SEPARATOR as u8
            || delimiter(message[end]);
        out.extend_from_slice(&message[..index]);
        out.extend_from_slice(if left && right { b"~" } else { &home });
        message = &message[end..];
    }
    out.extend_from_slice(message);
    out
}

/// String boundary used by the generated reference bridge.
pub fn format_path(value: String) -> Result<String, String> {
    String::from_utf8(path(value.as_bytes(), home().as_deref())).map_err(|error| error.to_string())
}

/// String error boundary used by the generated reference bridge.
pub fn format_error_text(value: String) -> Result<String, String> {
    String::from_utf8(error_text(value.as_bytes(), home().as_deref()))
        .map_err(|error| error.to_string())
}

fn decode(value: &str, error: &str) -> Result<Vec<u8>, String> {
    let value: Vec<u8> = value
        .bytes()
        .filter(|byte| !matches!(byte, b'\r' | b'\n'))
        .collect();
    BASE64.decode(value).map_err(|_| error.to_string())
}

fn byte_inputs(
    value: &str,
    home: &str,
    present: bool,
) -> Result<(Vec<u8>, Option<Vec<u8>>), String> {
    let value = decode(value, "invalid value base64")?;
    let home = decode(home, "invalid home base64")?;
    if present && home.contains(&0) {
        return Err("invalid HOME environment".into());
    }
    Ok((value, present.then_some(home)))
}

/// Lossless path bridge, including invalid UTF-8 and an absent HOME.
pub fn path_bytes(value: String, home: String, present: bool) -> Result<String, String> {
    let (value, home) = byte_inputs(&value, &home, present)?;
    Ok(BASE64.encode(path(&value, home.as_deref())))
}

/// Lossless diagnostic bridge, including invalid UTF-8 and an absent HOME.
pub fn error_bytes(value: String, home: String, present: bool) -> Result<String, String> {
    let (value, home) = byte_inputs(&value, &home, present)?;
    Ok(BASE64.encode(error_text(&value, home.as_deref())))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn windows_volumes_and_relative_paths_remain_lexical() {
        // Cases from Go 1.26.11 path/filepath/path_test.go, also run on Unix hosts.
        for (input, expected) in [
            (r"c:", r"c:."),
            (r"c:\", r"c:\"),
            (r"c:abc\..\..\.\.\..\def", r"c:..\..\def"),
            (r"c:\abc\def\..\..", r"c:\"),
            (r"c:\..\abc", r"c:\abc"),
            (r"c:..\abc", r"c:..\abc"),
            (r"c:\b:\..\..\..\d", r"c:\d"),
            (r"\", r"\"),
            ("/", r"\"),
            (r"\\i\..\c$", r"\\i\..\c$"),
            (r"\\i\..\i\c$", r"\\i\..\i\c$"),
            (r"\\host\share\foo\..\bar", r"\\host\share\bar"),
            ("//host/share/foo/../baz", r"\\host\share\baz"),
            (r"\\host\share\foo\..\..\..\..\bar", r"\\host\share\bar"),
            (r"\\.\C:\a\..\..\..\..\bar", r"\\.\C:\bar"),
            (r"\\.\C:\\\\a", r"\\.\C:\a"),
            (r"\\a\b\..\c", r"\\a\b\c"),
            (r"\\a\b", r"\\a\b"),
            (r".\c:", r".\c:"),
            (r".\c:\foo", r".\c:\foo"),
            (r".\c:foo", r".\c:foo"),
            ("//abc", r"\\abc"),
            ("///abc", r"\\\abc"),
            ("//abc//", r"\\abc\\"),
            (r"\\?\C:\", r"\\?\C:\"),
            (r"\\?\C:\a", r"\\?\C:\a"),
            ("a/../c:", r".\c:"),
            (r"a\..\c:", r".\c:"),
            ("a/../c:/a", r".\c:\a"),
            ("a/../../c:", r"..\c:"),
            ("foo:bar", "foo:bar"),
            ("/a/../??/a", r"\.\??\a"),
            (r"\\.\UNC\host\share\a\..", r"\\.\UNC\host\share\"),
            (r"\\.", r"\\."),
            (r"\??\c:\a\..", r"\??\c:\"),
            ("", "."),
            ("relative", "relative"),
        ] {
            assert_eq!(
                clean_path(input.as_bytes(), true),
                expected.as_bytes(),
                "{input:?}"
            );
        }
    }

    #[test]
    #[cfg(unix)]
    fn folds_only_descendants_and_diagnostic_boundaries() {
        assert_eq!(path(b"/a/file", Some(b"/a///")), b"~/file");
        assert_eq!(path(b"/ab/file", Some(b"/a")), b"/ab/file");
        assert_eq!(path(b"/a", Some(b"/x/../a")), b"~");
        assert_eq!(
            error_text(b"'/a/file' x/a /ab /a", Some(b"/a")),
            b"'~/file' x/a /ab ~"
        );
        assert_eq!(path(b"../home/file", Some(b"../home")), b"~/file");
        assert_eq!(path(b"home/file", Some(b"relative/../home")), b"~/file");
        assert_eq!(path(b"./file", Some(b".")), b"~/file");
        assert_eq!(path(b"/a", Some(b"/../../a")), b"~");
        assert_eq!(path(b"/a", None), b"/a");
        assert_eq!(error_text(b"/a", Some(b"")), b"/a");
    }

    #[test]
    #[cfg(unix)]
    fn byte_boundaries_are_lossless() {
        assert_eq!(
            error_text(b"\xc2\xa0/a/\xff", Some(b"/a")),
            b"\xc2\xa0~/\xff"
        );
        assert_eq!(
            error_text(b"\xce\xb1/a/\xff", Some(b"/a")),
            b"\xce\xb1/a/\xff"
        );
        assert_eq!(path(b"/\xff/\xfe", Some(b"/\xff")), b"~/\xfe");
        assert_eq!(path(b"/x", Some(b"/")), b"~/x");
        assert_eq!(error_text(b"/x", Some(b"/")), b"/x");
    }

    #[test]
    #[cfg(unix)]
    fn malformed_transport_inputs_fail() {
        assert_eq!(
            path_bytes("!".into(), "".into(), true),
            Err("invalid value base64".into())
        );
        assert_eq!(
            error_bytes("".into(), "!".into(), true),
            Err("invalid home base64".into())
        );
        assert_eq!(
            error_bytes("".into(), "AA==".into(), true),
            Err("invalid HOME environment".into())
        );
        assert_eq!(
            path_bytes("L2E=\n".into(), "L2E=".into(), true),
            Ok("fg==".into())
        );
        assert!(format_path("other".into()).is_ok());
        assert!(format_error_text("other".into()).is_ok());
    }

    #[test]
    #[cfg(windows)]
    fn native_windows_formatting_uses_cleaned_home_and_backslash_boundaries() {
        assert_eq!(
            path(br"C:\Users\alex\file", Some(br"C:/Users/alex/")),
            br"~\file"
        );
        assert_eq!(
            path(br"C:\Users\alexander\file", Some(br"C:\Users\alex")),
            br"C:\Users\alexander\file"
        );
        assert_eq!(
            error_text(
                br"'C:\Users\alex\file' C:\Users\alexander",
                Some(br"C:\Users\alex")
            ),
            br"'~\file' C:\Users\alexander"
        );
        assert_eq!(
            path(br"\\host\share\home\file", Some(br"\\host\share\home\.")),
            br"~\file"
        );
    }
}
