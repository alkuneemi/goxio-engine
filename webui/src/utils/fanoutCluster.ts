import type { Graph, GraphEdge, GraphNode, HealthStatus, NodeType } from '../types';
import { worstHealth } from './graphView';

/** Collapse a parent's children when more than this many share a kind. */
export const FANOUT_CLUSTER_ABOVE = 50;

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

/**
 * Replace a huge fan-out (a disk with hundreds of CONTAINS edges, for example)
 * with one cluster node per child kind. Expanding a key puts those children
 * back, parented to the cluster so the hub itself stays a single edge.
 * Descendants of a collapsed member (a container on a hidden volume) hide too.
 */
export function collapseFanout(
  graph: Graph,
  expanded: ReadonlySet<string>,
  threshold = FANOUT_CLUSTER_ABOVE,
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
  const suppressContains = new Set<string>();
  const extraNodes: GraphNode[] = [];
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
        for (const member of members) {
          extraEdges.push({
            id: `edge:${key}:${member.id}`,
            source: key,
            target: member.id,
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
      if (hidden.has(node.id)) continue;
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

  const nodes = graph.nodes.filter(node => !hidden.has(node.id)).concat(extraNodes);
  const edges = graph.edges.filter(edge => {
    if (hidden.has(edge.source) || hidden.has(edge.target)) return false;
    if (edge.type === 'contains' && suppressContains.has(`${edge.source}\0${edge.target}`)) return false;
    return true;
  }).concat(extraEdges);

  return { graph: { nodes, edges }, clusters, memberCluster };
}
