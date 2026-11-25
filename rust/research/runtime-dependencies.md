---
created_on: 2026-11-25 07:12
last_modified: 2026-11-25 07:17
status: current
---

# Rust runtime dependency and contract research

This report supports a behavior-preserving rewrite. Prefer `croner` for a validation feasibility spike, `cron-descriptor` for an English-description comparison, `clap` with a compatibility help renderer, and Axum with the existing React bundle. None of these choices establishes parity. YAML belongs to release tooling, not the application runtime. Rust Testcontainers can own the daemon fixture lifecycle, but two operations need additional support.

Evidence is source and documentation reviewed on 2026-11-25; metadata timestamps use America/Los_Angeles. Application findings are pinned to [Go baseline `5fa7005813c99bc54ebef41447d4adb2aaa6c32a`][baseline]. Crate versions below were checked against the primary crates.io registry and their published sources. No Rust dependency graph was resolved, compiled, benchmarked, or compared against a running Go binary in this research. Recommendations remain conditional on the equivalence checks below.

## Candidate versions

These are reviewed candidate versions, not a proposed Cargo.lock. The minimum Rust versions are package declarations, not a tested minimum for the combined dependency graph.

| Purpose | Candidate | Declared minimum Rust | Recommendation and limit |
| --- | --- | --- | --- |
| Cron validation / field values | [croner 5.0.0][croner-meta] | 1.85 | Strongest reviewed five-field candidate; needs a constrained dialect adapter. Do not use its scheduler. |
| English descriptions | [cron-descriptor 0.1.2][descriptor-meta] | 1.75 | Compare with the existing normalization pipeline and English, 24-hour settings; exact prose remains unverified. |
| Argument parsing | [clap 4.6.10][clap-doc] | 1.85 | Keep its parser; replace help presentation with the existing tree and agent formats. |
| Wrapping | [textwrap 0.16.4][textwrap-doc] | 1.88 | Optional wrapping primitive; its defaults differ from the Go renderer. |
| Display width | [unicode-width 0.2.3][unicode-doc] | 1.66 | Candidate cell-width calculation, subject to Go runewidth comparison. |
| Terminal size | [terminal_size 0.4.4][terminal-doc] | 1.71 | Query stdout specifically after applying the existing `COLUMNS` rule. |
| JSON models | [serde 1.0.229][serde-meta], [serde_json 1.0.154][json-meta] | 1.56 / 1.71 | Runtime configuration, authorization state, and dashboard wire models; decoding policies differ by endpoint. |
| Dashboard HTTP | [axum 0.8.9][axum-doc], [tokio 1.54.1][tokio-meta] | 1.80 / 1.85 | Local server and routing; explicitly reproduce HTTP/security behavior. |
| HTTP middleware | [tower-http 0.7.1][tower-doc] | 1.65 | Header utilities where useful; static filesystem serving is not asset embedding. |
| Embedded assets | [rust-embed 8.14.0][embed-source] | 1.80 | Enable `debug-embed` so both debug and release binaries are self-contained. `include_dir` 0.7.4 is an alternative, not an additional requirement. |
| Optional release YAML reader | [serde_yaml_ng 0.10.0][yaml-ng-doc] | 1.64 | Only if migrating the release verifier; YAML 1.1 support is not universal Go YAML equivalence. |
| Daemon integration fixture | [testcontainers 0.28.0][tc-doc] | 1.88 | Development dependency; image build, exec, cleanup, plus adapters for exec user and live upload. |

The reviewed `textwrap` and `testcontainers` versions set a provisional Rust 1.88 floor. Select and pin a supported toolchain after resolving the actual graph and checking release targets; package declarations alone do not establish buildability. Registry version records are available through `https://crates.io/api/v1/crates/<crate>/<version>`; published source archives are linked below where API details matter.

## Cron validation and descriptions

### Existing behavior

The application writes raw schedule strings into the user's system crontab and invokes `crontab -`; it does not run robfig's scheduler. Rust must preserve this boundary. Adding a Tokio cron scheduler would change process lifetime, clock/DST behavior, installation, and authorization execution rather than replace a dependency. Generated entries also preserve the command as one shell argument, escape cron percent signs, and carry absolute store/configuration paths. [Generation and installation source][go-crontab]

The current dependencies are robfig/cron/v3 **3.0.1** and lnquy/cron **1.1.1**. The `.cronrc` reader configures exactly minute, hour, day-of-month, month, and day-of-week fields. It separately permits the case-sensitive aliases `@reboot`, `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@midnight`, and `@hourly`; `@every` is explicitly refused. It retains the original schedule, command, raw line, and line number. Environment assignments, blank lines, whole-line comments, quoted values, and the command remainder are application parsing responsibilities, not cron-expression crate features. [Manifest][go-mod], [reader and normalization][go-cronrc]

