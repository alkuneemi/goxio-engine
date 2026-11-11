// Below this zoom, edge labels and card chrome cost more than they show.
// 0.5 is where a CONTAINS label is still large enough to read.
export const COMPACT_ZOOM_BELOW = 0.5;

// Past this many edges, dashed strokes and per-edge labels dominate pan/zoom
// even when the user is zoomed in (an expanded hub, for example).
export const COMPACT_EDGE_AT = 80;

export type DetailLevel = 'full' | 'compact';

/**
 * Level of detail for the map. `compact` hides edge labels, draws thin solid
 * edges, and drops shadows, filters, and thumbnails. `full` is the normal-zoom
 * card and edge styling.
 */
export function detailLevel(zoom: number, edgeCount: number): DetailLevel {
  if (edgeCount >= COMPACT_EDGE_AT) return 'compact';
  if (zoom < COMPACT_ZOOM_BELOW) return 'compact';
  return 'full';
}
