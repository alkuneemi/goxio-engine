import { useCallback, useMemo, useEffect, useRef, useState } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  ControlButton,
  MiniMap,
  useNodesState,
  useEdgesState,
  useReactFlow,
  useStore,
  useStoreApi,
  ReactFlowProvider,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
  BackgroundVariant,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import CustomNode, { type CustomNodeData } from './CustomNode';
import CustomEdge, { type CustomEdgeData } from './CustomEdge';
import { EdgeMarkerDefs } from './EdgeMarkerDefs';
import { EmptyState } from '../ui';
import type { Graph, GraphEdge } from '../../types';
import { calculatePriority, calculateHierarchicalLayout, calculateForceDirectedLayout, calculateSwimlaneLayout, collapseFanout, debounce, detailLevel, placeMissingNodes, worstHealth, type NamespaceSummary } from '../../utils';
import { useLayoutPersistence } from '../../hooks';
import { useForceSimulation } from '../../hooks/useForceSimulation';
import type { LayoutMode } from '../HeaderBar';
import { GraphDetailContext } from './detailContext';
import styles from './GraphCanvas.module.css';

const nodeTypes: NodeTypes = {
  custom: CustomNode,
};

const edgeTypes: EdgeTypes = {
  custom: CustomEdge,
};

interface GraphCanvasProps {
  graph: Graph | undefined;
  selectedNodeId: string | null;
  onNodeSelect: (nodeId: string | null) => void;
  onEdgeClick?: (edge: GraphEdge) => void;
  layoutMode: LayoutMode;
  resetKey?: number;
  isLoading?: boolean;
  error?: Error | null;
  onRetry?: () => void;
  // Per-namespace summaries from collapseNamespaces; drive lane stats and the
  // collapsed rendering.
  namespaces?: Map<string, NamespaceSummary>;
  onToggleCollapse?: (namespaceName: string) => void;
  // Changing this refits the viewport (e.g. a new filter); see the fit effect.
  fitKey?: string;
}

// Never zoom past 1:1 when fitting, so a handful of nodes (e.g. one
// isolated namespace) doesn't fill the screen.
const FIT_VIEW = { padding: 0.15, maxZoom: 1 };

// Default card size for nodes without an explicit style size; only used to
// pad the bounds when fitting.
const FIT_NODE_W = 220;
const FIT_NODE_H = 80;

// Viewport that fits every top-level node, computed from layout positions.
// React Flow's fitView only sees nodes it has measured, and with viewport
// virtualization most nodes are never mounted, so it fit the wrong area.
function viewportForLayout(
  nodes: Node<CustomNodeData>[],
  size: { width: number; height: number },
): { x: number; y: number; zoom: number } | null {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of nodes) {
    if (n.parentId) continue; // inside a group whose box already covers it
    const w = typeof n.style?.width === 'number' ? n.style.width : FIT_NODE_W;
    const h = typeof n.style?.height === 'number' ? n.style.height : FIT_NODE_H;
    minX = Math.min(minX, n.position.x);
    minY = Math.min(minY, n.position.y);
    maxX = Math.max(maxX, n.position.x + w);
    maxY = Math.max(maxY, n.position.y + h);
  }
  if (!isFinite(minX)) return null;
  const bw = maxX - minX, bh = maxY - minY;
  const usable = 1 - 2 * FIT_VIEW.padding;
  const zoom = Math.min(FIT_VIEW.maxZoom, Math.max(MIN_ZOOM, Math.min((size.width * usable) / bw, (size.height * usable) / bh)));
  return {
    x: size.width / 2 - (minX + bw / 2) * zoom,
    y: size.height / 2 - (minY + bh / 2) * zoom,
    zoom,
  };
}

const MIN_ZOOM = 0.2;

// Past this many nodes React Flow only mounts what's inside the viewport.
// Low enough that zooming into an expanded hub does not keep every card mounted.
const VIRTUALIZE_NODES = 40;

// Pulse animations stay off for graphs this large; the health dot still shows state.
const CALM_NODES = 200;

