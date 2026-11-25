use std::ffi::OsString;

use clap::{Arg, ArgAction, Command, value_parser};

/// The command `units::dispatch` gets when argv names no subcommand.
pub const ROOT: &str = "cron-person";

fn rest() -> Arg {
    Arg::new("args")
        .num_args(0..)
        .trailing_var_arg(true)
        .allow_hyphen_values(true)
        .value_parser(value_parser!(OsString))
}

/// Routes argv to a command. Anything this parser rejects runs the original, so it never
/// has to match every way the original accepts its arguments.
pub fn command() -> Command {
    Command::new(ROOT)
        .disable_help_flag(true)
        .disable_version_flag(true)
        .disable_help_subcommand(true)
        .args_conflicts_with_subcommands(true)
        .arg(rest())
        .subcommand(
            Command::new("allow")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("completion")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("crontab")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("dashboard")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("deny")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("dir")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("exec")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("help")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("hook")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("skill")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("status")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(rest()),
        )
        .subcommand(
            Command::new("task")
                .disable_help_flag(true)
                .arg(Arg::new("help").long("help").action(ArgAction::Count))
                .arg(
                    Arg::new("limit")
                        .long("limit")
                        .num_args(1)
                        .action(ArgAction::Append)
                        .value_parser(value_parser!(OsString)),
                )
                .arg(
                    Arg::new("tail")
                        .long("tail")
                        .num_args(1)
                        .action(ArgAction::Append)
                        .value_parser(value_parser!(OsString)),
                )
                .arg(rest()),
        )
}
