import { useEffect, useRef, useState, type Dispatch, type MutableRefObject, type SetStateAction } from 'react';
import type { Graph } from '../types';
import { createForceSimulation, flowPosition, type FlowPosition, type ForceNode } from '../utils/forceDirectedLayout';

interface Positioned {
  id: string;
  position: FlowPosition;
}

interface UseForceSimulationOptions<N extends Positioned> {
  enabled: boolean;
  ready: boolean;
  simKey: string;
  graph: Graph;
  size: { width: number; height: number };
  pinned: Map<string, { x: number; y: number; isPinned?: boolean }>;
  interactingRef: MutableRefObject<boolean>;
  setNodes: Dispatch<SetStateAction<N[]>>;
  onFrozen: (positions: Map<string, FlowPosition>) => void;
}

/**
 * Runs d3-force off the React render path. Ticks are batched into animation
 * frames and skipped entirely while the user pans, zooms, or drags. Once alpha
 * cools to d3's stopping point the simulation is stopped and positions stay
 * frozen — later frames do not tick and do not write node state.
 */
export function useForceSimulation<N extends Positioned>({
  enabled,
  ready,
  simKey,
  graph,
  size,
  pinned,
  interactingRef,
  setNodes,
  onFrozen,
}: UseForceSimulationOptions<N>): { settled: boolean; resume: () => void; nudge: (id: string, position: FlowPosition) => void } {
  const graphRef = useRef(graph);
  const sizeRef = useRef(size);
  const pinnedRef = useRef(pinned);
  const onFrozenRef = useRef(onFrozen);
  useEffect(() => {
    graphRef.current = graph;
    sizeRef.current = size;
    pinnedRef.current = pinned;
    onFrozenRef.current = onFrozen;
  }, [graph, size, pinned, onFrozen]);

  const simNodesRef = useRef<Map<string, ForceNode>>(new Map());
  const resumeRef = useRef<() => void>(() => {});
  const generationRef = useRef(0);

  // `settledKey` is the simKey whose positions are frozen. Clearing it when the
  // key changes (or when force layout turns off) makes the caller wait for the
  // next cooldown instead of fitting a stale layout.
  const [settledKey, setSettledKey] = useState<string | null>(null);
  if (!enabled && settledKey !== null) setSettledKey(null);
  if (enabled && settledKey !== null && settledKey !== simKey) setSettledKey(null);

  useEffect(() => {
    if (!enabled || !ready) return;
    const generation = ++generationRef.current;
    const currentGraph = graphRef.current;
    const currentSize = sizeRef.current;
    const { simulation, nodes } = createForceSimulation(currentGraph, currentSize, pinnedRef.current);
    const byId = new Map(nodes.map(node => [node.id, node]));
    simNodesRef.current = byId;

    let stopped = false;
    let raf = 0;
    let ticks = 0;
    let latest = new Map<string, FlowPosition>();

    const publish = () => {
      const next = new Map(latest);
      let moved = false;
      for (const node of nodes) {
        const position = flowPosition(node, currentSize);
        if (!position) continue;
        const prev = next.get(node.id);
        if (!prev || prev.x !== position.x || prev.y !== position.y) moved = true;
        next.set(node.id, position);
      }
      if (!moved) return;
      latest = next;
      setNodes(current => {
        let changed = false;
        const updated = current.map(node => {
          const position = next.get(node.id);
          if (!position) return node;
          if (node.position.x === position.x && node.position.y === position.y) return node;
          changed = true;
          return { ...node, position };
        });
        return changed ? updated : current;
      });
    };

    const step = () => {
      if (stopped || generation !== generationRef.current) return;
      if (interactingRef.current) return;
      const started = performance.now();
      let count = 0;
      while (count < 24 && performance.now() - started < 10) {
        simulation.tick();
        count += 1;
      }
      ticks += count;
      publish();
      // 300 ticks is d3's default cooldown (alphaMin ^ (1/300)), matching the
      // previous synchronous layout, then the simulation is never restarted.
      if (ticks >= 300 || simulation.alpha() <= simulation.alphaMin()) {
        simulation.stop();
        stopped = true;
        if (generation === generationRef.current) {
          onFrozenRef.current(latest);
          setSettledKey(simKey);
        }
        return;
      }
      raf = requestAnimationFrame(step);
    };

    raf = requestAnimationFrame(step);
    resumeRef.current = () => {
      if (!stopped && generation === generationRef.current) {
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(step);
      }
    };

    return () => {
      stopped = true;
      simulation.stop();
      cancelAnimationFrame(raf);
      if (resumeRef.current) resumeRef.current = () => {};
    };
  }, [enabled, ready, simKey, interactingRef, setNodes]);

  const nudge = (id: string, position: FlowPosition) => {
    const node = simNodesRef.current.get(id);
    const currentSize = sizeRef.current;
    if (!node) return;
    node.fx = position.x + currentSize.width / 2;
    node.fy = position.y + currentSize.height / 2;
    node.x = node.fx;
    node.y = node.fy;
  };

  return {
    settled: !enabled || settledKey === simKey,
    resume: () => resumeRef.current(),
    nudge,
  };
}