function structureKey(
  graph: Graph,
  layoutMode: LayoutMode,
  size: { width: number; height: number },
  resetKey: number,
): string {
  let hash = graph.nodes.length * 131 + graph.edges.length * 17 + resetKey;
  for (const node of graph.nodes) {
    hash = Math.imul(hash, 33) ^ node.id.charCodeAt(0) ^ node.id.charCodeAt(node.id.length - 1);
    hash = (hash + node.id.length) | 0;
  }
  for (const edge of graph.edges) {
    hash = Math.imul(hash, 31) ^ edge.source.charCodeAt(0) ^ edge.target.charCodeAt(0);
    hash = (hash + edge.source.length + edge.target.length) | 0;
  }
  return `${layoutMode}:${Math.round(size.width / 80)}x${Math.round(size.height / 80)}:${hash}`;
}

function getConnectedNodeIds(nodeId: string, edges: GraphEdge[]) {
  const sources = new Set<string>();
  const targets = new Set<string>();

  edges.forEach(edge => {
    if (edge.source === nodeId) targets.add(edge.target);
    if (edge.target === nodeId) sources.add(edge.source);
  });

  const all = new Set([...sources, ...targets]);
  return { sources, targets, all };
}

// Build a map of namespace ids for each node (used for cross-ns edge detection).
function buildNodeNamespaceMap(graph: Graph): Map<string, string> {
  const nsMap = new Map<string, string>();
  for (const node of graph.nodes) {
    if (node.type === 'namespace') {
      nsMap.set(node.id, node.id);
    } else if (node.parent) {
      nsMap.set(node.id, node.parent);
    }
  }
  return nsMap;
}

// Edge-derived per-node counts, computed in one pass. Doing these per node
// (filtering every edge for every node) was O(nodes x edges) and dominated
// layout time on large clusters.
function edgeCounts(edges: GraphEdge[]) {
  const degree = new Map<string, number>();
  const routesTo = new Map<string, number>();
  for (const e of edges) {
    degree.set(e.source, (degree.get(e.source) ?? 0) + 1);
    degree.set(e.target, (degree.get(e.target) ?? 0) + 1);
    if (e.type === 'routes_to') routesTo.set(e.source, (routesTo.get(e.source) ?? 0) + 1);
  }
  return { degree, routesTo };
}

// Id prefix of the merged service->workload edges swimlane draws.
const SWIM_EDGE_PREFIX = 'swim:';

const SWIM_WORKLOAD_TYPES = new Set(['deployment', 'statefulset', 'daemonset']);

// Swimlane already encodes containment spatially (lane = namespace, row =
// workload + its pods), so `contains` edges only add noise, and a service's
// routes_to fan-out to every pod becomes one edge to the owning workload.
// Pods without an owning workload keep their direct edge.
function simplifySwimlaneEdges(edges: GraphEdge[], nodeById: Map<string, Graph['nodes'][number]>): GraphEdge[] {
  const podOwner = new Map<string, string>();
  for (const e of edges) {
    if (e.type !== 'contains') continue;
    if (nodeById.get(e.target)?.type === 'pod' && SWIM_WORKLOAD_TYPES.has(nodeById.get(e.source)?.type ?? '')) {
      podOwner.set(e.target, e.source);
    }
  }
  const out: GraphEdge[] = [];
  const seen = new Set<string>();
  for (const e of edges) {
    if (e.type === 'contains') continue;
    const owner = e.type === 'routes_to' ? podOwner.get(e.target) : undefined;
    if (!owner) {
      out.push(e);
      continue;
    }
    const id = `${SWIM_EDGE_PREFIX}${e.source}->${owner}`;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push({ id, source: e.source, target: owner, type: 'routes_to' });
  }
  return out;
}

