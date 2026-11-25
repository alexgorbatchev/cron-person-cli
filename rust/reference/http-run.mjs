import { mkdir, mkdtemp, rm, symlink } from "node:fs/promises";
import { dirname, resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
import { fixture, observations, cleanup } from "./fixture.mjs";

const [target, portText = process.env.PORT] = process.argv.slice(2);
const executable = resolve(target);
const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const stopped = Promise.withResolvers();
const onStop = () => stopped.resolve();
process.once("SIGTERM", onStop);
process.once("SIGINT", onStop);
let work, f, child, server, reader;
function signalChild(signal) {
  if (!child) return;
  if (process.platform === "win32") child.kill(signal);
  else {
    try { process.kill(-child.pid, signal); }
    catch (error) { if (error.code !== "ESRCH") throw error; }
  }
}
try {
  await mkdir(join(repo, ".tmp"), { recursive: true });
  work = await mkdtemp(join(repo, ".tmp", "rust-http-reference-"));
  f = await fixture(work);
  const linkDir = join(f.home, "link");
  await mkdir(linkDir, { mode: 0o700 });
  await symlink(join(f.project, ".cronrc"), join(linkDir, ".cronrc"));
  child = Bun.spawn([executable, "dashboard", "--port=0"], {
    cwd: f.project,
    env: { ...f.env, CRON_REFERENCE_TRANSPORT: "http", BUN_RUNTIME_TRANSPILER_CACHE_PATH: join(repo, ".cache", "rust-reference-bun") },
    detached: process.platform !== "win32",
    stdin: "ignore", stdout: "pipe", stderr: "inherit",
  });
  reader = child.stdout.getReader();
  let startup = "";
  while (!startup.includes("\n")) {
    const next = await Promise.race([reader.read(), stopped.promise.then(() => ({ done: true }))]);
    if (next.done) throw new Error("dashboard stopped before publishing its URL");
    startup += new TextDecoder().decode(next.value);
  }
  const match = startup.match(/^url: (http:\/\/127\.0\.0\.1:\d+\/)#([^\s]+)\n$/);
  if (!match) throw new Error(`unexpected dashboard startup: ${startup}`);
  const base = new URL(match[1]);
  const token = match[2];
  const substitute = (value) => value.replaceAll("{project}", f.project).replaceAll("{link}", linkDir);
  server = Bun.serve({
    hostname: "127.0.0.1", port: Number(portText),
    async fetch(request) {
      const input = new URL(request.url);
      if (input.pathname === "/__observations") {
        const result = await observations(f, executable);
        return new Response(result.text.replaceAll(work, "[CWD]"), { headers: { "content-type": "application/json" } });
      }
      const headers = new Headers(request.headers);
      headers.delete("host");
      if (headers.get("x-dashboard-token") === "fixture-valid-token") headers.set("x-dashboard-token", token);
      if (headers.get("origin") === "http://fixture.local") headers.set("origin", base.origin);
      const dest = new URL(input.pathname + input.search, base);
      for (const [key, value] of dest.searchParams) dest.searchParams.set(key, substitute(value));
      const body = ["GET", "HEAD"].includes(request.method) ? undefined : substitute(await request.text());
      const response = await fetch(dest, { method: request.method, headers, body, redirect: "manual" });
      const bytes = await response.arrayBuffer();
      const outputHeaders = new Headers(response.headers);
      outputHeaders.delete("content-length");
      const contentType = outputHeaders.get("content-type") ?? "";
      const output = contentType.includes("text") || contentType.includes("json")
        ? new TextDecoder().decode(bytes).replaceAll(work, "[CWD]") : bytes;
      return new Response(request.method === "HEAD" ? null : output, { status: response.status, headers: outputHeaders });
    },
  });
  await stopped.promise;
} finally {
  server?.stop(true);
  if (child) {
    signalChild("SIGTERM");
    const deadline = setTimeout(() => signalChild("SIGKILL"), 5000);
    try { await child.exited; }
    finally { clearTimeout(deadline); signalChild("SIGKILL"); }
    await reader?.cancel();
  }
  if (f) await cleanup(f);
  if (work) await rm(work, { recursive: true });
  process.off("SIGTERM", onStop);
  process.off("SIGINT", onStop);
}