Important inherited semantics:

- Robfig accepts comma unions, ranges, positive steps, case-insensitive month/weekday abbreviations, and `N/step` as `N-max/step`. Its field parser treats both `*` and `?` as wildcards, including minute, hour, and month positions. Bounds and invalid syntax must be tested against this configured parser, not generic POSIX documentation. [Robfig parser 3.0.1][robfig-parser]
- The application's weekday normalizer handles numeric Sunday `7` before validation and description. `5-7` becomes `5-6,0`; `5-7/2` becomes `5-6/2`, yielding Friday only for validation/descriptions. A stepped range ending in `7` does not automatically add Sunday. `7-7` becomes `0`, and `7-7/step` becomes `0-0/step`. The original expression is still emitted into the real crontab. Preserve this observed distinction during a compatibility rewrite; correcting or unifying it is a separate behavior decision. [Normalizer][go-cronrc], [description tests][go-description-test]
- Robfig's date matcher uses its wildcard marker bits to choose DOM/DOW AND versus OR. The application description wrapper makes a separate lexical decision: describe two clauses joined by `; or ` only when neither original day field contains `*` or `?`. These two rules are not interchangeable. [Robfig matcher][robfig-spec], [application description wrapper][go-description]

The description pipeline is part of the contract. It expands aliases, special-cases `@reboot` as `At system startup`, applies weekday normalization, validates with robfig, and obtains numeric field masks. It keeps `*`, `?`, and certain compact `*/step` expressions; otherwise it describes sorted numeric values from those masks. Minute/hour simple intervals are retained only when the step is smaller than the period and divides it evenly. For two restricted day fields it describes each alternative separately, retaining month restrictions in both clauses. Finally it uses lnquy's English locale and **24-hour time**. [Description source][go-description]

Consequently `*/35 * * * *` describes minutes 0 and 35, not a continuous every-35-minute interval; `*/60 * * * *` collapses to hourly values; and `0 */23 * * *` describes midnight and 23:00. Existing tests assert selected fragments rather than the complete description for many cases, so passing those tests alone would not prove exact text compatibility. [Description tests][go-description-test]

### Rust candidates and semantic traps

`croner` **5.0.0** exposes five-field parsing, configurable seconds/year handling, POSIX weekday numbering, and public field `values()`. A spike should explicitly set `Seconds::Disallowed`, `Year::Disallowed`, `alternative_weekdays(false)`, and `dom_and_dow(false)` and then constrain the accepted language. Its defaults permit optional seconds/year; its syntax also includes modifiers such as `L`, `W`, `#`, and a DOM/DOW conjunction extension that this application does not accept. Crate defaults therefore cannot become the public grammar. [Parser source][croner-parser], [component source][croner-component]

| Observed difference | Compatibility requirement |
| --- | --- |
| `croner` permits `?` only in the day fields, while robfig accepts it in every configured field. | Validate/translate the Go wildcard syntax for crate consumption while retaining the original input and description decisions. |
| `croner` rejects `N/step` by default; its `sloppy_ranges(true)` also accepts empty-start `/step`. | Prefer a narrow `N-max/step` transformation after Go-compatible validation; enabling sloppy mode alone broadens accepted input. |
| `croner` understands Sunday 7 and can include it in a stepped range. | Apply the existing application normalizer first, including its Friday-only `5-7/2` result. |
| Step storage/parsing is `u16`, with checked addition during expansion. Robfig parses positive machine-sized integers. | Include very large steps and addition overflow in the comparison corpus. A valid large step can select a single value yet fail in the candidate. |
| `croner` records day wildcard state using equality to `*`; robfig has bit markers, and descriptions use the application's lexical test. | Do not substitute crate date matching or description rules for the current wrapper. |

These are source differences, not executed differential results. If the adapter becomes an independent cron parser or cannot cover the actual corpus, reassess this candidate rather than claiming that five-field support establishes compatibility. [Published croner 5.0.0 source archive][croner-archive]

The reviewed `cron` **0.17.0** crate is a weaker fit: its longhand parser requires seconds with an optional year, its numeric weekdays start at Sunday **1**, and `Schedule::includes` combines DOM and DOW with AND. Merely prefixing `0` to five fields does not solve those differences. Do not select it as a direct robfig replacement. [Parser][cron-parser], [weekday mapping][cron-weekdays], [matcher][cron-schedule]

