import React, { useMemo, useState, useCallback, useEffect } from 'react';
import { Panel } from '../ui';
import { useNodeFromGraph } from '../../api';
import { calculatePriority, countConnections, isCoreSpace, isSpaceNode, nodeActivity, nodeSizeLabel, pressureValue } from '../../utils';
import type { Graph, HealthStatus, PriorityTier } from '../../types';
import ActionModal, { type SpaceActionName } from '../space/ActionModal';
import styles from './NodeInspector.module.css';

interface NodeInspectorProps {
  nodeId: string | null;
  onClose: () => void;
  graph?: Graph;
  onNodeSelect?: (nodeId: string) => void;
}

const healthLabels: Record<HealthStatus, string> = {
  healthy: 'Operational',
  degraded: 'Degraded',
  unhealthy: 'Critical',
  unknown: 'Unknown',
};

const priorityLabels: Record<PriorityTier, string> = {
  critical: 'Critical',
  high: 'High',
  medium: 'Medium',
  low: 'Low',
};

export default function NodeInspector({ nodeId, onClose, graph, onNodeSelect }: NodeInspectorProps) {
  const node = useNodeFromGraph(nodeId);
  const [copied, setCopied] = useState(false);
  const [action, setAction] = useState<SpaceActionName | null>(null);
  useEffect(() => { setAction(null); }, [nodeId]);

  const handleCopyId = useCallback(() => {
    if (!node) return;
    navigator.clipboard.writeText(node.id).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [node]);

  const priority = useMemo(() => {
    if (!node || !graph?.edges) return 'low' as PriorityTier;
    const cc = countConnections(node.id, graph.edges);
    return calculatePriority(node, cc);
  }, [node, graph]);

  const connections = useMemo(() => {
    if (!nodeId || !graph?.nodes || !graph?.edges) return { incoming: [] as { id: string; name: string }[], outgoing: [] as { id: string; name: string }[] };

    const nodeMap = new Map(graph.nodes.map(n => [n.id, n.name]));
    const incoming: { id: string; name: string }[] = [];
    const outgoing: { id: string; name: string }[] = [];

    graph.edges.forEach(edge => {
      if (edge.target === nodeId) {
        incoming.push({ id: edge.source, name: nodeMap.get(edge.source) || edge.source });
      }
      if (edge.source === nodeId) {
        outgoing.push({ id: edge.target, name: nodeMap.get(edge.target) || edge.target });
      }
    });

    return { incoming, outgoing };
  }, [nodeId, graph]);

  return (
    <Panel isOpen={!!nodeId} onClose={onClose} ariaLabel="Node inspector">
      {node && (
        <div key={nodeId} className={styles.inspector}>
          <header className={styles.header}>
            <div className={`${styles.accentBar} ${styles[`accent_${priority}`]}`} />

            <div className={styles.headerContent}>
              <div className={styles.titleRow}>
                <div className={styles.titleGroup}>
                  <span className={`${styles.priorityBadge} ${styles[`priority_${priority}`]}`}>
                    {priorityLabels[priority]}
                  </span>
                  <h2 className={styles.name}>{node.name}</h2>
                </div>
                <button
                  className={styles.closeBtn}
                  onClick={onClose}
                  aria-label="Close inspector"
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M18 6L6 18M6 6l12 12" />
                  </svg>
                </button>
              </div>

              <div className={styles.meta}>
                <span className={styles.typeTag}>{node.type}</span>
                <span className={`${styles.healthTag} ${styles[`health_${node.health}`]}`}>
                  <span className={styles.healthDot} />
                  {healthLabels[node.health]}
                </span>
              </div>
            </div>
          </header>

          <div className={styles.content}>
            <section className={styles.statsGrid}>
              <div className={styles.stat}>
                <span className={styles.statLabel}>ID</span>
                <span className={styles.statValueRow}>
                  <span className={styles.statValue}>{node.id}</span>
                  <button
                    className={styles.copyBtn}
                    onClick={handleCopyId}
                    aria-label="Copy node ID"
                    title="Copy ID"
                  >
                    {copied ? (
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <path d="M20 6L9 17l-5-5" />
                      </svg>
                    ) : (
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <rect x="9" y="9" width="13" height="13" rx="2" />
                        <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
                      </svg>
                    )}
                    {copied && <span className={styles.copiedFeedback}>Copied!</span>}
                  </button>
                </span>
              </div>
              {node.parent && (
                <div className={styles.stat}>
                  <span className={styles.statLabel}>Parent</span>
                  <span className={styles.statValue}>{node.parent}</span>
                </div>
              )}
            </section>

            {isSpaceNode(node) && (
              <SpaceFacts node={node} graph={graph} onNodeSelect={onNodeSelect} onAction={setAction} />
            )}

            {Object.keys(node.metadata).filter(k => k !== 'priority').length > 0 && (
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>
                  <span className={styles.sectionIcon}>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M12 2L2 7l10 5 10-5-10-5z" />
                      <path d="M2 17l10 5 10-5" />
                      <path d="M2 12l10 5 10-5" />
                    </svg>
                  </span>
                  Metadata
                </h3>
                <dl className={styles.metaList}>
                  {Object.entries(node.metadata)
                    .filter(([key]) => key !== 'priority')
                    .map(([key, value]) => (
                      <div key={key} className={styles.metaItem}>
                        <dt className={styles.metaKey}>{formatKey(key)}</dt>
                        <dd className={styles.metaValue}>{formatValue(value)}</dd>
                      </div>
                    ))}
                </dl>
              </section>
            )}

            <section className={styles.section}>
              <h3 className={styles.sectionTitle}>
                <span className={styles.sectionIcon}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <circle cx="12" cy="12" r="3" />
                    <path d="M12 2v4m0 12v4M2 12h4m12 0h4" />
                  </svg>
                </span>
                Connections
              </h3>
              {connections.incoming.length > 0 && (
                <div className={styles.connectionGroup}>
                  <span className={styles.connectionLabel}>Incoming</span>
                  <ul className={styles.connectionList}>
                    {connections.incoming.map((conn) => (
                      <li key={conn.id} className={styles.connectionItem}>
                        <button
                          className={styles.connectionLink}
                          onClick={() => onNodeSelect?.(conn.id)}
                          aria-label={`Navigate to ${conn.name}`}
                        >
                          {conn.name}
                          <svg className={styles.connectionArrow} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                            <path d="M5 12h14M12 5l7 7-7 7" />
                          </svg>
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {connections.outgoing.length > 0 && (
                <div className={styles.connectionGroup}>
                  <span className={styles.connectionLabel}>Outgoing</span>
                  <ul className={styles.connectionList}>
                    {connections.outgoing.map((conn) => (
                      <li key={conn.id} className={styles.connectionItem}>
                        <button
                          className={styles.connectionLink}
                          onClick={() => onNodeSelect?.(conn.id)}
                          aria-label={`Navigate to ${conn.name}`}
                        >
                          {conn.name}
                          <svg className={styles.connectionArrow} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                            <path d="M5 12h14M12 5l7 7-7 7" />
                          </svg>
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {connections.incoming.length === 0 && connections.outgoing.length === 0 && (
                <p className={styles.placeholder}>No connections</p>
              )}
            </section>
          </div>
        </div>
      )}
      {node && action && (
        <ActionModal node={node} graph={graph} action={action} onClose={() => setAction(null)} />
      )}
    </Panel>
  );
}

const PRESSURE_KEYS = [
  ['disk_pressure', 'Disk'],
  ['cpu_pressure', 'CPU'],
  ['ram_pressure', 'RAM'],
  ['io_pressure', 'IO'],
] as const;

function SpaceFacts({
  node, graph, onNodeSelect, onAction,
}: {
  node: NonNullable<ReturnType<typeof useNodeFromGraph>>;
  graph?: Graph;
  onNodeSelect?: (nodeId: string) => void;
  onAction: (action: SpaceActionName) => void;
}) {
  const activity = nodeActivity(node);
  const size = nodeSizeLabel(node);
  const containers = Array.isArray(node.metadata.containers) ? node.metadata.containers.map(String) : [];
  const repoId = typeof node.metadata.repo === 'string' ? node.metadata.repo : '';
  const repoName = typeof node.metadata.repo_name === 'string' ? node.metadata.repo_name : repoId;
  const account = typeof node.metadata.github_account === 'string' ? node.metadata.github_account : '';
  const path = typeof node.metadata.path === 'string' ? node.metadata.path : '';
  const server = typeof node.metadata.server === 'string' ? node.metadata.server : '';
  const state = typeof node.metadata.state === 'string' ? node.metadata.state : '';
  const note = typeof node.metadata.action_note === 'string' ? node.metadata.action_note : '';
  const blocked = isCoreSpace(node) || node.type === 'disk' || node.type === 'github_account';
  const blockReason = node.type === 'disk'
    ? 'Disks stay on the map'
    : node.type === 'github_account'
      ? 'GitHub accounts are not removed from the map'
      : 'Core spaces are never deleted or archived';
  const accountNode = graph?.nodes.find(n => n.type === 'github_account' && (n.name === account || n.id === account));
  const repoNode = graph?.nodes.find(n => n.id === repoId || (n.type === 'repo' && n.name === repoName));
  const pressures = PRESSURE_KEYS
    .map(([key, label]) => ({ key, label, value: pressureValue(node, key) }))
    .filter(p => p.value !== null);

  return (
    <section className={styles.section}>
      <h3 className={styles.sectionTitle}>Space</h3>
      <dl className={styles.metaList}>
        <Fact k="Type" v={node.type} />
        <Fact k="Path" v={path || '—'} />
        <Fact k="Size" v={size || '—'} />
        <Fact k="Activity" v={activity || '—'} />
        <Fact k="Cleanable" v={node.metadata.cleanable === true ? 'Yes' : 'No'} />
        <Fact k="Unused" v={node.metadata.unused === true ? 'Yes' : 'No'} />
        <Fact k="Server" v={server || '—'} />
        <Fact k="Provenance" v={typeof node.metadata.provenance === 'string' ? node.metadata.provenance : '—'} />
        <Fact k="Size state" v={typeof node.metadata.size_state === 'string' ? node.metadata.size_state : '—'} />
        <Fact k="Device group" v={typeof node.metadata.device_group === 'string' ? node.metadata.device_group : '—'} />
        {node.metadata.overlaps_parent === true && <Fact k="Accounting" v="Included in the parent filesystem, not a second copy" />}
        {typeof node.metadata.inspect_state === 'string' && <Fact k="Inspect" v={node.metadata.inspect_state} />}
        {state && <Fact k="State" v={state} />}
        <div className={styles.metaItem}>
          <dt className={styles.metaKey}>Containers</dt>
          <dd className={styles.metaValue}>{containers.length ? containers.join(', ') : '—'}</dd>
        </div>
        <div className={styles.metaItem}>
          <dt className={styles.metaKey}>Repo</dt>
          <dd className={styles.metaValue}>
            {repoNode ? (
              <button type="button" className={styles.connectionLink} onClick={() => onNodeSelect?.(repoNode.id)}>{repoName || repoNode.name}</button>
            ) : (repoName || '—')}
          </dd>
        </div>
        <div className={styles.metaItem}>
          <dt className={styles.metaKey}>GitHub account</dt>
          <dd className={styles.metaValue}>
            {accountNode ? (
              <button type="button" className={styles.connectionLink} onClick={() => onNodeSelect?.(accountNode.id)}>{account}</button>
            ) : (account || '—')}
          </dd>
        </div>
      </dl>
      {pressures.length > 0 ? (
        <div className={styles.pressures}>
          {pressures.map(p => (
            <div key={p.key} className={styles.pressure}>
              <span>{p.label} {Math.round((p.value ?? 0) * 100)}%</span>
              <span className={styles.pressureTrack}>
                <span style={{ width: `${Math.max(0, Math.min(100, (p.value ?? 0) * 100))}%` }} />
              </span>
            </div>
          ))}
        </div>
      ) : (
        <p className={styles.placeholder}>Disk, CPU, RAM, and IO pressure were not reported.</p>
      )}
      {note && <p className={styles.placeholder}>{note}</p>}
      <div className={styles.actions}>
        <button type="button" disabled={blocked} title={blocked ? blockReason : 'Record a delete'} onClick={() => onAction('delete')}>Delete</button>
        <button type="button" disabled={blocked} title={blocked ? blockReason : 'Record an archive'} onClick={() => onAction('archive')}>Archive</button>
        <button type="button" onClick={() => onAction('transfer')}>Transfer</button>
      </div>
    </section>
  );
}

function Fact({ k, v }: { k: string; v: string }) {
  return (
    <div className={styles.metaItem}>
      <dt className={styles.metaKey}>{k}</dt>
      <dd className={styles.metaValue}>{v}</dd>
    </div>
  );
}

function formatKey(key: string): string {
  return key
    .replace(/([A-Z])/g, ' $1')
    .replace(/[_-]/g, ' ')
    .replace(/^\w/, c => c.toUpperCase())
    .trim();
}

function formatValue(value: unknown): React.ReactNode {
  if (value === null || value === undefined) {
    return '—';
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return '—';
    return (
      <span className={styles.tagList}>
        {value.map((v, i) => (
          <span key={i} className={styles.tag}>{String(v)}</span>
        ))}
      </span>
    );
  }
  if (typeof value === 'boolean') {
    return value ? 'Yes' : 'No';
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return '—';
    return (
      <span className={styles.tagList}>
        {entries.map(([k, v]) => (
          <span key={k} className={styles.tag}>
            <span className={styles.tagKey}>{k}</span>
            <span className={styles.tagSep}>=</span>
            <span className={styles.tagVal}>{String(v)}</span>
          </span>
        ))}
      </span>
    );
  }
  return String(value);
}
