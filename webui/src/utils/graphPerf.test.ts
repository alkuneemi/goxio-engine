import type { Graph, GraphNode } from '../types';

function assertEqual(actual: unknown, expected: unknown, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)} but got ${String(actual)}`);
  }
}
import { collapseFanout, FANOUT_CLUSTER_ABOVE } from './fanoutCluster';
import { COMPACT_EDGE_AT, COMPACT_ZOOM_BELOW, detailLevel } from './graphDetail';

function child(id: string, kind: string, parent = 'disk'): GraphNode {
  return {
    id,
    type: 'volume',
    name: id,
    parent,
    health: id.endsWith('0') ? 'degraded' : 'healthy',
    metadata: { kind },
  };
}

function fan(count: number, kind: string, parent = 'disk'): Graph {
  const nodes: GraphNode[] = [
    { id: parent, type: 'disk', name: parent, health: 'healthy', metadata: { kind: 'mount' } },
  ];
  const edges = [];
  for (let i = 0; i < count; i++) {
    const id = `${kind}-${i}`;
    nodes.push(child(id, kind, parent));
    edges.push({ id: `e-${kind}-${i}`, source: parent, target: id, type: 'contains', label: 'CONTAINS' });
  }
  return { nodes, edges };
}

const above = FANOUT_CLUSTER_ABOVE + 1;
const at = FANOUT_CLUSTER_ABOVE;

const collapsed = collapseFanout(fan(above, 'volume'), new Set());
assertEqual(collapsed.clusters.length, 1, 'one cluster');
assertEqual(collapsed.clusters[0].count, above, 'cluster count');
assertEqual(collapsed.graph.nodes.some(node => node.metadata.cluster === true), true, 'cluster node');
assertEqual(collapsed.graph.nodes.some(node => node.id === 'volume-1'), false, 'member hidden');
assertEqual(collapsed.graph.edges.some(edge => edge.target === 'volume-1'), false, 'member edge hidden');
assertEqual(collapsed.graph.edges.filter(edge => edge.source === 'disk').length, 1, 'hub has one edge');
assertEqual(collapsed.memberCluster.get('volume-3'), collapsed.clusters[0].key, 'member index');

const kept = collapseFanout(fan(at, 'volume'), new Set());
assertEqual(kept.clusters.length, 0, 'threshold is exclusive');
assertEqual(kept.graph.nodes.length, at + 1, 'uncollapsed size');

const mixed = collapseFanout((() => {
  const volumes = fan(above, 'volume');
  const images = fan(above, 'image');
  return {
    nodes: [...volumes.nodes, ...images.nodes.filter(node => node.id !== 'disk')],
    edges: [...volumes.edges, ...images.edges],
  };
})(), new Set());
assertEqual(mixed.clusters.length, 2, 'kinds cluster separately');
assertEqual(mixed.graph.nodes.filter(node => node.metadata.cluster === true).length, 2, 'two cluster nodes');

const withDescendant = fan(above, 'volume');
withDescendant.nodes.push({
  id: 'ctr',
  type: 'container',
  name: 'ctr',
  parent: 'volume-1',
  health: 'healthy',
  metadata: {},
});
withDescendant.edges.push({ id: 'e-ctr', source: 'volume-1', target: 'ctr', type: 'contains', label: 'CONTAINS' });
const hiddenDesc = collapseFanout(withDescendant, new Set());
assertEqual(hiddenDesc.graph.nodes.some(node => node.id === 'ctr'), false, 'descendant hidden');

const key = collapsed.clusters[0].key;
const opened = collapseFanout(fan(above, 'volume'), new Set([key]));
assertEqual(opened.graph.nodes.some(node => node.id === 'volume-1'), true, 'expanded member');
assertEqual(opened.graph.nodes.some(node => node.id === key), true, 'cluster stays');
assertEqual(opened.graph.edges.some(edge => edge.source === 'disk' && edge.target === 'volume-1'), false, 'hub edge stays collapsed');
assertEqual(opened.graph.edges.some(edge => edge.source === key && edge.target === 'volume-1'), true, 'cluster fans out');
assertEqual(opened.graph.nodes.find(node => node.id === key)?.metadata.clusterExpanded, true, 'expanded flag');

assertEqual(detailLevel(0.2, 4), 'compact', 'zoomed out');
assertEqual(detailLevel(COMPACT_ZOOM_BELOW, 4), 'full', 'normal zoom');
assertEqual(detailLevel(1, COMPACT_EDGE_AT), 'compact', 'large edge count');
assertEqual(detailLevel(1, COMPACT_EDGE_AT - 1), 'full', 'small edge count');

console.log('graph perf checks ok');