For prose, evaluate `cron-descriptor` **0.1.2** independently from validation. Its published API is `cronparser::cron_expression_descriptor::get_description_cron_options_locale`, returning a `Result`; use `Options::twenty_four_hour()` and locale `en`. The default is 12-hour time and zero-based weekdays. The implementation sets a process-global locale, so introducing concurrent locale changes would require separate review. Feed it the application's normalized descriptor fields and DOM/DOW clause split, not raw expressions. Its own examples and shared descriptor ancestry do not prove lnquy wording equivalence. `croner` also has a description implementation, with the same unproven text issue. [cron-descriptor 0.1.2 source][descriptor-source], [lnquy 1.1.1 source][lnquy-source]

Required comparison cases include every supported alias; `?` in all five positions; names and case; lists/ranges/steps; `1,3/2`; `*/35`, `*/60`, `*/65535`, `*/65536`; `0 */23`; DOM/DOW restrictions with and without `*` or `?`; Sunday `0`, `7`, `5-7`, `5-7/2`, `0-7/2`, `7-7/1`; invalid zero/negative steps, reversed ranges, extra fields, unsupported modifiers/descriptors; and environment/command preservation. Compare acceptance, field values, complete description bytes, error class/message policy, and emitted raw crontab separately. Keep a real-daemon check because validation masks do not determine what the OS executes.

## Hierarchical help, terminal width, and agent mode

The current stack is Cobra **1.10.2**, cobra-help-tree/v4 **4.0.0**, and go-runewidth **0.0.30**. `setupHelp` combines a technical catalog, recursive command graph, generated skill source, and a human tree configured to hide generated commands. Human help requires aligned `├─`/`╰─` trees with word wrapping and no truncation. Agent help uses a separate structured renderer, not the same tree at a larger width. [Manifest][go-mod], [help setup/catalog][go-help], [tree renderer][help-tree], [agent renderer][help-agent]

Preserve the following source-backed details:

- Width selection is positive integer `COLUMNS` first, then the real stdout terminal width, then **0/unwrapped**. Invalid, zero, or negative `COLUMNS` falls through to detection. Detection is not based on the command's injected writer, stderr, or stdin. Unknown width must not silently become 80 or 100 columns. [Terminal rules][help-terminal]
- Human wrapping measures display cells, preserves indentation/tree continuation alignment, expands tabs at eight-column stops, wraps at spaces, and never splits an overlong single word. The tests also verify that wrapping does not remove words. [Width implementation][help-width], [application width tests][go-help-width]
- `AGENT` is trimmed and case-folded; `1`, `true`, and `yes` enable it. Agent help ignores `COLUMNS`, does not truncate or wrap, and includes usage, arguments, environment, sorted flags including inherited flags, metadata, and recursive subcommands. It begins with the skill-reading alert checked by the application's tests. Its generated-command visibility differs from the human renderer; do not impose the human hiding rule on both. [Mode detection][help-terminal], [agent renderer][help-agent], [skill/alert tests][go-help-test]
- Root persistent `--store` and `--config`, command argument boundaries, aliases implemented as commands, root/group help behavior, version output, generated commands, and parser errors remain CLI contracts. `skill` prints the generated install command and URL, not the skill body. Dashboard has command-local help even though it lacks an explicit technical catalog entry. [Root command][go-root], [help setup][go-help], [generated skill behavior][help-skill]
- Agent mode also controls ordinary command output and hook behavior. It is not permission to convert all output to JSON or YAML. Task listing is plain text in both modes. [Task output][go-task], [hook command][go-hook]

Use **clap 4.6.10** for argument parsing and command/flag introspection; maintain one source of command metadata for rendering both existing formats. `Command::override_help` is supported but applies to one command, so root-only replacement is insufficient. Clap's help templates do not supply this project's recursive tree or technical agent catalog. Preserve parser behavior through library configuration and explicit fixtures, not a replacement argv parser. [Command API][clap-doc]

Clap's optional `wrap_help` feature and `term_width` do not reproduce the project policy: the documented fallback is 100 columns, and `term_width(0)` disables wrapping. Use the explicit application width rule before custom rendering. With **terminal_size 0.4.4**, call `terminal_size_of(stdout)` on Unix/Windows; `terminal_size()` falls back to stderr and stdin, which changes redirected-output behavior. [Clap width API][clap-doc], [terminal_size Unix source][terminal-unix], [Windows source][terminal-windows]