function computeLayout(
  graph: Graph,
  layoutMode: LayoutMode,
  savedPositions: Map<string, { x: number; y: number; isPinned?: boolean }>,
  viewportSize: { width: number; height: number },
  namespaces: Map<string, NamespaceSummary>,
  onToggleCollapse: ((namespaceName: string) => void) | undefined,
  // Force layout passes positions it already settled (or a cheap seed). The
  // synchronous 300-tick simulation must not run during render.
  presetPositions?: Map<string, { x: number; y: number }> | null,
  onToggleCluster?: (clusterKey: string) => void,
): {
  nodes: Node<CustomNodeData>[];
  edges: Edge<CustomEdgeData>[];
  // Every edge, for layouts that render a simplified set: the selected
  // node's real edges are shown on top of the simplified ones.
  detailEdges?: Edge<CustomEdgeData>[];
} {
  const isSwimLane = layoutMode === 'swimlane';
  const { degree, routesTo } = edgeCounts(graph.edges);

  // Namespace data shared by both render paths.
  const namespaceData = (nodeId: string, health: CustomNodeData['health']) => {
    const summary = namespaces.get(nodeId);
    if (!summary) return {};
    return {
      health: summary.collapsed ? worstHealth(summary.healthCounts, health) : health,
      workloadCount: summary.workloadCount,
      podCount: summary.podCount,
      serviceCount: summary.serviceCount,
      healthCounts: summary.healthCounts,
      collapsed: summary.collapsed,
      onToggleCollapse,
    };
  };

  // ── Swimlane layout ─────────────────────────────────────────────────
  if (isSwimLane) {
    const { positions: swimPositions, laneBounds } = calculateSwimlaneLayout(
      graph,
      id => namespaces.get(id)?.collapsed ?? false,
    );
    const nodeNsMap = buildNodeNamespaceMap(graph);

    // Merge with saved pinned positions.
    const finalPositions = new Map(swimPositions);
    savedPositions.forEach((pos, nodeId) => {
      if (pos.isPinned && swimPositions.has(nodeId)) {
        finalPositions.set(nodeId, { x: pos.x, y: pos.y });
      }
    });

    const nodes: Node<CustomNodeData>[] = graph.nodes.map(node => {
      const connectionCount = degree.get(node.id) ?? 0;
      const priority = calculatePriority(node, connectionCount);
      const savedPos = savedPositions.get(node.id);
      const position = finalPositions.get(node.id) || { x: 0, y: 0 };
      const isGroup = node.type === 'namespace';

      // Enriched metadata for per-type rendering.
      const metadata = node.metadata ?? {};

      // Service: routes_to count.
      const routesToCount = node.type === 'k8s_service' ? (routesTo.get(node.id) ?? 0) : undefined;

      const rfNode: Node<CustomNodeData> = {
        id: node.id,
        type: 'custom',
        position,
        data: {
          name: node.name,
          type: node.type,
          health: node.health,
          priority,
          connectionCount,
          isConnected: undefined,
          isSource: false,
          isTarget: false,
          isPinned: savedPos?.isPinned || false,
          isGroup,
          isSwimlane: true,
          // K8s enriched data
          metadata,
          routesToCount,
          ...(isGroup ? namespaceData(node.id, node.health) : {}),
        },
      };

      // Namespace nodes in swimlane get lane dimensions as style.
      if (isGroup) {
        const bounds = laneBounds.get(node.id);
        if (bounds) {
          rfNode.style = { width: bounds.w, height: bounds.h };
        }
      }

      return rfNode;
    });

    // Namespace (group) nodes first for React Flow z-order (they render behind).
    nodes.sort((a, b) => {
      const ag = a.data.isGroup ? 0 : 1;
      const bg = b.data.isGroup ? 0 : 1;
      return ag - bg;
    });

    const toFlowEdge = (edge: GraphEdge): Edge<CustomEdgeData> => {
      const sourceNs = nodeNsMap.get(edge.source);
      const targetNs = nodeNsMap.get(edge.target);
      const isCrossNamespace = sourceNs !== targetNs && sourceNs !== undefined && targetNs !== undefined;

      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        type: 'custom',
        data: {
          label: edge.label,
          edgeType: edge.type,
          isActive: undefined,
          isCrossNamespace,
        },
      };
    };
    const nodeById = new Map(graph.nodes.map(n => [n.id, n]));
    const edges = simplifySwimlaneEdges(graph.edges, nodeById).map(toFlowEdge);
    const detailEdges = graph.edges.map(toFlowEdge);

    return { nodes, edges, detailEdges };
  }

  // ── Hierarchical / Force layout (existing) ──────────────────────────
  const basePositions = presetPositions
    ? presetPositions
    : layoutMode === 'hierarchical'
      ? calculateHierarchicalLayout(graph)
      : calculateForceDirectedLayout(graph, viewportSize);

  // Merge with saved positions (pinned nodes override calculated positions)
  const finalPositions = new Map(basePositions);
  savedPositions.forEach((pos, nodeId) => {
    if (pos.isPinned && basePositions.has(nodeId)) {
      finalPositions.set(nodeId, { x: pos.x, y: pos.y });
    }
  });

  // Identify namespace groups (nodes whose metadata.group === true). For each
  // group, collect children via the parent pointer, compute their bounding
  // box, and lay the group out as a React Flow container.
  const GROUP_PAD = 24;
  const GROUP_HEADER_H = 36;
  const CHILD_W = 220;
  const CHILD_H = 64;
  const groupChildren = new Map<string, string[]>();
  for (const n of graph.nodes) {
    if (!n.parent) continue;
    if (!groupChildren.has(n.parent)) groupChildren.set(n.parent, []);
    groupChildren.get(n.parent)!.push(n.id);
  }
  const groupBounds = new Map<string, { x: number; y: number; w: number; h: number }>();
  for (const n of graph.nodes) {
    if (!n.metadata?.group) continue;
    const childIds = groupChildren.get(n.id) ?? [];
    if (childIds.length === 0) continue;
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const cid of childIds) {
      const p = finalPositions.get(cid);
      if (!p) continue;
      minX = Math.min(minX, p.x);
      minY = Math.min(minY, p.y);
      maxX = Math.max(maxX, p.x + CHILD_W);
      maxY = Math.max(maxY, p.y + CHILD_H);
    }
    if (!isFinite(minX)) continue;
    groupBounds.set(n.id, {
      x: minX - GROUP_PAD,
      y: minY - GROUP_PAD - GROUP_HEADER_H,
      w: maxX - minX + 2 * GROUP_PAD,
      h: maxY - minY + 2 * GROUP_PAD + GROUP_HEADER_H,
    });
  }

  const nodes: Node<CustomNodeData>[] = graph.nodes.map(node => {
    const connectionCount = degree.get(node.id) ?? 0;
    const priority = calculatePriority(node, connectionCount);
    const savedPos = savedPositions.get(node.id);
    const groupBox = node.metadata?.group ? groupBounds.get(node.id) : undefined;
    // A group with nothing inside (e.g. a collapsed namespace) renders as a
    // regular card rather than an empty container.
    const isGroup = groupBox !== undefined;

    let position = finalPositions.get(node.id) || { x: 0, y: 0 };
    let parentId: string | undefined;
    let extent: 'parent' | undefined;

    if (isGroup && groupBox) {
      position = { x: groupBox.x, y: groupBox.y };
    } else if (node.parent) {
      const parentBox = groupBounds.get(node.parent);
      if (parentBox) {
        position = { x: position.x - parentBox.x, y: position.y - parentBox.y };
        parentId = node.parent;
        extent = 'parent';
      }
    }

    const rfNode: Node<CustomNodeData> = {
      id: node.id,
      type: 'custom',
      position,
      data: {
        name: node.name,
        type: node.type,
        health: node.health,
        priority,
        connectionCount,
        isConnected: undefined,
        isSource: false,
        isTarget: false,
        isPinned: savedPos?.isPinned || false,
        isGroup,
        metadata: node.metadata,
        isCluster: node.metadata?.cluster === true,
        clusterKey: typeof node.metadata?.clusterKey === 'string' ? node.metadata.clusterKey : undefined,
        clusterKind: typeof node.metadata?.clusterKind === 'string' ? node.metadata.clusterKind : undefined,
        clusterExpanded: node.metadata?.clusterExpanded === true,
        onToggleCluster: node.metadata?.cluster === true ? onToggleCluster : undefined,
        ...(node.type === 'namespace' ? namespaceData(node.id, node.health) : {}),
      },
    };
    if (isGroup && groupBox) {
      rfNode.style = { width: groupBox.w, height: groupBox.h };
    }
    if (parentId) {
      rfNode.parentId = parentId;
      rfNode.extent = extent;
    }
    return rfNode;
  });

  // Group nodes must precede their children in the array (React Flow requirement).
  nodes.sort((a, b) => {
    const ag = a.data.isGroup ? 0 : 1;
    const bg = b.data.isGroup ? 0 : 1;
    return ag - bg;
  });

  const edges: Edge<CustomEdgeData>[] = graph.edges.map(edge => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: 'custom',
    data: {
      label: edge.label,
      edgeType: edge.type,
      isActive: undefined,
    },
  }));

  return { nodes, edges };
}

