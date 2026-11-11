// Runs the cluster and level-of-detail checks. esbuild bundles the TypeScript
// the same way Vite does, so the test imports the real modules.
import * as esbuild from 'esbuild';
import { pathToFileURL } from 'node:url';

const outfile = new URL('../node_modules/.cache/graph-perf-test.mjs', import.meta.url);
await esbuild.build({
  entryPoints: [new URL('../src/utils/graphPerf.test.ts', import.meta.url).pathname],
  bundle: true,
  format: 'esm',
  platform: 'node',
  outfile: outfile.pathname,
  logLevel: 'silent',
});
await import(pathToFileURL(outfile.pathname).href);
