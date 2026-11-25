import { run } from "./cli-run.mjs";

const [target, ...args] = process.argv.slice(2);
// During migration either executable can generate commands naming itself.
// Normalize only these two build identities; task identities remain validated.
const original = Bun.fileURLToPath(new URL("../../bin/cron-person-go-reference", import.meta.url));
await run(target, args, [original]);
