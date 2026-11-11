// Below this zoom, edge labels and card chrome cost more than they show.
// 0.5 is where a CONTAINS label is still large enough to read.
export const COMPACT_ZOOM_BELOW = 0.5;

// At this zoom a CONTAINS label is large on screen. Full detail is then drawn
// only for elements React Flow has mounted, which are the ones in the viewport.
export const FULL_DETAIL_ZOOM = 0.9;

// Past this many edges inside the viewport, dashed strokes and per-edge labels
// dominate pan and zoom. The total graph size does not decide this on its own.
export const COMPACT_EDGE_AT = 80;

export type DetailLevel = 'full' | 'compact';

export interface NodeBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface ViewRect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/**
 * Level of detail for the map. `visibleEdges` is the number of edges that
 * intersect the current viewport, not the size of the whole graph.
 * `compact` hides edge labels, draws thin solid edges, and drops shadows,
 * filters, and thumbnails. `full` is the normal card and edge styling.
 * At {@link FULL_DETAIL_ZOOM} and above the viewport is small enough that the
 * mounted edges and nodes can carry labels.
 */
export function detailLevel(zoom: number, visibleEdges: number): DetailLevel {
  if (zoom >= FULL_DETAIL_ZOOM) return 'full';
  if (zoom < COMPACT_ZOOM_BELOW) return 'compact';
  if (visibleEdges >= COMPACT_EDGE_AT) return 'compact';
  return 'full';
}

function boxIntersects(box: NodeBox, view: ViewRect): boolean {
  return box.x < view.right && box.x + box.w > view.left && box.y < view.bottom && box.y + box.h > view.top;
}

function orient(ax: number, ay: number, bx: number, by: number, cx: number, cy: number): number {
  return (by - ay) * (cx - bx) - (bx - ax) * (cy - by);
}

function segmentsCross(
  x1: number, y1: number, x2: number, y2: number,
  x3: number, y3: number, x4: number, y4: number,
): boolean {
  const o1 = orient(x1, y1, x2, y2, x3, y3);
  const o2 = orient(x1, y1, x2, y2, x4, y4);
  const o3 = orient(x3, y3, x4, y4, x1, y1);
  const o4 = orient(x3, y3, x4, y4, x2, y2);
  return o1 * o2 < 0 && o3 * o4 < 0;
}

function segmentHitsRect(x1: number, y1: number, x2: number, y2: number, view: ViewRect): boolean {
  const inside = (x: number, y: number) => x >= view.left && x <= view.right && y >= view.top && y <= view.bottom;
  if (inside(x1, y1) || inside(x2, y2)) return true;
  const { left, top, right, bottom } = view;
  return segmentsCross(x1, y1, x2, y2, left, top, right, top)
    || segmentsCross(x1, y1, x2, y2, right, top, right, bottom)
    || segmentsCross(x1, y1, x2, y2, right, bottom, left, bottom)
    || segmentsCross(x1, y1, x2, y2, left, bottom, left, top);
}

/** An edge is visible when either card meets the viewport or the line crosses it. */
export function edgeIntersectsView(source: NodeBox | undefined, target: NodeBox | undefined, view: ViewRect): boolean {
  if (!source || !target) return false;
  if (boxIntersects(source, view) || boxIntersects(target, view)) return true;
  return segmentHitsRect(
    source.x + source.w / 2,
    source.y + source.h / 2,
    target.x + target.w / 2,
    target.y + target.h / 2,
    view,
  );
}

export function countVisibleEdges(
  edges: { source: string; target: string }[],
  boxes: Map<string, NodeBox>,
  view: ViewRect,
): number {
  let visible = 0;
  for (const edge of edges) {
    if (edgeIntersectsView(boxes.get(edge.source), boxes.get(edge.target), view)) visible += 1;
  }
  return visible;
}
