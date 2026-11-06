import type { Graph, GraphNode } from '../../types';
import { LEVEL_LABEL, nodeActivity, nodeSizeLabel, pressureValue, spaceChildren } from '../../utils';
import styles from './DrillCards.module.css';

interface DrillCardsProps {
  graph?: Graph;
  stack: string[];
  onStack: (next: string[]) => void;
  onSelect: (nodeId: string) => void;
}

export default function DrillCards({ graph, stack, onStack, onSelect }: DrillCardsProps) {
  const byId = new Map(graph?.nodes.map(n => [n.id, n]) ?? []);
  const currentId = stack[stack.length - 1] ?? null;
  const current = currentId ? byId.get(currentId) : undefined;
  const items = graph ? spaceChildren(graph, currentId) : [];
  const title = current ? (LEVEL_LABEL[current.type] ?? 'Next') : 'Disks';
  const crumbs = stack.map(id => byId.get(id)).filter((n): n is GraphNode => !!n);

  return (
    <div className={styles.wrap} data-testid="space-cards">
      <div className={styles.toolbar}>
        <button
          type="button"
          className={styles.back}
          onClick={() => onStack(stack.slice(0, -1))}
          disabled={stack.length === 0}
        >
          Back
        </button>
        <nav className={styles.crumbs} aria-label="Space hierarchy">
          <button type="button" className={styles.crumb} onClick={() => onStack([])}>Disks</button>
          {crumbs.map((n, i) => (
            <span key={n.id} className={styles.crumbWrap}>
              <span className={styles.sep}>/</span>
              <button type="button" className={styles.crumb} onClick={() => onStack(stack.slice(0, i + 1))}>
                {n.name}
              </button>
            </span>
          ))}
        </nav>
        <h2 className={styles.title}>{title}</h2>
      </div>

      {!graph ? (
        <p className={styles.empty}>Waiting for the map.</p>
      ) : items.length === 0 ? (
        <p className={styles.empty}>
          {current
            ? 'Nothing else under this node. Its details stay in the side panel — a missing repo or database does not remove it.'
            : 'No disks in this server’s snapshot yet.'}
        </p>
      ) : (
        <ul className={styles.grid}>
          {items.map(node => {
            const activity = nodeActivity(node);
            const size = nodeSizeLabel(node);
            const pressure = pressureValue(node, 'disk_pressure');
            const nextCount = graph ? spaceChildren(graph, node.id).length : 0;
            return (
              <li key={node.id}>
                <button
                  type="button"
                  className={styles.card}
                  onClick={() => {
                    onSelect(node.id);
                    onStack([...stack, node.id]);
                  }}
                >
                  <span className={styles.cardTop}>
                    <span className={styles.type}>{node.type === 'github_account' ? 'github' : node.type}</span>
                    {activity && <span className={`${styles.activity} ${styles[`act_${activity}`]}`}>{activity}</span>}
                    {node.metadata?.unused === true && <span className={styles.unused}>unused</span>}
                  </span>
                  <span className={styles.name}>{node.name}</span>
                  <span className={styles.meta}>
                    {size || 'size n/a'}
                    {node.metadata?.cleanable === true ? ' · cleanable' : ''}
                    {nextCount > 0 ? ` · ${nextCount} inside` : ''}
                  </span>
                  {pressure !== null && (
                    <span className={styles.bar} title={`Disk pressure ${Math.round(pressure * 100)}%`}>
                      <span style={{ width: `${Math.max(0, Math.min(100, pressure * 100))}%` }} />
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
