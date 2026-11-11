import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '../index.css';
import { GraphCanvas } from '../components/graph';
import { calculateForceDirectedLayout } from '../utils/forceDirectedLayout';
import { buildKsaGraph, buildScaleGraph, scaleGraphStats } from './syntheticGraph';

const profile = new URLSearchParams(window.location.search).get('profile');
const graph = profile === 'ksa' ? buildKsaGraph() : buildScaleGraph();
const stats = scaleGraphStats(graph);

declare global {
  interface Window {
    __BENCH_STATS: typeof stats;
    __timeForceLayout: () => number;
  }
}

window.__BENCH_STATS = stats;
window.__timeForceLayout = () => {
  const t = performance.now();
  calculateForceDirectedLayout(graph, { width: 1440, height: 900 });
  return performance.now() - t;
};

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <div style={{ width: '100%', height: '100%' }} data-benchmark="map" data-nodes={stats.nodes} data-edges={stats.edges} data-hub={stats.hubChildren}>
      <GraphCanvas
        graph={graph}
        selectedNodeId={null}
        onNodeSelect={() => {}}
        layoutMode="force"
      />
    </div>
  </StrictMode>,
);
