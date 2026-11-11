import type { Graph, GraphEdge, GraphNode, HealthStatus, NodeType } from '../types';
import { worstHealth } from './graphView';

/** Collapse a parent's children when more than this many share a kind. */
export const FANOUT_CLUSTER_ABOVE = 50;

/** How many members an expanded cluster reveals before "show more". */
export const CLUSTER_PAGE = 40;

export interface FanoutClusterInfo {
  key: string;
  parentId: string;
  kind: string;
  nodeType: NodeType;
  count: number;
  memberIds: string[];
}

export interface FanoutResult {
  graph: Graph;
  clusters: FanoutClusterInfo[];
  /** Every clustered member, including ones currently expanded. */
  memberCluster: Map<string, string>;
}

const EMPTY_HEALTH: Record<HealthStatus, number> = {
  healthy: 0,
  degraded: 0,
  unhealthy: 0,
  unknown: 0,
};

export function nodeKind(node: GraphNode): string {
  const kind = node.metadata?.kind;
  if (typeof kind === 'string' && kind) return kind;
  return node.type;
}

function plural(kind: string, count: number): string {
  if (count === 1) return kind;
  if (kind.endsWith('s')) return kind;
  return `${kind}s`;
}

function canCluster(node: GraphNode): boolean {
  if (node.metadata?.cluster === true) return false;
  if (node.metadata?.group === true) return false;
  if (node.type === 'namespace') return false;
  return true;
}