If using **textwrap 0.16.4**, investigate `break_words(false)`, `WordSplitter::NoHyphenation`, `WordSeparator::AsciiSpace`, `WrapAlgorithm::FirstFit`, and explicit initial/subsequent indentation. Defaults include word breaking and other algorithms that differ from the existing wrapper. This configuration is a starting point, not proof of whitespace or tree alignment parity. **unicode-width 0.2.3** also requires comparison against Go runewidth for combining marks, CJK, emoji/ZWJ sequences, and locale-sensitive widths. [Textwrap options][textwrap-doc], [Unicode width documentation][unicode-doc]

Required captures: every command/subcommand help, root/group no-argument behavior, `--help` versus `-h`, generated help/completion/skill commands, inherited/local/hidden flags, unknown subcommands, version output, widths 40/80/1000/unset/invalid, redirected stdout with stderr still attached, tabs and long words, Unicode, and `AGENT` variants. Existing width tests establish useful invariants but do not replace full help/agent snapshots. [Width tests][go-help-width], [help tests][go-help-test]

## YAML compatibility and runtime serialization

There is **no YAML task format or runtime configuration format** in the inspected Go application. `.cronrc` is crontab text; settings are JSON; authorization records are JSON. The only Go YAML import is **gopkg.in/yaml.v3 3.0.1** in `scripts/verify-release/main.go`, which reads `.goreleaser.yml` into a partial struct containing `project_name` and one build's `binary`, `goos`, and `goarch`. It requires a nonempty project name and exactly one build while ignoring unrelated GoReleaser fields. [Runtime config][go-config], [authorization store][go-store], [release verifier][go-release-verifier], [actual YAML][go-goreleaser]

Recommendation: omit a YAML dependency from the Rust application. If the parent also migrates the release verifier, keep YAML parsing isolated there and compare its actual limited input schema. `serde_yaml` **0.9.34+deprecated** explicitly states it is no longer maintained. **serde_yaml_ng 0.10.0** is a possible replacement for this narrow reader, but advertises YAML **1.1**, whereas go-yaml/v3 documents mostly YAML **1.2** with selected 1.1 compatibility. Neither is a universal substitute for the other. [serde_yaml published source][yaml-old-source], [serde_yaml_ng documentation][yaml-ng-doc], [go-yaml 3.0.1 compatibility][go-yaml-readme]

For the verifier, compare unknown-key handling, missing/empty strings, missing/multiple builds, sequence types, quoted/template strings, duplicate mapping keys, nulls, anchors/merge keys, scalar coercion, and extra YAML documents. Do not enable Serde `deny_unknown_fields` on this partial GoReleaser model: the real file contains many deliberately unread fields. Existing-file success establishes the required starting fixture; broader YAML support needs separate evidence.

For runtime JSON, use endpoint-specific Serde models. Configuration rejects unknown keys and trailing documents, starts from defaults (history retention 30 days, total size 100 MiB, per-run output 10 MiB), rejects negative limits, and treats zero as disabling a limit. Dashboard mutation decoding is more permissive. A single shared strictness policy would change behavior. Preserve existing record names and timestamps; do not migrate disk formats as an accidental consequence of using Serde. [Config loader][go-config], [record schema][go-store], [dashboard decoder][go-dashboard]

## Axum and the embedded dashboard

### The UI omitted by analysis

The analyzer reports `ui_files: 0`, but the baseline includes `web/`, a React dashboard/editor, a Bun/Tailwind build, and generated embedded HTML/JS/CSS. The actual source contract must supplement that report. The frontend uses **React/react-dom 19.3.0**, **@alexgorbatchev/shadcn 0.0.138**, and **@base-ui/react 1.9.0**; `web/bun.lock` records resolved dependencies. The Bun build also emits `THIRD_PARTY_NOTICES.txt`. [Frontend manifest][web-package], [lockfile][web-lock], [build][web-build], [dashboard component][web-dashboard], [task editor][web-editor]

Retaining this UI with a Rust backend yields a standalone executable containing JavaScript assets, not a frontend rewritten in Rust. It requires no Bun/Node process at runtime. Replacing React with a Rust/WASM UI is a separate rewrite and is not justified by this dependency substitution.

Recommend **Axum 0.8.9 + Tokio 1.54.1** and **rust-embed 8.14.0** with `debug-embed`. Embed the complete existing asset directory, including generated filenames and dependency notices, and preserve the build step that creates it. Do not hardcode chunk hashes or let debug builds read an arbitrary working-directory asset tree. `rust-embed` defaults to runtime filesystem reads in debug builds unless `debug-embed` is enabled. `include_dir` **0.7.4** is a simpler always-embedded alternative. [Axum docs][axum-doc], [rust-embed source/feature rules][embed-source], [include_dir docs][include-doc]

