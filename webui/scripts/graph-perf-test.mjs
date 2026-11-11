// Runs the cluster and level-of-detail checks. esbuild bundles the TypeScript
// the same way Vite does, so the test imports the real modules.
import * as esbuild from 'esbuild';
import { pathToFileURL } from 'node:url';

const tests = [
  ['../src/utils/graphPerf.test.ts', 'graph-perf-test.mjs'],
  ['../src/components/portal/serverStats.test.ts', 'server-stats-test.mjs'],
];
for (const [entry, name] of tests) {
  const outfile = new URL(`../node_modules/.cache/${name}`, import.meta.url);
  await esbuild.build({
    entryPoints: [new URL(entry, import.meta.url).pathname],
    bundle: true,
    format: 'esm',
    platform: 'node',
    outfile: outfile.pathname,
    logLevel: 'silent',
  });
  await import(`${pathToFileURL(outfile.pathname).href}?t=${Date.now()}`);
}