function applyHighlighting(
  layoutNodes: Node<CustomNodeData>[],
  layoutEdges: Edge<CustomEdgeData>[],
  selectedNodeId: string | null,
  graphEdges: GraphEdge[],
  detailEdges: Edge<CustomEdgeData>[] | undefined,
): {
  nodes: Node<CustomNodeData>[];
  edges: Edge<CustomEdgeData>[];
} {
  if (!selectedNodeId) return { nodes: layoutNodes, edges: layoutEdges };

  const connectedInfo = getConnectedNodeIds(selectedNodeId, graphEdges);

  const nodes = layoutNodes.map(node => {
    if (node.id === selectedNodeId) return node;

    return {
      ...node,
      data: {
        ...node.data,
        isConnected: connectedInfo.all.has(node.id),
        isSource: connectedInfo.sources.has(node.id),
        isTarget: connectedInfo.targets.has(node.id),
      },
    };
  });

  const shown = new Set(layoutEdges.map(e => e.id));
  const touching = (e: Edge<CustomEdgeData>) => e.source === selectedNodeId || e.target === selectedNodeId;
  const extra = (detailEdges ?? []).filter(e => !shown.has(e.id) && touching(e));
  const edges = [...layoutEdges, ...extra].map(edge => ({
    ...edge,
    data: {
      ...edge.data,
      isActive: touching(edge),
    },
  }));

  return { nodes, edges };
}

