import { resolve } from "node:path";
import { fixture, observations, cleanup } from "./fixture.mjs";

const [target, ...args] = process.argv.slice(2);
if (!target) throw new Error("expected executable followed by CLI arguments");
const executable = resolve(target);
if (process.env.CRON_REFERENCE_TRANSPORT === "http") {
  const child = Bun.spawn([executable, ...args], { stdin: "inherit", stdout: "inherit", stderr: "inherit" });
  process.exit(await child.exited);
}
const work = process.cwd();
const f = await fixture(work, process.env.CRON_FIXTURE ?? "empty");
try {
  const child = Bun.spawn([executable, ...args], {
    cwd: process.env.CRON_FIXTURE === "orphan" ? f.home : f.project,
    env: f.env,
    stdin: Bun.stdin,
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr, code] = await Promise.all([new Response(child.stdout).arrayBuffer(), new Response(child.stderr).arrayBuffer(), child.exited]);
  const result = await observations(f, executable);
  await Bun.write(resolve(work, "observations.json"), result.text);
  await Bun.write(Bun.stdout, result.normalizeBytes(stdout));
  await Bun.write(Bun.stderr, result.normalizeBytes(stderr));
  process.exitCode = code;
} finally {
  await cleanup(f);
}
