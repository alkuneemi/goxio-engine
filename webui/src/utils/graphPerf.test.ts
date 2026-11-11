import type { Graph, GraphNode } from '../types';

function assertEqual(actual: unknown, expected: unknown, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)} but got ${String(actual)}`);
  }
}
import { CLUSTER_PAGE, collapseFanout, FANOUT_CLUSTER_ABOVE } from './fanoutCluster';
import { COMPACT_EDGE_AT, COMPACT_ZOOM_BELOW, countVisibleEdges, detailLevel, FULL_DETAIL_ZOOM, type NodeBox, type ViewRect } from './graphDetail';
import { placeMissingNodes } from './forceDirectedLayout';

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
assertEqual(detailLevel(0.7, COMPACT_EDGE_AT), 'compact', 'many visible edges');
assertEqual(detailLevel(0.7, COMPACT_EDGE_AT - 1), 'full', 'few visible edges');
assertEqual(detailLevel(FULL_DETAIL_ZOOM, 1168), 'full', 'high zoom shows labels');
assertEqual(detailLevel(FULL_DETAIL_ZOOM - 0.01, 1168), 'compact', 'just below high zoom stays compact');

const view: ViewRect = { left: 0, top: 0, right: 200, bottom: 200 };
const boxes = new Map<string, NodeBox>([
  ['a', { x: 10, y: 10, w: 40, h: 20 }],
  ['b', { x: 80, y: 40, w: 40, h: 20 }],
  ['c', { x: 800, y: 800, w: 40, h: 20 }],
  ['d', { x: 900, y: 40, w: 40, h: 20 }],
]);
assertEqual(countVisibleEdges(
  [{ source: 'a', target: 'b' }, { source: 'c', target: 'd' }],
  boxes,
  view,
), 1, 'only the edge inside the viewport counts');
assertEqual(countVisibleEdges(
  [{ source: 'c', target: 'a' }],
  boxes,
  view,
), 1, 'an edge that enters the viewport counts');

function sizedFan(count: number) {
  const graph = fan(count, 'folder');
  for (const node of graph.nodes) {
    const match = /^folder-(\d+)$/.exec(node.id);
    if (match) node.metadata.size_bytes = Number(match[1]);
  }
  return graph;
}

const many = sizedFan(418);
const manyKey = collapseFanout(many, new Set()).clusters[0].key;
const page = collapseFanout(many, new Set([manyKey]));
const pageMembers = page.graph.nodes.filter(node => node.id.startsWith('folder-'));
assertEqual(pageMembers.length, CLUSTER_PAGE, 'first page size');
assertEqual(pageMembers.some(node => node.id === 'folder-417'), true, 'largest folder is shown');
assertEqual(pageMembers.some(node => node.id === 'folder-0'), false, 'smallest folder stays hidden');
assertEqual(page.graph.nodes.some(node => node.name === 'show more (378)'), true, 'show more count');
assertEqual(page.graph.nodes.filter(node => node.id.startsWith('folder-')).length < 418, true, 'page is not the whole cluster');

const second = collapseFanout(many, new Set([manyKey]), FANOUT_CLUSTER_ABOVE, new Map([[manyKey, CLUSTER_PAGE * 2]]));
assertEqual(second.graph.nodes.filter(node => node.id.startsWith('folder-')).length, CLUSTER_PAGE * 2, 'second page');
assertEqual(second.graph.nodes.some(node => node.name === 'show more (338)'), true, 'show more after the second page');

const focused = collapseFanout(many, new Set([manyKey]), FANOUT_CLUSTER_ABOVE, undefined, 'folder-0');
assertEqual(focused.graph.nodes.some(node => node.id === 'folder-0'), true, 'focused member is revealed');
assertEqual(focused.graph.nodes.filter(node => node.id.startsWith('folder-')).length, CLUSTER_PAGE + 1, 'focus adds one member');

const ring = new Map<string, { x: number; y: number }>([['cluster', { x: 0, y: 0 }]]);
placeMissingNodes(
  [{ id: 'cluster' }, ...Array.from({ length: 8 }, (_, index) => ({ id: `m${index}`, parent: 'cluster' }))],
  ring,
);
let left = 0;
let right = 0;
let aboveParent = 0;
let belowParent = 0;
for (let index = 0; index < 8; index++) {
  const point = ring.get(`m${index}`);
  if (!point) throw new Error(`missing ring position m${index}`);
  if (point.x < -50) left += 1;
  if (point.x > 50) right += 1;
  if (point.y < -50) aboveParent += 1;
  if (point.y > 50) belowParent += 1;
}
assertEqual(left > 0 && right > 0 && aboveParent > 0 && belowParent > 0, true, 'members ring the cluster');

console.log('graph perf checks ok');