function memberSize(node: GraphNode): number {
  const raw = node.metadata?.size_bytes;
  if (typeof raw === 'number' && Number.isFinite(raw)) return raw;
  if (typeof raw === 'string') {
    const parsed = Number(raw);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function compareMembers(a: GraphNode, b: GraphNode): number {
  const bySize = memberSize(b) - memberSize(a);
  if (bySize !== 0) return bySize;
  const byName = a.name.localeCompare(b.name);
  if (byName !== 0) return byName;
  return a.id.localeCompare(b.id);
}

/**
 * Replace a huge fan-out (a disk with hundreds of CONTAINS edges, for example)
 * with one cluster node per child kind. Expanding a key reveals the largest
 * members a page at a time, parented to the cluster so they can sit around it.
 * The remainder is a single "show more (N)" node. Descendants of a collapsed
 * member (a container on a hidden volume) hide too.
 */
export function collapseFanout(
  graph: Graph,
  expanded: ReadonlySet<string>,
  threshold = FANOUT_CLUSTER_ABOVE,
  shown?: ReadonlyMap<string, number>,
  focusId?: string | null,
): FanoutResult {
  const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
  const childrenOf = new Map<string, GraphNode[]>();
  const seenChild = new Set<string>();

  const addChild = (parentId: string, child: GraphNode) => {
    if (!nodeById.has(parentId) || parentId === child.id) return;
    const mark = `${parentId}\0${child.id}`;
    if (seenChild.has(mark)) return;
    seenChild.add(mark);
    let list = childrenOf.get(parentId);
    if (!list) {
      list = [];
      childrenOf.set(parentId, list);
    }
    list.push(child);
  };

  for (const node of graph.nodes) {
    if (node.parent) addChild(node.parent, node);
  }
  for (const edge of graph.edges) {
    if (edge.type !== 'contains') continue;
    const child = nodeById.get(edge.target);
    if (child) addChild(edge.source, child);
  }

  const clusters: FanoutClusterInfo[] = [];
  const memberCluster = new Map<string, string>();
  const hidden = new Set<string>();
  const pinnedVisible = new Set<string>();
  const moved = new Set<string>();
  const suppressContains = new Set<string>();
  const extraNodes: GraphNode[] = [];
  const revealed: GraphNode[] = [];
  const extraEdges: GraphEdge[] = [];

  for (const [parentId, children] of childrenOf) {
    const groups = new Map<string, GraphNode[]>();
    for (const child of children) {
      if (!canCluster(child)) continue;
      const kind = nodeKind(child);
      let group = groups.get(kind);
      if (!group) {
        group = [];
        groups.set(kind, group);
      }
      group.push(child);
    }

    for (const [kind, members] of groups) {
      if (members.length <= threshold) continue;
      const key = `cluster:${parentId}:${kind}`;
      const info: FanoutClusterInfo = {
        key,
        parentId,
        kind,
        nodeType: members[0].type,
        count: members.length,
        memberIds: members.map(member => member.id),
      };
      clusters.push(info);
      const counts: Record<HealthStatus, number> = { ...EMPTY_HEALTH };
      for (const member of members) {
        memberCluster.set(member.id, key);
        counts[member.health] += 1;
        suppressContains.add(`${parentId}\0${member.id}`);
      }

      extraNodes.push({
        id: key,
        type: members[0].type,
        name: `${members.length} ${plural(kind, members.length)}`,
        parent: parentId,
        health: worstHealth(counts, 'unknown'),
        metadata: {
          cluster: true,
          clusterKey: key,
          clusterCount: members.length,
          clusterKind: kind,
          clusterExpanded: expanded.has(key),
        },
      });
      extraEdges.push({
        id: `edge:${key}`,
        source: parentId,
        target: key,
        type: 'contains',
        label: 'CONTAINS',
      });

      if (expanded.has(key)) {
        const sorted = [...members].sort(compareMembers);
        const limit = shown?.get(key) ?? CLUSTER_PAGE;
        const visible = sorted.slice(0, Math.min(limit, sorted.length));
        if (focusId) {
          const focus = sorted.find(member => member.id === focusId);
          if (focus && !visible.some(member => member.id === focus.id)) visible.push(focus);
        }
        const visibleIds = new Set(visible.map(member => member.id));
        for (const member of members) {
          if (!visibleIds.has(member.id)) hidden.add(member.id);
        }
        for (const member of visible) {
          pinnedVisible.add(member.id);
          moved.add(member.id);
          revealed.push({ ...member, parent: key });
          extraEdges.push({
            id: `edge:${key}:${member.id}`,
            source: key,
            target: member.id,
            type: 'contains',
            label: 'CONTAINS',
          });
        }
        const remaining = sorted.length - visible.length;
        if (remaining > 0) {
          const moreId = `${key}:more`;
          extraNodes.push({
            id: moreId,
            type: members[0].type,
            name: `show more (${remaining})`,
            parent: key,
            health: 'unknown',
            metadata: {
              showMore: true,
              clusterKey: key,
              remaining,
            },
          });
          extraEdges.push({
            id: `edge:${moreId}`,
            source: key,
            target: moreId,
            type: 'contains',
            label: 'CONTAINS',
          });
        }
      } else {
        for (const member of members) hidden.add(member.id);
      }
    }
  }

  if (clusters.length === 0) {
    return { graph, clusters, memberCluster };
  }

  // Containers, databases, and anything else that only hangs off a hidden
  // member disappear with it. A node stays if any contains-parent is visible.
  const containsParents = new Map<string, string[]>();
  for (const edge of graph.edges) {
    if (edge.type !== 'contains') continue;
    let parents = containsParents.get(edge.target);
    if (!parents) {
      parents = [];
      containsParents.set(edge.target, parents);
    }
    parents.push(edge.source);
  }

  let grew = true;
  while (grew) {
    grew = false;
    for (const node of graph.nodes) {
      if (hidden.has(node.id) || pinnedVisible.has(node.id)) continue;
      if (node.parent && hidden.has(node.parent)) {
        hidden.add(node.id);
        grew = true;
        continue;
      }
      const parents = containsParents.get(node.id);
      if (parents && parents.length > 0 && parents.every(parentId => hidden.has(parentId))) {
        hidden.add(node.id);
        grew = true;
      }
    }
  }

  const nodes = graph.nodes.filter(node => !hidden.has(node.id) && !moved.has(node.id)).concat(revealed, extraNodes);
  const edges = graph.edges.filter(edge => {
    if (hidden.has(edge.source) || hidden.has(edge.target)) return false;
    if (edge.type === 'contains' && suppressContains.has(`${edge.source}\0${edge.target}`)) return false;
    return true;
  }).concat(extraEdges);

  return { graph: { nodes, edges }, clusters, memberCluster };
}