Serve embedded bytes with explicit MIME types; `nosniff` makes incorrect JS/CSS types a functional failure. Tower HTTP's filesystem `ServeDir` alone does not embed files. The Go implementation uses `http.FileServer`, not an unconditional SPA index fallback, so unknown asset paths must not all become successful HTML responses. Inventory root/index redirects, HEAD, ranges, conditional requests, path cleaning/traversal, missing files, and cache headers before choosing how much of Go's file-server wire behavior the compatibility layer must preserve. [Current handler][go-dashboard], [Go file-server implementation at toolchain 1.26.11][go-http-fs], [Tower HTTP 0.7.1 docs][tower-doc]

### Existing HTTP and security contract

All behavior in this section comes from the [dashboard handler][go-dashboard], [dashboard tests][go-dashboard-test], [store implementation][go-store], and [React API client][web-dashboard].

| Surface | Observed behavior to retain |
| --- | --- |
| Listener and startup | Bind IPv4 `127.0.0.1`; default port 8080, `--port 0` selects an available port. Print `url: http://<listener>/#<token>` with a newline. Generate a fresh cryptographically random token per server start. |
| Browser token | React reads the fragment with `window.location.hash.slice(1)` and sends `X-Dashboard-Token` on API requests. Keep it out of query strings, redirects, and logs; preserve fragment-based launch. |
| API authentication | Every request under `/api/`, including unknown routes/methods, first receives `Cache-Control: no-store` and requires the exact token header. Failure is 401 with `unauthorized\n`. Static assets are public. |
| Origin | After token validation, a nonempty Origin must equal `http://` plus the request Host. Foreign Origin is 403 with `foreign origin\n`; absent Origin is permitted. This is the current rule, not a fixed allowlist of bound hosts. |
| Global headers | All responses carry `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and CSP `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'`. |
| Directory list | `GET /api/directories` returns records `{dir,path,hash,allowed_at}` sorted by directory. An empty store returns `[]`. These are approval records, not the richer document status model. |
| Document read | `GET /api/document?dir=<absolute>` returns `{dir,content,revision,status}`. Revision is SHA-256 of the raw bytes read, including the empty-byte digest for an absent file. |
| Mutations | PUT, POST, DELETE `/api/document` accept the same JSON document shape; limit body reads to 1 MiB. No Content-Type check, unknown-field rejection, or second decode to require EOF. Missing string fields retain zero values. |
| Conflicts | Absolute path is required. Inspect current bytes, compare revision before action; mismatch returns 409 with `file changed; reload before saving\n`. One process-local mutex serializes API operations. |
| PUT | Validate complete `.cronrc`; create an absent file exclusively with mode 0600, or replace a regular file through a sibling temp file while retaining existing permission bits. Reject symlink/nonregular replacement. Saving does not reapprove the changed content. |
| POST | Status action `allowed` calls store approval; `denied` revokes it. Other values fail. Approval and save remain separate, and dashboard changes do not automatically synchronize the system crontab. |
| DELETE | Require a regular file; revoke approval before deleting it. A failed delete must not leave approval behind. |
| Responses | Successful mutation returns JSON `{\"ok\":true}` plus newline. Returned operational/parse errors become 400 with text and newline. Unknown API paths are 404; unsupported document methods are 405. `HEAD /api/directories` is 404, while `HEAD /api/document` is 405. |
| Lifecycle | HTTP request-header read timeout is 5 seconds. Interrupt/cancel initiates server shutdown with a 5-second context deadline. |

Document statuses include `allowed`, `changed`, `unauthorized`, `missing_cronrc`, `orphan`, `unreadable`, and `invalid`; the actual inspection path determines which is returned. The action value `denied` is not a new persistent status. React edits raw text and structured task rows, preserves the document revision, confirms discarded changes/deletion, and prevents approving unsaved edits. Its editor regex is a UI convenience, not the authoritative schedule validator. [Store statuses/inspection][go-store], [client/editor][web-dashboard], [task editor][web-editor]

The loopback listener, unpredictable header token, same-origin rule, and CSP should remain a single explicit boundary. Do not introduce permissive CORS, cookies, remote binding, unauthenticated mutation paths, or a token placed into a URL query as incidental framework changes. The inspected implementation has no explicit Host allowlist and its mutex is process-local; these facts do not establish protection against every DNS-rebinding or cross-process filesystem race. Stronger host validation, origin rules, or filesystem atomicity would require separate threat-model/behavior decisions, not a claim that the current implementation already provides them.

### Axum defaults that require deliberate handling

