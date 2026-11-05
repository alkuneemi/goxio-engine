import type { GraphResponse, NodeResponse, HealthResponse, SpaceSummary, SpaceActionRequest, SpaceActionRecord, PortalServer } from '../types';

const API_BASE = '/api';

let portalServer = '';
let portalCSRF = '';

export function setPortalSession(serverId: string, csrf: string) {
  portalServer = serverId;
  portalCSRF = csrf;
}

export function portalServerId(): string {
  return portalServer;
}

function withServer(endpoint: string): string {
  if (!portalServer) return endpoint;
  const join = endpoint.includes('?') ? '&' : '?';
  return `${endpoint}${join}server=${encodeURIComponent(portalServer)}`;
}

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

  const method = (options?.method ?? 'GET').toUpperCase();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-GoXIO-Request': '1',
    ...(options?.headers as Record<string, string> | undefined),
  };
  if (method !== 'GET' && portalCSRF) {
    headers['X-CSRF-Token'] = portalCSRF;
  }

  const response = await fetch(url, {
    ...options,
    credentials: 'same-origin',
    headers,
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
    return request<GraphResponse>(withServer('/graph'));
  },

  async getNode(id: string): Promise<NodeResponse> {
    return request<NodeResponse>(withServer(`/node/${encodeURIComponent(id)}`));
  },

  async getHealth(): Promise<HealthResponse> {
    return request<HealthResponse>(withServer('/health'));
  },

  async getSpace(): Promise<{ data: SpaceSummary }> {
    return request<{ data: SpaceSummary }>(withServer('/space'));
  },

  async login(login: string, password: string): Promise<{ data: { login: string; csrf: string } }> {
    return request('/portal/login', { method: 'POST', body: JSON.stringify({ login, password }) });
  },

  async logout(): Promise<void> {
    await request('/portal/logout', { method: 'POST', body: '{}' });
    setPortalSession('', '');
  },

  async me(): Promise<{ data: { csrf: string } }> {
    return request('/portal/me');
  },

  async servers(): Promise<{ data: PortalServer[] }> {
    return request('/portal/servers');
  },

  async requestRefresh(serverId: string): Promise<void> {
    const prev = portalServer;
    portalServer = serverId;
    try {
      await request(`/portal/servers/${encodeURIComponent(serverId)}/refresh`, { method: 'POST', body: '{}' });
    } finally {
      portalServer = prev;
    }
  },

  async postSpaceAction(body: SpaceActionRequest): Promise<{ data: SpaceActionRecord }> {
    return request<{ data: SpaceActionRecord }>('/space/actions', {
      method: 'POST',
      body: JSON.stringify(body),
    });
  },
};

export { ApiError };
