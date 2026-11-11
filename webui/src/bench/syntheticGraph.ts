import type { Graph, GraphNode, HealthStatus } from '../types';

// Tiny fingerprint-like mark, matching image nodes on the map. Data URI so the
// bench does not depend on a server. The current node renderer ignores it
// until thumbnail support is wired up.
const THUMB =
  'data:image/svg+xml,' +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 48 48">' +
      '<circle cx="24" cy="24" r="22" fill="#1c1c1c"/>' +
      '<circle cx="24" cy="24" r="16" fill="none" stroke="#8b8b8b" stroke-width="1.5"/>' +
      '<circle cx="24" cy="24" r="10" fill="none" stroke="#6b6b6b" stroke-width="1.2"/>' +
      '<circle cx="24" cy="24" r="4" fill="#9a9a9a"/>' +
      '</svg>',
  );

export const HUB_ID = 'disk-sda1';

function node(
  id: string,
  type: GraphNode['type'],
  name: string,
  metadata: GraphNode['metadata'],
  parent?: string,
  health: HealthStatus = 'healthy',
): GraphNode {
  return { id, type, name, health, metadata, ...(parent ? { parent } : {}) };
}

/**
 * Synthetic space map shaped like the owner's Force-Directed view:
 * 600 nodes, 500 edges, one disk hub with 391 CONTAINS children
 * (201 volumes, 64 images, 126 folders).
 */
export function buildScaleGraph(): Graph {
  const nodes: GraphNode[] = [];
  const edges: Graph['edges'] = [];
  let edgeN = 0;
  const link = (source: string, target: string, type = 'contains', label = 'CONTAINS') => {
    edges.push({ id: `e-${edgeN++}`, source, target, type, label });
  };

  nodes.push(node('disk-sda', 'disk', 'sda', { kind: 'device-group', physical_disk: false }));
  nodes.push(node(
    HUB_ID,
    'disk',
    '/dev/sda1',
    { kind: 'mount', path: '/', filesystem: '/dev/sda1', disk_pressure: 0.72 },
    'disk-sda',
    'degraded',
  ));
  link('disk-sda', HUB_ID);

  const addChildren = (kind: string, count: number, nameOf: (i: number) => string) => {
    for (let i = 0; i < count; i++) {
      const id = `${kind}-${i}`;
      nodes.push(node(
        id,
        'volume',
        nameOf(i),
        {
          kind,
          path: `/var/lib/docker/${kind}/${i}`,
          ...(kind === 'image' ? { thumbnail: THUMB, digest: `sha256:${i.toString(16).padStart(8, '0')}` } : {}),
        },
        HUB_ID,
      ));
      link(HUB_ID, id);
    }
  };

  addChildren('volume', 201, i => (i === 0 ? 'meeto-bot-r' : `meeto-bot-${i}`));
  addChildren('image', 64, i => `ghcr.io/mailcow/nginx:1.30.4-${i}`);
  addChildren('folder', 126, i => `/var/lib/docker/volumes/data-${i}/_data`);

  // Fill out to 600 nodes / 500 edges without adding another hub.
  // 90 containers hang off the first volumes, 18 databases off containers.
  for (let i = 0; i < 90; i++) {
    const id = `container-${i}`;
    nodes.push(node(id, 'container', `ctr-${i}`, { kind: 'container', image: `ghcr.io/mailcow/nginx:1.30.4-${i % 64}` }, `volume-${i}`));
    link(`volume-${i}`, id);
  }
  for (let i = 0; i < 18; i++) {
    const id = `database-${i}`;
    nodes.push(node(id, 'database', `db-${i}`, { kind: 'database' }, `container-${i}`));
    link(`container-${i}`, id);
  }
  // Remaining nodes keep the count at 600. They are linked with the edges
  // still available (repos owned by accounts) so the edge total lands on 500.
  for (let i = 0; i < 8; i++) {
    nodes.push(node(`account-${i}`, 'github_account', `acct-${i}`, { github_account: `acct-${i}` }));
  }
  // Repos and accounts are present on the map but not given extra edges:
  // the edge budget is the hub fan-out plus the container/database links.
  for (let i = 0; i < 80; i++) {
    nodes.push(node(`repo-${i}`, 'repo', `repo-${i}`, { github_account: `acct-${i % 8}` }));
  }
  // 2 disks + 201 + 64 + 126 + 90 + 18 + 8 + 80 = 589. Eleven unattached
  // folders bring the node count to 600 without new hub edges.
  for (let i = 0; i < 11; i++) {
    nodes.push(node(`folder-extra-${i}`, 'volume', `/var/log/extra-${i}`, { kind: 'folder', path: `/var/log/extra-${i}` }));
  }

  if (nodes.length !== 600 || edges.length !== 500) {
    throw new Error(`scale graph is ${nodes.length} nodes / ${edges.length} edges, expected 600 / 500`);
  }
  const hubChildren = edges.filter(e => e.source === HUB_ID).length;
  if (hubChildren < 300) {
    throw new Error(`hub has ${hubChildren} children, expected at least 300`);
  }
  return { nodes, edges };
}

export function scaleGraphStats(graph: Graph) {
  const hubChildren = graph.edges.filter(e => e.source === HUB_ID).length;
  return {
    nodes: graph.nodes.length,
    edges: graph.edges.length,
    hubChildren,
    imagesWithThumbnail: graph.nodes.filter(n => typeof n.metadata.thumbnail === 'string').length,
  };
}