- Axum `Json<T>` requires JSON Content-Type, rejects incompatible JSON data with 422, rejects missing JSON Content-Type with 415, and ordinarily applies a 2 MiB body limit. The Go mutation path applies 1 MiB and maps decode errors to 400. Use a bounded body/decode layer with the required response mapping; do not assume a typed `Json` handler preserves the wire contract. Serde defaults/null/duplicate-field/trailing-value behavior also needs comparison. [Json extractor][axum-json], [rejection source][axum-rejection], [default body limit][axum-limit]
- Axum `get` also handles HEAD automatically. The Go API branches on literal methods and has the asymmetric HEAD behavior recorded above. Apply explicit handling and route fallbacks. Authentication/no-store must cover unknown `/api/` requests before route/method rejection; a route-local layer on known endpoints may leave gaps. [Axum GET routing][axum-get], [Router layering/fallback API][axum-router]
- Apply security headers around static files, API success, API errors, and fallback responses. Keep exact Origin/Host comparison semantics unless a deliberate security change is approved. Built-in Basic/Bearer authentication or CORS layers do not implement the existing custom-token rule. [Tower HTTP modules][tower-doc], [Go handler][go-dashboard]
- Blocking store/filesystem work must not hold up Tokio's executor. Preserve operation serialization and cross-process store/file locks independently of the choice of async mutex. Hash the same raw snapshot used for revision checks; JSON round-tripping or newline normalization before hashing changes approval/revision behavior. [Tokio blocking-work guidance][tokio-blocking], [Go store][go-store]
- `axum::serve` intentionally offers no connection configuration. Retaining the five-second header timeout requires a configurable Hyper/Hyper-util serving path; Hyper **1.13.0** exposes `header_read_timeout` and requires a timer. Graceful shutdown also needs a bounded lifecycle policy rather than assuming a framework default gives five seconds. [Axum serve source][axum-serve], [Hyper 1.13.0 HTTP/1 builder][hyper-builder]

Required evidence includes a byte-level asset inventory/hash comparison; disconnected operation of the executable; production and debug embedding; browser interaction using the retained UI; CRUD/status/revision flows; stale/invalid content; save-versus-approval; unauthorized/foreign/missing Origin; unknown route and method ordering; exact headers/errors; bodies around 1 MiB; symlink/nonregular files and permission retention; and cancellation/header-timeout behavior. Existing Go tests exercise core CRUD/security/conflicts, but do not cover every wire detail listed here. No screenshots or new browser checks were produced because this task changes no UI or server implementation.

## Real cron daemon integration tests

The existing `daemon` build-tag suite runs Debian cron in a disposable Docker image based on `debian:bookworm-slim`, with a real unprivileged user and cron process. It copies a Linux binary into the running container, executes as root or that user with explicit HOME/AGENT, and waits for actual minute fires. It checks cwd/environment/literal `%`, successful cron history, refusal after edits/revocation, in-place binary upgrades, moved executable paths after sync, and preserving personal entries during uninstall. This is stronger than a parser or container-start test. [Go fixture][go-daemon], [Dockerfile][go-daemon-docker], [recipes][go-just]

Recommend evaluating **testcontainers 0.28.0** as a dev dependency with `GenericBuildableImage`, `AsyncBuilder`/`AsyncRunner`, and the existing Dockerfile intent. The crate supports build contexts, startup waits, exec environment variables, exit status, separate stdout/stderr readers, and container removal. Blocking runners are feature-gated. Keep Docker/real-time tests opt-in and separate from hermetic unit tests. [Crate documentation][tc-doc], [buildable image source][tc-build], [execution command source][tc-exec], [container source][tc-container]

Two reviewed API gaps matter:

1. `ExecCommand` has command, wait conditions, and environment fields, but no per-exec user setter. `ImageExt::with_user` is container-wide, unsuitable when root cron must coexist with user-specific CLI invocations. A validated `runuser`/`su` wrapper in the Debian fixture or direct Docker exec API is needed; confirm HOME/environment behavior and exit propagation. [Exec source][tc-exec], [image options][tc-image-ext]
2. `with_copy_to` prepares files before startup. The reviewed public running-container API provides copy-from, but not Go's live copy-to operation; the internal upload client is private. The upgrade test must upload a new executable while cron is already running. Evaluate the crate's re-exported Bollard client/API for upload and explicit exec-user control, or a narrowly owned Docker CLI adapter. Ensure it uses the same resolved daemon/TLS credentials and preserves `.new` staging, mode 0755, and rename-over-old semantics. Preloading one binary at container startup does not exercise the existing upgrade contract. [Container API source][tc-container], [internal client/source boundary][tc-client], [Bollard re-export][tc-lib], [Go staging helper][go-daemon]

