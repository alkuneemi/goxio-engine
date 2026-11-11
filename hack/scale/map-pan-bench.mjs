// Frame-time benchmark for the GoXIO map view.
// Serves nothing itself: point it at the bench page (vite dev or preview).
//
//   node hack/scale/map-pan-bench.mjs http://127.0.0.1:4173/bench.html
//
// Writes a JSON report to stdout. Set OUT=/path/file.json to also save it.
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../webui/package.json', import.meta.url));
const { chromium } = require('playwright');

const URL_ = process.argv[2] ?? 'http://127.0.0.1:4173/bench.html';

function summarizeProfile(profile) {
  const self = new Map();
  const nodes = profile.nodes;
  for (let i = 0; i < profile.samples.length; i++) {
    const id = profile.samples[i];
    const dt = profile.timeDeltas[i] || 0;
    const node = nodes[id];
    if (!node) continue;
    const frame = node.callFrame || {};
    const name = frame.functionName || '(anonymous)';
    const url = (frame.url || '').split('/').slice(-2).join('/');
    const key = url ? `${name}  ${url}:${frame.lineNumber}` : name;
    self.set(key, (self.get(key) || 0) + dt);
  }
  const total = [...self.values()].reduce((a, b) => a + b, 0) || 1;
  return [...self.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 18)
    .map(([name, us]) => ({
      name,
      selfMs: Math.round(us / 1000),
      pct: Math.round((us / total) * 1000) / 10,
    }));
}

function frameStats(frames) {
  if (!frames.length) return { frames: 0 };
  const sorted = [...frames].sort((a, b) => a - b);
  const sum = frames.reduce((a, b) => a + b, 0);
  const at = (p) => sorted[Math.min(sorted.length - 1, Math.max(0, Math.ceil(p * sorted.length) - 1))];
  const over = (ms) => frames.filter((f) => f >= ms).length;
  return {
    frames: frames.length,
    durationMs: Math.round(sum),
    fps: Math.round((frames.length / sum) * 1000),
    avgMs: Math.round((sum / frames.length) * 100) / 100,
    p50Ms: Math.round(at(0.5) * 100) / 100,
    p95Ms: Math.round(at(0.95) * 100) / 100,
    maxMs: Math.round(sorted[sorted.length - 1] * 100) / 100,
    jankOver32ms: over(32),
    jankOver50ms: over(50),
    longFramesOver100ms: over(100),
  };
}

async function metricSnapshot(session) {
  const { metrics } = await session.send('Performance.getMetrics');
  const want = ['TaskDuration', 'ScriptDuration', 'LayoutDuration', 'RecalcStyleDuration', 'LayoutCount', 'RecalcStyleCount', 'JSHeapUsedSize'];
  const out = {};
  for (const m of metrics) {
    if (want.includes(m.name)) out[m.name] = m.value;
  }
  return out;
}

function metricDelta(before, after) {
  const out = {};
  for (const key of Object.keys(after)) {
    const d = after[key] - (before[key] ?? 0);
    out[key] = key.endsWith('Size') ? Math.round(d / 1048576) : Math.round(d * 1000) / 1000;
  }
  return out;
}

async function withFrames(page, gesture) {
  await page.evaluate(() => {
    window.__frames = [];
    window.__lastFrame = performance.now();
    const tick = (now) => {
      window.__frames.push(now - window.__lastFrame);
      window.__lastFrame = now;
      window.__raf = requestAnimationFrame(tick);
    };
    window.__raf = requestAnimationFrame(tick);
  });
  await gesture();
  // One frame so the last interval is closed after the gesture returns.
  await page.evaluate(() => new Promise((resolve) => {
    requestAnimationFrame(() => {
      cancelAnimationFrame(window.__raf);
      resolve();
    });
  }));
  const frames = await page.evaluate(() => window.__frames.slice(1));
  return frameStats(frames);
}

async function profiled(session, page, gesture) {
  const before = await metricSnapshot(session);
  await session.send('Profiler.enable');
  await session.send('Profiler.start');
  const frames = await withFrames(page, gesture);
  const { profile } = await session.send('Profiler.stop');
  await session.send('Profiler.disable');
  const after = await metricSnapshot(session);
  return { frames, metrics: metricDelta(before, after), hot: summarizeProfile(profile) };
}

