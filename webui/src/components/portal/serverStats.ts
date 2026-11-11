import type { PortalServer } from '../../types';
import type { PortalCopy } from './portalCopy';

export interface ServerStat {
  key: string;
  label: string;
  value: number;
}

/** Card counters. Volumes and folders are separate: a volume count is never labeled as folders. */
export function serverStatItems(server: Pick<PortalServer, 'containers' | 'databases' | 'volumes' | 'folders' | 'images'>, copy: PortalCopy): ServerStat[] {
  return [
    { key: 'containers', label: copy.statContainers, value: server.containers ?? 0 },
    { key: 'databases', label: copy.statDatabases, value: server.databases ?? 0 },
    { key: 'volumes', label: copy.statVolumes, value: server.volumes ?? 0 },
    { key: 'folders', label: copy.statFolders, value: server.folders ?? 0 },
    { key: 'images', label: copy.statImages, value: server.images ?? 0 },
  ];
}