The documented host resolution considers testcontainers properties, `DOCKER_HOST`, and standard/rootless sockets; it does not document selecting the active Docker CLI context. Preserve the existing recipe's `DOCKER_HOST` derivation when unset. Keep explicit test deadlines and collect exit/stdout/stderr evidence rather than relying on startup waits to prove cron fires. Normal Drop cleanup is not evidence of cleanup after every process termination; verify the chosen fixture's cancellation path and retain resources only when cleanup cannot be confirmed. [Host resolution and lifecycle docs][tc-doc], [current recipe][go-just]

The Go fixture cross-compiles a static Linux binary. Rust compilation needs a selected Linux target, compatible linker/libc, and target standard library; changing only the target triple is not a demonstrated replacement on macOS or Windows hosts. The parent must select a reproducible Linux build path and exercise it on supported development/CI hosts before accepting this harness.

## Parent decisions and acceptance boundaries

Research is ready for parent integration. No dependency has been accepted as equivalent, and no executable behavior was changed. These decisions remain for the rewrite owner:

| Decision | Evidence and recommended direction | Unresolved consequence |
| --- | --- | --- |
| Meaning of whole-app Rust | The application ships a functional React/shadcn frontend omitted by analyzer UI detection. Retain the existing bundle and replace the Go backend. | A literal Rust frontend requires a separately scoped UI rewrite and visual/interaction acceptance. |
| Exact compatibility threshold | Crate source differences are demonstrated for validation, descriptions, help, and HTTP defaults; current tests often assert fragments/invariants. Preserve existing behavior and compare full outputs first. | The owner must establish how to handle mismatches in prose, error text, file-server edge behavior, and inherited parser quirks. No loosening is assumed here. |
| Cron adapter versus source port | `croner` offers useful parsing/value primitives but requires dialect translations and large-step handling; prose has a separate candidate. Spike against the complete Go corpus before locking dependencies. | If adaptation becomes a second parser or exact prose cannot be achieved, a narrow source-derived port needs an explicit maintainability/licensing decision. |
| Dashboard serving depth | Axum is suitable, but preserving five-second header timeout requires configurable serving, and JSON/routing defaults differ. Retain the existing security/wire boundary. | New security hardening and altered HTTP acceptance belong in deliberate changes with their own tests. |
| Release tooling scope | YAML is confined to the GoReleaser verifier. Keep it out of runtime. | Porting the verifier or replacing GoReleaser changes release tooling scope and determines whether any Rust YAML crate is needed. |
| Toolchain and daemon harness | Reviewed wrapping/container crates declare Rust 1.88; daemon parity needs Linux builds, per-user exec, and live binary upload. | Resolve Cargo features/lockfile, release-target support, Linux build method, and the fixture adapter before implementation acceptance. |

The next action is to incorporate these contracts and unresolved choices into the rewrite plan, then run narrowly scoped differential spikes. This report supplies source evidence, not a completed parity gate.

