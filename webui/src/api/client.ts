import type { GraphResponse, NodeResponse, HealthResponse, SpaceSummary, SpaceActionRequest, SpaceActionRecord } from '../types';

const API_BASE = '/api';

class ApiError extends Error {
  status: number;
  statusText: string;

  constructor(message: string, status: number, statusText: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.statusText = statusText;
  }
}

async function request<T>(endpoint: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${endpoint}`;

  const response = await fetch(url, {
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    ...options,
  });

  if (!response.ok) {
    let message = `Request failed: ${response.statusText}`;
    try {
      const body = await response.json() as { error?: string };
      if (body?.error) message = body.error;
    } catch {
      // body was not JSON
    }
    throw new ApiError(message, response.status, response.statusText);
  }

  return response.json();
}

export const api = {
  async getGraph(): Promise<GraphResponse> {
    return request<GraphResponse>('/graph');
  },

  async getNode(id: string): Promise<NodeResponse> {
    return request<NodeResponse>(`/node/${encodeURIComponent(id)}`);
  },

  async getHealth(): Promise<HealthResponse> {
    return request<HealthResponse>('/health');
  },

  async getSpace(): Promise<{ data: SpaceSummary }> {
    return request<{ data: SpaceSummary }>('/space');
  },

  async postSpaceAction(body: SpaceActionRequest): Promise<{ data: SpaceActionRecord }> {
    return request<{ data: SpaceActionRecord }>('/space/actions', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  },
};

export { ApiError };
