import type { Graph, GraphNode } from './graph';

export interface ApiResponse<T> {
  data: T;
  error?: string;
}

export type GraphResponse = ApiResponse<Graph>;

export type NodeResponse = ApiResponse<GraphNode>;

export interface PortalServer {
  id: string;
  name: string;
  address: string;
  status: string;
  partial: boolean;
  complete: boolean;
  detail?: string;
}

export interface SpaceSummary {
  source?: string;
  status?: string;
  host: string;
  collected_at: string;
  host_writes: boolean;
  detail?: string;
  node_count?: number;
  edge_count?: number;
  partial?: boolean;
  complete?: boolean;
  missing_size_count?: number;
  truncated_count?: number;
  listed_containers?: number;
  volumes?: number;
  images?: number;
  folders?: number;
  mounts?: number;
  databases?: number;
  repos?: number;
  accounts?: number;
  sizes_measured?: number;
  sizes_pending?: number;
  sizes_timeout?: number;
  sizes_unreadable?: number;
  listing_complete?: boolean;
}

export interface SpaceActionRequest {
  node_id: string;
  action: 'delete' | 'archive' | 'transfer';
  confirm: string;
  target_account?: string;
  target_server?: string;
}

export interface SpaceActionRecord {
  id: string;
  at: string;
  node_id: string;
  node_name: string;
  action: string;
  target_account?: string;
  target_server?: string;
  applied_to_host: boolean;
  note: string;
}

export interface HealthResponse {
  status: 'ok' | 'degraded' | 'error';
  timestamp: string;
  version?: string;
}

export interface WebSocketMessage {
  type: 'health_batch' | 'node_update' | 'graph_update';
  payload: unknown;
}

export interface HealthUpdatePayload {
  nodeId: string;
  health: GraphNode['health'];
}

// The server sends only nodes whose health changed since its last tick.
export interface HealthBatchPayload {
  updates: HealthUpdatePayload[];
}

export interface NodeUpdatePayload {
  node: GraphNode;
}