function GraphCanvasInner({
  graph,
  selectedNodeId,
  onNodeSelect,
  onEdgeClick,
  layoutMode,
  resetKey,
  isLoading,
  error,
  onRetry,
  namespaces,
  onToggleCollapse,
  fitKey,
}: GraphCanvasProps) {
  const { setViewport } = useReactFlow();
  const store = useStoreApi();
  const hasFitRef = useRef(false);
  const canvasRef = useRef<HTMLDivElement>(null);
  const { savedPositions, savePosition, clearLayout } = useLayoutPersistence(graph, layoutMode);
  const [isTransitioning, setIsTransitioning] = useState(false);
  const [justSavedNodeId, setJustSavedNodeId] = useState<string | null>(null);
  // Layout transition: start it during render when layoutMode changes, so the
  // fade begins on the same frame as the new layout.
  const [transitionMode, setTransitionMode] = useState(layoutMode);
  if (transitionMode !== layoutMode) {
    setTransitionMode(layoutMode);
    setIsTransitioning(true);
  }

  const graphIsEmpty = graph?.nodes?.length === 0;

  // Dynamic viewport via ResizeObserver. `ready` gates the force simulation so
  // it runs once, against the real pane, instead of again when 1200x800 is replaced.
  const [viewportSize, setViewportSize] = useState({ width: 1200, height: 800, ready: false });
  useEffect(() => {
    const el = canvasRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const entry = entries[0];
      if (entry) {
        const { width, height } = entry.contentRect;
        if (width > 0 && height > 0) {
          setViewportSize(prev => (
            prev.ready && prev.width === width && prev.height === height
              ? prev
              : { width, height, ready: true }
          ));
        }
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
    // The canvas div only exists once loading/error/empty states are gone,
    // so re-run when those change or the observer never attaches.
  }, [isLoading, error, graphIsEmpty]);

  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);
  const expandedSet = useMemo(() => new Set(expandedKeys), [expandedKeys]);
  const toggleCluster = useCallback((key: string) => {
    setExpandedKeys(prev => (prev.includes(key) ? prev.filter(item => item !== key) : [...prev, key].sort()));
  }, []);
  // Collapse hub fan-out before layout. Swimlane already simplifies containment.
  const fanout = useMemo(() => {
    if (!graph || layoutMode === 'swimlane') return null;
    return collapseFanout(graph, expandedSet);
  }, [graph, expandedSet, layoutMode]);
  const displayGraph = fanout?.graph ?? graph;
  // Selecting a node that lives inside a collapsed hub opens that hub. Adjusted
  // during render so the expanded graph is what layout sees on this pass.
  const selectedCluster = selectedNodeId && fanout ? fanout.memberCluster.get(selectedNodeId) : undefined;
  if (selectedCluster && !expandedKeys.includes(selectedCluster)) {
    setExpandedKeys([...expandedKeys, selectedCluster].sort());
  }

  const pinSignature = useMemo(() => {
    const ids: string[] = [];
    savedPositions.forEach((pos, id) => { if (pos.isPinned) ids.push(id); });
    ids.sort();
    return ids.join('|');
  }, [savedPositions]);
  const simKey = useMemo(() => {
    if (!graph) return 'empty';
    return `${structureKey(graph, layoutMode, viewportSize, resetKey ?? 0)}:${pinSignature}`;
  }, [graph, layoutMode, viewportSize, resetKey, pinSignature]);

  const interactingRef = useRef(false);
  // Positions captured when the simulation freezes. Ticks update node state
  // directly and do not replace this map, so edges are not rebuilt per frame.
  const [frozen, setFrozen] = useState<Map<string, { x: number; y: number }>>(() => new Map());
  const rememberPositions = useCallback((positions: Map<string, { x: number; y: number }>) => {
    setFrozen(positions);
  }, []);
  const layoutPositions = useMemo(() => {
    if (layoutMode !== 'force' || !displayGraph) return null;
    const map = new Map(frozen);
    placeMissingNodes(displayGraph.nodes, map);
    return map;
  }, [displayGraph, layoutMode, frozen]);

  // Memo 1: Expensive layout computation — NOT dependent on selectedNodeId.
  // Force positions come from the simulation hook, never from a render-time tick loop.
  const { layoutNodes, layoutEdges, detailEdges } = useMemo(() => {
    if (!displayGraph?.nodes) return { layoutNodes: [], layoutEdges: [], detailEdges: undefined };
    const { nodes, edges, detailEdges } = computeLayout(
      displayGraph,
      layoutMode,
      savedPositions,
      viewportSize,
      namespaces ?? new Map(),
      onToggleCollapse,
      layoutPositions,
      toggleCluster,
    );
    return { layoutNodes: nodes, layoutEdges: edges, detailEdges };
  }, [displayGraph, layoutMode, savedPositions, viewportSize, namespaces, onToggleCollapse, layoutPositions, toggleCluster]);

  // Latest layout + size for deferred fits (they run from timers).
  const fitInputRef = useRef({ layoutNodes, viewportSize });
  useEffect(() => {
    fitInputRef.current = { layoutNodes, viewportSize };
  }, [layoutNodes, viewportSize]);
  const fitView = useCallback((opts: { duration?: number } = {}) => {
    const { layoutNodes: ln, viewportSize: vs } = fitInputRef.current;
    // Prefer React Flow's own measured size: the ResizeObserver isn't attached
    // when the canvas first mounts behind the loading state, so viewportSize
    // can still be its 1200x800 fallback here.
    const { width, height } = store.getState();
    const vp = viewportForLayout(ln, width > 0 && height > 0 ? { width, height } : vs);
    if (vp) setViewport(vp, { duration: opts.duration });
  }, [setViewport, store]);

  // Force layout fits after the simulation freezes; fitting the seed circle
  // first would frame the wrong area. The ref is updated once the hook runs.
  const forceSettledRef = useRef(layoutMode !== 'force');
  // End the transition and refit. layoutMode is a dependency so a second
  // switch mid-transition restarts the timer.
  useEffect(() => {
    if (!isTransitioning) return;
    let timer = 0;
    const finish = () => {
      if (layoutMode === 'force' && !forceSettledRef.current) {
        timer = window.setTimeout(finish, 40);
        return;
      }
      setIsTransitioning(false);
      fitView({ duration: 500 });
    };
    timer = window.setTimeout(finish, 100);
    return () => clearTimeout(timer);
  }, [isTransitioning, layoutMode, fitView]);

  // Memo 2: Cheap highlighting — runs on node click, no layout recomputation
  const { flowNodes, flowEdges } = useMemo(() => {
    if (layoutNodes.length === 0) return { flowNodes: [], flowEdges: [] };
    const { nodes, edges } = applyHighlighting(layoutNodes, layoutEdges, selectedNodeId, graph?.edges ?? [], detailEdges);
    return { flowNodes: nodes, flowEdges: edges };
  }, [layoutNodes, layoutEdges, selectedNodeId, graph?.edges, detailEdges]);

  const [nodes, setNodes, onNodesChange] = useNodesState(flowNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(flowEdges);

  const force = useForceSimulation({
    enabled: layoutMode === 'force' && (displayGraph?.nodes.length ?? 0) > 0,
    ready: viewportSize.ready,
    simKey,
    graph: displayGraph ?? { nodes: [], edges: [] },
    size: viewportSize,
    pinned: savedPositions,
    interactingRef,
    setNodes,
    onFrozen: rememberPositions,
  });
  useEffect(() => {
    forceSettledRef.current = force.settled;
  }, [force.settled]);

  useEffect(() => {
    setNodes(flowNodes);
    setEdges(flowEdges);
  }, [flowNodes, flowEdges, setNodes, setEdges]);

  const edgeCount = flowEdges.length;
  const detail = useStore(useCallback(
    (state: { transform: [number, number, number] }) => detailLevel(state.transform[2], edgeCount),
    [edgeCount],
  ));

  // Fit once when the graph first arrives and whenever fitKey changes (the
  // caller bumps it for filter changes). Not on every node-count change:
  // expanding/collapsing a namespace or pods churning in a live cluster must
  // leave the user's viewport alone. Force layout waits until the simulation
  // has cooled so the frame matches the frozen positions.
  const hasNodes = (graph?.nodes?.length ?? 0) > 0;
  const prevFitKeyRef = useRef(fitKey);
  useEffect(() => {
    if (!hasNodes || !force.settled) return;
    if (hasFitRef.current && fitKey === prevFitKeyRef.current) return;
    hasFitRef.current = true;
    prevFitKeyRef.current = fitKey;
    setTimeout(() => fitView({ duration: 500 }), 100);
  }, [hasNodes, fitKey, fitView, force.settled]);

  // Reset positions when resetKey changes (triggered by "Reset Positions" button).
  // The fit waits until a force simulation has frozen again.
  const prevResetKeyRef = useRef(resetKey);
  const resetFitPendingRef = useRef(false);
  useEffect(() => {
    if (resetKey === undefined || resetKey === prevResetKeyRef.current) return;
    prevResetKeyRef.current = resetKey;
    clearLayout();
    resetFitPendingRef.current = true;
  }, [resetKey, clearLayout]);
  useEffect(() => {
    if (!resetFitPendingRef.current || !force.settled) return;
    resetFitPendingRef.current = false;
    setTimeout(() => fitView({ duration: 500 }), 50);
  }, [force.settled, resetKey, fitView]);

  const nodesWithSelection = useMemo(() => {
    return nodes.map(node => ({
      ...node,
      selected: node.id === selectedNodeId,
      data: {
        ...node.data,
        justSaved: node.id === justSavedNodeId,
      },
    }));
  }, [nodes, selectedNodeId, justSavedNodeId]);

  const handleNodeClick = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      const data = node.data as { isCluster?: boolean; clusterKey?: string };
      if (data.isCluster && data.clusterKey) {
        toggleCluster(data.clusterKey);
        return;
      }
      onNodeSelect(node.id);
    },
    [onNodeSelect, toggleCluster]
  );

  const handleMoveStart = useCallback(() => {
    interactingRef.current = true;
  }, []);

  const handleMoveEnd = useCallback(() => {
    interactingRef.current = false;
    force.resume();
  }, [force]);

  const handleEdgeClick = useCallback(
    (_event: React.MouseEvent, edge: Edge) => {
      // A simplified swimlane edge stands for several real edges, so there is
      // no single edge to inspect. Select its source service instead: that
      // reveals the real edges on the canvas and lists them in the inspector,
      // and the node id round-trips through the URL like any selection.
      if (edge.id.startsWith(SWIM_EDGE_PREFIX)) {
        onNodeSelect(edge.source);
        return;
      }
      if (!onEdgeClick || !graph) return;
      const graphEdge = graph.edges.find(e => e.id === edge.id);
      if (graphEdge) onEdgeClick(graphEdge);
    },
    [onEdgeClick, onNodeSelect, graph]
  );

  const handlePaneClick = useCallback(() => {
    onNodeSelect(null);
  }, [onNodeSelect]);

  // Debounced save position handler
  const debouncedSavePosition = useMemo(
    () => debounce((...args: unknown[]) => {
      const [nodeId, position] = args as [string, { x: number; y: number }];
      savePosition(nodeId, position, true);
    }, 500),
    [savePosition]
  );

  const handleNodeDragStart = useCallback(() => {
    interactingRef.current = true;
  }, []);

  const handleNodeDragStop = useCallback(
    (_event: React.MouseEvent, node: Node) => {
      interactingRef.current = false;
      if (force.settled) {
        setFrozen(prev => {
          const next = new Map(prev);
          next.set(node.id, node.position);
          return next;
        });
      }
      force.nudge(node.id, node.position);
      force.resume();
      debouncedSavePosition(node.id, node.position);
      setJustSavedNodeId(node.id);
      setTimeout(() => setJustSavedNodeId(prev => prev === node.id ? null : prev), 600);
    },
    [debouncedSavePosition, force]
  );

  if (error) {
    return (
      <div className={styles.loading}>
        <div className={styles.loadingContent}>
          <div className={styles.errorIcon}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 8v4m0 4h.01" />
            </svg>
          </div>
          <span className={styles.loadingText}>Failed to load graph</span>
          <span className={styles.errorDetail}>{error.message}</span>
          {onRetry && (
            <button className={styles.retryBtn} onClick={onRetry}>
              Try again
            </button>
          )}
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className={styles.skeleton}>
        <div className={styles.skeletonNode} style={{ top: '20%', left: '15%' }} />
        <div className={styles.skeletonNode} style={{ top: '35%', left: '45%' }} />
        <div className={styles.skeletonNode} style={{ top: '15%', left: '65%' }} />
        <div className={styles.skeletonNode} style={{ top: '55%', left: '25%' }} />
        <div className={styles.skeletonNode} style={{ top: '50%', left: '70%' }} />
        <div className={styles.skeletonNode} style={{ top: '70%', left: '50%' }} />
      </div>
    );
  }

  if (graph && graph.nodes?.length === 0) {
    return <EmptyState />;
  }

  return (
    <div
      ref={canvasRef}
      className={`${styles.canvas} ${isTransitioning ? styles.transitioning : ''}`}
      data-calm={nodes.length > CALM_NODES}
      data-detail={detail}
    >
      <GraphDetailContext.Provider value={detail}>
      <ReactFlow
        nodes={nodesWithSelection}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={handleNodeClick}
        onEdgeClick={handleEdgeClick}
        onPaneClick={handlePaneClick}
        onMoveStart={handleMoveStart}
        onMoveEnd={handleMoveEnd}
        onNodeDragStart={handleNodeDragStart}
        onNodeDragStop={handleNodeDragStop}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView={layoutMode !== 'force'}
        fitViewOptions={FIT_VIEW}
        minZoom={MIN_ZOOM}
        maxZoom={2.5}
        proOptions={{ hideAttribution: true }}
        onlyRenderVisibleElements={nodes.length > VIRTUALIZE_NODES}
        defaultEdgeOptions={{ type: 'custom' }}
      >
        <EdgeMarkerDefs />
        <Background
          variant={BackgroundVariant.Dots}
          gap={28}
          size={1.2}
          color="rgba(255, 255, 255, 0.06)"
        />
        <Controls
          className={styles.controls}
          showInteractive={false}
          showFitView={false}
          position="bottom-right"
        >
          <ControlButton onClick={() => fitView({ duration: 400 })} title="Fit view" aria-label="Fit view">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className={styles.strokeIcon}>
              <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
            </svg>
          </ControlButton>
        </Controls>
        <MiniMap
          className={styles.minimap}
          nodeColor="#2a2a2a"
          nodeStrokeColor="rgba(255, 255, 255, 0.15)"
          nodeBorderRadius={4}
          maskColor="rgba(0, 0, 0, 0.7)"
          bgColor="#111111"
          position="bottom-left"
          pannable
          zoomable
        />
      </ReactFlow>
      </GraphDetailContext.Provider>
    </div>
  );
}

export default function GraphCanvas(props: GraphCanvasProps) {
  return (
    <ReactFlowProvider>
      <GraphCanvasInner {...props} />
    </ReactFlowProvider>
  );
}
