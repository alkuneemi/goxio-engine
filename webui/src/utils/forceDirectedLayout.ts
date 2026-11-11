import {
  forceSimulation,
  forceLink,
  forceManyBody,
  forceCenter,
  forceCollide,
  type Simulation,
  type SimulationNodeDatum,
  type SimulationLinkDatum,
} from 'd3-force';
import type { Graph } from '../types';

export interface ForceNode extends SimulationNodeDatum {
  id: string;
}

interface ForceLink extends SimulationLinkDatum<ForceNode> {
  source: string;
  target: string;
}

export interface FlowPosition {
  x: number;
  y: number;
}

// Same forces the synchronous layout has always used, so a settled map looks
// the way it did before the simulation moved off the render path.
export function createForceSimulation(
  graph: Graph,
  containerSize: { width: number; height: number },
  pinned: Map<string, { x: number; y: number; isPinned?: boolean }>,
): { simulation: Simulation<ForceNode, ForceLink>; nodes: ForceNode[] } {
  const { width, height } = containerSize;
  const nodes: ForceNode[] = graph.nodes.map(node => {
    const pin = pinned.get(node.id);
    const sim: ForceNode = { id: node.id };
    if (pin?.isPinned) {
      sim.fx = pin.x + width / 2;
      sim.fy = pin.y + height / 2;
      sim.x = sim.fx;
      sim.y = sim.fy;
    }
    return sim;
  });

  const links: ForceLink[] = graph.edges.map(edge => ({
    source: edge.source,
    target: edge.target,
  }));

  const simulation = forceSimulation(nodes)
    .force(
      'link',
      forceLink<ForceNode, ForceLink>(links)
        .id(d => d.id)
        .distance(180)
        .strength(0.5),
    )
    .force('charge', forceManyBody().strength(-400))
    .force('center', forceCenter(width / 2, height / 2))
    .force('collide', forceCollide().radius(150).strength(0.7))
    .stop();

  return { simulation, nodes };
}

export function flowPosition(node: ForceNode, containerSize: { width: number; height: number }): FlowPosition | null {
  if (node.x === undefined || node.y === undefined) return null;
  return { x: node.x - containerSize.width / 2, y: node.y - containerSize.height / 2 };
}

export function calculateForceDirectedLayout(
  graph: Graph,
  containerSize: { width: number; height: number },
): Map<string, FlowPosition> {
  if (!graph.nodes.length) return new Map();

  const { simulation, nodes } = createForceSimulation(graph, containerSize, new Map());
  for (let i = 0; i < 300; i++) simulation.tick();

  const positions = new Map<string, FlowPosition>();
  nodes.forEach(node => {
    const position = flowPosition(node, containerSize);
    if (position) positions.set(node.id, position);
  });
  return positions;
}

/**
 * Give nodes that do not yet have a position somewhere to render before the
 * simulation moves them. Members of an expanded cluster land in a grid around
 * their parent so expanding does not need another simulation.
 */
export function placeMissingNodes(
  nodes: { id: string; parent?: string }[],
  positions: Map<string, FlowPosition>,
): void {
  const missing = nodes.filter(node => !positions.has(node.id));
  if (missing.length === 0) return;

  const around = new Map<string, string[]>();
  const loose: string[] = [];
  for (const node of missing) {
    if (node.parent && positions.has(node.parent)) {
      const list = around.get(node.parent);
      if (list) list.push(node.id);
      else around.set(node.parent, [node.id]);
    } else {
      loose.push(node.id);
    }
  }

  loose.forEach((id, index) => {
    const angle = (index / Math.max(loose.length, 1)) * Math.PI * 2;
    const radius = 280 + (index % 8) * 36;
    positions.set(id, { x: Math.cos(angle) * radius, y: Math.sin(angle) * radius });
  });

  for (const [parentId, ids] of around) {
    const origin = positions.get(parentId);
    if (!origin) continue;
    const cols = Math.ceil(Math.sqrt(ids.length));
    ids.forEach((id, index) => {
      const col = index % cols;
      const row = Math.floor(index / cols);
      positions.set(id, {
        x: origin.x + (col - (cols - 1) / 2) * 210,
        y: origin.y + 150 + row * 96,
      });
    });
  }
}
