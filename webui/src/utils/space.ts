import type { Graph, GraphNode, SpaceActivity } from '../types';

export const SPACE_ACTIVITIES: SpaceActivity[] = ['daily', 'intermittent', 'idle', 'core'];

const SPACE_TYPES = new Set(['disk', 'volume', 'container', 'database', 'repo', 'github_account']);

export function isSpaceNode(node: GraphNode): boolean {
  if (typeof node.metadata?.collector === 'string') return true;
  return node.id.startsWith('space-') && SPACE_TYPES.has(node.type);
}

export function formatBytes(value: unknown): string {
  const n = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN;
  if (!Number.isFinite(n) || n < 0) return '';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  if (i === 0) return `${Math.round(v)} ${units[i]}`;
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

export function nodeSizeLabel(node: GraphNode): string {
  const human = node.metadata?.size;
  if (typeof human === 'string' && human.trim()) return human;
  return formatBytes(node.metadata?.size_bytes);
}

export function nodeActivity(node: GraphNode): SpaceActivity | '' {
  const a = node.metadata?.activity;
  if (a === 'daily' || a === 'intermittent' || a === 'idle' || a === 'core') return a;
  return '';
}

export function isCoreSpace(node: GraphNode): boolean {
  return node.metadata?.core === true || node.metadata?.activity === 'core';
}

export function pressureValue(node: GraphNode, key: string): number | null {
  const v = node.metadata?.[key];
  if (typeof v !== 'number' || !Number.isFinite(v)) return null;
  return v;
}

function bySize(a: GraphNode, b: GraphNode): number {
  const as = typeof a.metadata?.size_bytes === 'number' ? a.metadata.size_bytes : 0;
  const bs = typeof b.metadata?.size_bytes === 'number' ? b.metadata.size_bytes : 0;
  if (bs !== as) return bs - as;
  return a.name.localeCompare(b.name);
}

export function spaceChildren(graph: Graph, id: string | null): GraphNode[] {
  const nodes = graph.nodes.filter(isSpaceNode);
  if (!id) return nodes.filter(n => n.type === 'disk').sort(bySize);
  const node = nodes.find(n => n.id === id);
  if (!node) return [];
  if (node.type === 'database') {
    const repo = node.metadata?.repo;
    const repoName = node.metadata?.repo_name;
    return nodes.filter(n => n.type === 'repo' && (n.id === repo || n.name === repo || n.name === repoName)).sort(bySize);
  }
  if (node.type === 'repo') {
    const account = node.metadata?.github_account;
    return nodes.filter(n => n.type === 'github_account' && (n.name === account || n.id === account));
  }
  const kids = nodes.filter(n => n.parent === id);
  if (kids.length) return kids.sort(bySize);
  const next = new Set(
    graph.edges
      .filter(e => e.source === id && (e.type === 'contains' || e.type === 'owns' || e.type === 'deploys_from'))
      .map(e => e.target),
  );
  return nodes.filter(n => next.has(n.id)).sort(bySize);
}

export function ancestorIds(graph: Graph, id: string): string[] {
  const byId = new Map(graph.nodes.map(n => [n.id, n]));
  const chain: string[] = [];
  let cur = byId.get(id);
  const seen = new Set<string>();
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    chain.push(cur.id);
    cur = cur.parent ? byId.get(cur.parent) : undefined;
  }
  return chain.reverse();
}

export const LEVEL_LABEL: Record<string, string> = {
  disk: 'Volumes',
  volume: 'Containers',
  container: 'Databases',
  database: 'Repos',
  repo: 'GitHub accounts',
  github_account: 'Owned repos',
};
