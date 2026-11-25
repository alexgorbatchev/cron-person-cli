import { mkdir, readdir, lstat, rm, chmod, readlink } from "node:fs/promises";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";

export const cronText = '# Fixture\nGREETING="hello world"\n* * * * * printf "%s\\n" "$GREETING"\n@daily echo daily\n';
export const taskID = (dir, command) => createHash("sha256").update(`${dir}\0${command}`).digest("hex").slice(0, 12);

export async function write(path, text, mode = 0o600) {
  await mkdir(resolve(path, ".."), { recursive: true, mode: 0o700 });
  await Bun.write(path, text);
  await chmod(path, mode);
}

export async function fixture(work, scenario = "empty") {
  const root = join(work, "fixture");
  await mkdir(root, { mode: 0o700 });
  const home = join(root, "home");
  const project = join(home, "project");
  const state = join(root, "state");
  const config = join(root, "config");
  const store = join(state, "cron-person", "allowed.json");
  await mkdir(project, { recursive: true, mode: 0o700 });
  await write(join(project, ".cronrc"), cronText);
  const approved = ["allowed", "changed", "invalid", "missing", "orphan", "config-invalid", "history"].includes(scenario);
  if (approved) {
    await write(store, JSON.stringify([{ dir: project, path: join(project, ".cronrc"), hash: createHash("sha256").update(cronText).digest("hex"), allowed_at: "2026-01-01T00:00:00Z" }], null, 2) + "\n");
  }
  if (scenario === "changed") await write(join(project, ".cronrc"), cronText + "# changed\n");
  if (scenario === "invalid") await write(join(project, ".cronrc"), "bad schedule\n");
  if (["missing", "no-file"].includes(scenario)) await rm(join(project, ".cronrc"));
  if (scenario === "orphan") await rm(project, { recursive: true });
  if (scenario === "corrupt-store") await write(store, "not json\n");
  if (scenario === "config-invalid") await write(join(config, "cron-person", "config.json"), '{"unknown":true}\n');
  await write(join(root, "crontab"), "# unrelated user entry\n0 0 * * * echo existing\n");
  // Never let characterization reach the user's real crontab executable.
  await write(join(root, "bin", "crontab"), '#!/bin/sh\nif [ "$1" = "-l" ]; then cat "$FIXTURE_CRONTAB"; else cat "$1" > "$FIXTURE_CRONTAB"; fi\n', 0o700);
  if (scenario === "history") {
    const command = "echo recorded";
    const id = taskID(project, command);
    const run = "20260101T000000.000000Z-01020304";
    const dir = join(state, "cron-person", "runs", id);
    await write(join(dir, "task.json"), JSON.stringify({ dir: project, command }) + "\n");
    await write(join(dir, run, "meta.json"), JSON.stringify({ id: run, task_id: id, dir: project, command, trigger: "manual", status: "succeeded", started_at: "2026-01-01T00:00:00Z", finished_at: "2026-01-01T00:00:01Z", exit_code: 0 }) + "\n");
    await write(join(dir, run, "output.log"), "[out] first\n[err] second\n");
    await write(join(dir, run, "run.lock"), "");
  }
  return { root, project, home, store, env: { ...process.env, HOME: home, USERPROFILE: home, XDG_STATE_HOME: state, XDG_CONFIG_HOME: config, SHELL: "/bin/sh", TZ: "UTC", PATH: `${join(root, "bin")}:${process.env.PATH}`, FIXTURE_CRONTAB: join(root, "crontab") } };
}

// Validate derived identities before replacing invocation-dependent values in observations.
export async function observations(f, executable) {
  const identities = new Map();
  const entries = [];
  async function walk(path) {
    for (const name of (await readdir(path)).sort()) {
      const full = join(path, name);
      const stat = await lstat(full);
      const item = { path: full.slice(f.root.length + 1), mode: stat.mode & 0o777, type: stat.isDirectory() ? "dir" : stat.isSymbolicLink() ? "symlink" : "file" };
      if (stat.isDirectory()) await walk(full);
      else if (stat.isSymbolicLink()) item.target = await readlink(full);
      else {
        const bytes = new Uint8Array(await Bun.file(full).arrayBuffer());
        const text = new TextDecoder("utf-8", { fatal: true });
        try { item.text = text.decode(bytes); } catch { item.base64 = Buffer.from(bytes).toString("base64"); }
        if (name === "task.json") {
          const task = JSON.parse(item.text);
          const id = taskID(task.dir, task.command);
          if (!full.endsWith(`/${id}/task.json`)) throw new Error("incorrect task directory identity");
          identities.set(id, `<task:${task.command}>`);
        }
        if (name === "meta.json") {
          const run = JSON.parse(item.text);
          if (run.task_id !== taskID(run.dir, run.command)) throw new Error("incorrect recorded task identity");
          if (!full.endsWith(`/${run.task_id}/${run.id}/meta.json`)) throw new Error("incorrect run directory identity");
          if (!/^\d{8}T\d{6}\.\d{6}Z-[a-f0-9]{8}$/.test(run.id)) throw new Error("incorrect run identifier format");
          identities.set(run.task_id, `<task:${run.command}>`);
          identities.set(run.id, "<run-id>");
        }
      }
      entries.push(item);
    }
  }
  await walk(f.root);
  const normalize = (value) => {
    let text = value.replaceAll(executable, "[EXE]");
    for (const [id, label] of identities) text = text.replaceAll(id, label);
    return text;
  };
  const normalizeBytes = (value) => {
    let bytes = Buffer.from(value);
    for (const [before, after] of [[executable, "[EXE]"], ...identities]) {
      const needle = Buffer.from(before);
      const replacement = Buffer.from(after);
      const parts = [];
      let start = 0;
      let index;
      while ((index = bytes.indexOf(needle, start)) !== -1) {
        parts.push(bytes.subarray(start, index), replacement);
        start = index + needle.length;
      }
      if (start) bytes = Buffer.concat([...parts, bytes.subarray(start)]);
    }
    return bytes;
  };
  return { text: normalize(JSON.stringify(entries, null, 2) + "\n"), normalize, normalizeBytes };
}

export async function cleanup(f) { await rm(f.root, { recursive: true }); }