[baseline]: https://github.com/alexgorbatchev/cron-person-cli/tree/5fa7005813c99bc54ebef41447d4adb2aaa6c32a
[go-mod]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/go.mod
[go-cronrc]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/cronrc/cronrc.go
[go-description]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/cronrc/description.go
[go-description-test]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/cronrc/description_test.go
[go-crontab]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/crontab/crontab.go
[robfig-parser]: https://github.com/robfig/cron/blob/v3.0.1/parser.go
[robfig-spec]: https://github.com/robfig/cron/blob/v3.0.1/spec.go
[lnquy-source]: https://github.com/lnquy/cron/tree/v1.1.1
[croner-meta]: https://crates.io/api/v1/crates/croner/5.0.0
[croner-archive]: https://static.crates.io/crates/croner/croner-5.0.0.crate
[croner-parser]: https://docs.rs/crate/croner/5.0.0/source/src/parser.rs
[croner-component]: https://docs.rs/crate/croner/5.0.0/source/src/component.rs
[cron-parser]: https://docs.rs/crate/cron/0.17.0/source/src/parsing.rs
[cron-weekdays]: https://docs.rs/crate/cron/0.17.0/source/src/time_unit/days_of_week.rs
[cron-schedule]: https://docs.rs/crate/cron/0.17.0/source/src/schedule.rs
[descriptor-meta]: https://crates.io/api/v1/crates/cron-descriptor/0.1.2
[descriptor-source]: https://docs.rs/crate/cron-descriptor/0.1.2/source/src/lib.rs
[go-root]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/root.go
[go-help]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/help.go
[go-help-width]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/help_width_test.go
[go-help-test]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/skill_test.go
[go-task]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/task.go
[go-hook]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/hook.go
[help-tree]: https://github.com/alexgorbatchev/cobra-help-tree/blob/v4.0.0/tree.go
[help-agent]: https://github.com/alexgorbatchev/cobra-help-tree/blob/v4.0.0/agent.go
[help-terminal]: https://github.com/alexgorbatchev/cobra-help-tree/blob/v4.0.0/terminal.go
[help-width]: https://github.com/alexgorbatchev/cobra-help-tree/blob/v4.0.0/width.go
[help-skill]: https://github.com/alexgorbatchev/cobra-help-tree/blob/v4.0.0/skill.go
[clap-doc]: https://docs.rs/clap/4.6.10/clap/struct.Command.html
[textwrap-doc]: https://docs.rs/textwrap/0.16.4/textwrap/struct.Options.html
[unicode-doc]: https://docs.rs/unicode-width/0.2.3/unicode_width/
[terminal-doc]: https://docs.rs/terminal_size/0.4.4/terminal_size/
[terminal-unix]: https://docs.rs/crate/terminal_size/0.4.4/source/src/unix.rs
[terminal-windows]: https://docs.rs/crate/terminal_size/0.4.4/source/src/windows.rs
[serde-meta]: https://crates.io/api/v1/crates/serde/1.0.229
[json-meta]: https://crates.io/api/v1/crates/serde_json/1.0.154
[go-config]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/config/config.go
[go-store]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/internal/store/store.go
[go-release-verifier]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/scripts/verify-release/main.go
[go-goreleaser]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/.goreleaser.yml
[yaml-ng-doc]: https://docs.rs/serde_yaml_ng/0.10.0/serde_yaml_ng/
[yaml-old-source]: https://docs.rs/crate/serde_yaml/0.9.34+deprecated/source/README.md
[go-yaml-readme]: https://github.com/go-yaml/yaml/blob/v3.0.1/README.md
[go-dashboard]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/dashboard.go
[go-dashboard-test]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/dashboard_test.go
[web-package]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/web/package.json
[web-lock]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/web/bun.lock
[web-build]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/web/build.ts
[web-dashboard]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/web/components/Dashboard.tsx
[web-editor]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/web/components/TaskEditor.tsx
[axum-doc]: https://docs.rs/axum/0.8.9/axum/
[axum-json]: https://docs.rs/axum/0.8.9/axum/struct.Json.html
[axum-rejection]: https://docs.rs/crate/axum/0.8.9/source/src/extract/rejection.rs
[axum-limit]: https://docs.rs/axum/0.8.9/axum/extract/struct.DefaultBodyLimit.html
[axum-get]: https://docs.rs/axum/0.8.9/axum/routing/method_routing/fn.get.html
[axum-router]: https://docs.rs/axum/0.8.9/axum/struct.Router.html
[axum-serve]: https://docs.rs/crate/axum/0.8.9/source/src/serve/mod.rs
[tokio-meta]: https://crates.io/api/v1/crates/tokio/1.54.1
[tokio-blocking]: https://docs.rs/tokio/1.54.1/tokio/task/fn.spawn_blocking.html
[tower-doc]: https://docs.rs/tower-http/0.7.1/tower_http/
[embed-source]: https://docs.rs/crate/rust-embed/8.14.0/source/README.md
[include-doc]: https://docs.rs/include_dir/0.7.4/include_dir/
[go-http-fs]: https://github.com/golang/go/blob/go1.26.11/src/net/http/fs.go
[hyper-builder]: https://docs.rs/hyper/1.13.0/hyper/server/conn/http1/struct.Builder.html
[go-daemon]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/cmd/cron-person/daemon_test.go
[go-daemon-docker]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/testdata/daemon/Dockerfile
[go-just]: https://github.com/alexgorbatchev/cron-person-cli/blob/5fa7005813c99bc54ebef41447d4adb2aaa6c32a/justfile
[tc-doc]: https://docs.rs/testcontainers/0.28.0/testcontainers/
[tc-build]: https://docs.rs/crate/testcontainers/0.28.0/source/src/buildables/generic.rs
[tc-exec]: https://docs.rs/crate/testcontainers/0.28.0/source/src/core/image/exec.rs
[tc-container]: https://docs.rs/crate/testcontainers/0.28.0/source/src/core/containers/async_container.rs
[tc-image-ext]: https://docs.rs/crate/testcontainers/0.28.0/source/src/core/image/image_ext.rs
[tc-client]: https://docs.rs/crate/testcontainers/0.28.0/source/src/core/client.rs
[tc-lib]: https://docs.rs/crate/testcontainers/0.28.0/source/src/lib.rs