const browser = await chromium.launch({
  args: ['--enable-precise-memory-info'],
});
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const session = await page.context().newCDPSession(page);
await session.send('Performance.enable');
if (process.env.CPU) {
  await session.send('Emulation.setCPUThrottlingRate', { rate: Number(process.env.CPU) });
}

try {
  await page.addInitScript(() => {
    window.__longTasks = [];
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        window.__longTasks.push({ start: Math.round(entry.startTime), ms: Math.round(entry.duration) });
      }
    }).observe({ type: 'longtask', buffered: true });
  });

  const t0 = Date.now();
  await page.goto(URL_, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('.react-flow__node', { timeout: 120000 });
  const firstNodeMs = Date.now() - t0;
  // Fit-view animation is 500ms. Let the viewport settle before gesturing.
  await page.waitForTimeout(1600);

  const dom = await page.evaluate(() => {
    const labels = [...document.querySelectorAll('.react-flow__edge text')].map((n) => n.textContent);
    const dashes = [...document.querySelectorAll('.react-flow__edge path')].filter((p) => {
      const dash = getComputedStyle(p).strokeDasharray;
      return dash && dash !== 'none';
    }).length;
    const box = document.querySelector('.react-flow__viewport')?.getBoundingClientRect();
    return {
      stats: window.__BENCH_STATS,
      domNodes: document.querySelectorAll('.react-flow__node').length,
      domEdges: document.querySelectorAll('.react-flow__edge').length,
      edgeLabels: labels.length,
      containsLabels: labels.filter((t) => (t || '').includes('CONTAINS')).length,
      dashedPaths: dashes,
      images: document.querySelectorAll('.react-flow__node img').length,
      clusters: document.querySelectorAll('[data-cluster="true"]').length,
      domAll: document.getElementsByTagName('*').length,
      viewport: box ? { w: Math.round(box.width), h: Math.round(box.height) } : null,
      zoom: document.querySelector('.react-flow__viewport')?.style?.transform || '',
    };
  });

  const pane = page.locator('.react-flow__pane');
  const box = await pane.boundingBox();
  if (!box) throw new Error('pane has no box');
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;

  const pan = await profiled(session, page, async () => {
    await page.mouse.move(cx, cy);
    await page.mouse.down();
    for (let i = 0; i < 50; i++) {
      const dx = Math.sin(i / 5) * 320;
      const dy = Math.cos(i / 7) * 180;
      await page.mouse.move(cx + dx, cy + dy, { steps: 3 });
    }
    await page.mouse.up();
  });

  const zoom = await profiled(session, page, async () => {
    await page.mouse.move(cx, cy);
    for (let i = 0; i < 24; i++) {
      await page.mouse.wheel(0, i < 12 ? -140 : 140);
      await page.waitForTimeout(40);
    }
  });

  // Second pan after the wheel, still around the fitted (zoomed-out) scene
  // if the wheel returned, plus whatever zoom landed on.
  const panAfterZoom = await profiled(session, page, async () => {
    await page.mouse.move(cx, cy);
    await page.mouse.down();
    for (let i = 0; i < 40; i++) {
      const dx = Math.sin(i / 4) * 260;
      const dy = Math.cos(i / 6) * 140;
      await page.mouse.move(cx + dx, cy + dy, { steps: 3 });
    }
    await page.mouse.up();
  });

  const forceLayoutMs = Math.round(await page.evaluate(() => window.__timeForceLayout()));
  const longTasks = await page.evaluate(() => window.__longTasks);

  const report = {
    url: URL_,
    firstNodeMs,
    forceLayoutMs,
    longTasksDuringLoad: longTasks.filter((t) => t.start < firstNodeMs + 2000),
    dom,
    pan,
    zoom,
    panAfterZoom,
  };
  const json = JSON.stringify(report, null, 2);
  console.log(json);
  if (process.env.OUT) {
    const fs = await import('node:fs');
    fs.writeFileSync(process.env.OUT, json);
  }
  if (process.env.SHOT) {
    await page.screenshot({ path: process.env.SHOT });
  }
} finally {
  await browser.close();
}
