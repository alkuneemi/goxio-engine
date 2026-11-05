import type { Graph, GraphNode } from './graph';

export interface ApiResponse<T> {
  data: T;
  error?: string;
}

export type GraphResponse = ApiResponse<Graph>;

export type NodeResponse = ApiResponse<GraphNode>;

export interface SpaceSummary {
  source: string;
  host: string;
  collected_at: string;
  host_writes: boolean;
  detail?: string;
  node_count: number;
  edge_count: number;
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
