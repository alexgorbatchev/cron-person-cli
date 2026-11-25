const baseline = await Bun.file(new URL("../research/cli-baseline.json", import.meta.url)).json();
const cases = baseline.records.map(({ name, args, env }) => ({ name, args, env: { AGENT: env.AGENT ?? "0", COLUMNS: env.COLUMNS ?? "", CRON_FIXTURE: "empty" } }));
for (const agent of ["0", "1"]) {
  for (const scenario of ["empty", "allowed", "changed", "invalid", "missing", "orphan", "corrupt-store", "config-invalid"]) {
    for (const args of [["status"], ["dir", "status", "."], ["dir", "check", "."], ["allow", "."], ["deny", "."], ["task", "list"], ["task", "history"], ["crontab", "show"], ["crontab", "sync"], ["crontab", "uninstall"], ["hook", "export", "bash"]]) {
      cases.push({ name: `${args.join("-").replaceAll(".", "cwd")}-${scenario}-agent-${agent}`, args, env: { AGENT: agent, CRON_FIXTURE: scenario, COLUMNS: "80" } });
    }
  }
  for (const args of [["exec", ".", "printf hello"], ["exec", ".", "printf error >&2; exit 7"], ["task", "run", ".", "printf hello"], ["exec", ".", "printf '%s' \"$GREETING\""], ["task", "history"], ["task", "history", "--limit=0"], ["task", "log", "20260101T000000.000000Z-01020304"], ["task", "log", "20260101T000000.000000Z-01020304", "--tail=1"], ["task", "log", "not-found"], ["task", "history", "--limit=-1"], ["task", "log", "x", "--tail=-1"]]) {
    const scenario = args[0] === "exec" || args[1] === "run" ? "allowed" : "history";
    cases.push({ name: `execution-${cases.length}-agent-${agent}`, args, env: { AGENT: agent, CRON_FIXTURE: scenario, COLUMNS: "80" } });
  }
}
for (const agent of ["0", "1"]) {
  for (const command of ["exec", "task-run"]) {
    cases.push({ name: `binary-output-${command}-agent-${agent}`, args: [...(command === "exec" ? ["exec"] : ["task", "run"]), ".", "printf '\\377\\000\\376'; printf '\\375' >&2"], env: { AGENT: agent, CRON_FIXTURE: "allowed", COLUMNS: "80" } });
  }
}
await Bun.write(new URL("cli-inputs.jsonl", import.meta.url), cases.map((value) => JSON.stringify(value)).join("\n") + "\n");
