import tailwind from 'bun-plugin-tailwind';
import { rm } from 'node:fs/promises';
await rm('../cmd/cron-person/dashboard-assets', { recursive: true, force: true });
const result = await Bun.build({
  entrypoints: ['./index.html'],
  outdir: '../cmd/cron-person/dashboard-assets',
  plugins: [tailwind],
  minify: true,
});
if (!result.success) throw new AggregateError(result.logs, 'Dashboard build failed');

// Retain dependency notices in the embedded distribution alongside the bundle.
const notices: string[] = [];
const licenses = new Bun.Glob('**/{LICENSE*,license*,LICENCE*,COPYING*,THIRD_PARTY_NOTICES*}');
for await (const path of licenses.scan({ cwd: './node_modules', followSymlinks: true })) {
  notices.push(`\n--- ${path} ---\n${await Bun.file(`./node_modules/${path}`).text()}`);
}
await Bun.write('../cmd/cron-person/dashboard-assets/THIRD_PARTY_NOTICES.txt', notices.sort().join('\n'));
