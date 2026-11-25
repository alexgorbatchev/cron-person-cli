"""Execute actual generated hooks; clean every process group before returning."""
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys

fixture = Path(sys.argv[1])
stub = "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$HOOK_ARGS\"\nprintf 'forwarded-warning\\n' >&2\nexit 23\n"
for name in ("cron-person", "tool with spaces"):
    path = fixture / name
    path.write_text(stub)
    path.chmod(0o700)

assertions = 0
for shell, args, call in (
    ("bash", ["--noprofile", "--norc", "-c"], "_cron_person_hook"),
    ("zsh", ["-f", "-c"], "_cron_person_hook"),
    ("fish", ["--no-config", "-c"], "__cron_person_export_eval"),
):
    executable = shutil.which(shell)
    if executable is None:
        print(f"SKIP: {shell} not installed", flush=True)
        continue
    for kind in ("explicit", "default", "expansion"):
        script = (fixture / f"{shell}-{kind}").read_text()
        if shell == "bash":
            setup = "PROMPT_COMMAND=prior_prompt\ntrap 'echo prior-int-handler' INT\n"
            check = f"""
(exit 17)
{call}
printf 'fail_status=%d\\n' "$?"
true
{call}
printf 'ok_status=%d\\n' "$?"
printf 'prompt=%s\\n' "$PROMPT_COMMAND"
trap -p INT
"""
            retained = "prompt=_cron_person_hook;prior_prompt"
        elif shell == "zsh":
            setup = "precmd_functions=(prior_prompt)\nchpwd_functions=(prior_chpwd)\ntrap 'echo prior-int-handler' INT\n"
            check = f"""
(exit 17)
{call}
printf 'fail_status=%d\\n' "$?"
true
{call}
printf 'ok_status=%d\\n' "$?"
printf 'counts=%d,%d;tail=%s,%s\\n' "${{#precmd_functions}}" "${{#chpwd_functions}}" "$precmd_functions[2]" "$chpwd_functions[2]"
trap
"""
            retained = "counts=2,2;tail=prior_prompt,prior_chpwd"
        else:
            setup = "trap 'echo prior-int-handler' INT\n"
            check = f"""
sh -c 'exit 17'
{call}
printf 'fail_status=%d\\n' $status
true
{call}
printf 'ok_status=%d\\n' $status
printf 'registered=%d\\n' (functions -q {call}; echo $status)
trap
"""
            retained = "registered=0"
        log = fixture / "arguments"
        log.unlink(missing_ok=True)
        env = {**os.environ, "PATH": str(fixture) + os.pathsep + os.environ["PATH"],
               "HOME": str(fixture), "ZDOTDIR": str(fixture),
               "XDG_CONFIG_HOME": str(fixture), "HOOK_ARGS": str(log),
               "HOOK_BINARY": str(fixture / "tool with spaces")}
        process = subprocess.Popen([executable, *args, setup + script + script + check],
                                   env=env, cwd=fixture, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, start_new_session=True)
        try:
            out, err = process.communicate(timeout=10)
        finally:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=5)
        assert process.returncode == 0, (shell, kind, process.returncode, out, err)
        for expected in (b"fail_status=17\n", b"ok_status=0\n",
                         retained.encode(), b"prior-int-handler"):
            assert expected in out, (shell, kind, expected, out, err)
            assertions += 1
        assert err == b"forwarded-warning\n" * 2, (shell, kind, out, err)
        assert log.read_bytes() == (f"hook\nexport\n{shell}\n".encode()) * 2
        assertions += 2
    print(f"PASS: {shell} lifecycle, existing handlers, default/space/expansion binary", flush=True)
print(f"native shell assertions: {assertions}", flush=True)
