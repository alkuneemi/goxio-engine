import type { Graph, SpaceActivity, SpaceSummary } from '../../types';
import type { Filters } from '../../utils';
import { SPACE_ACTIVITIES, hasSpaceFilter, isSpaceNode } from '../../utils';
import { api } from '../../api';
import styles from './SpaceBar.module.css';

export type MapView = 'cards' | 'map';

interface SpaceBarProps {
  graph?: Graph;
  summary?: SpaceSummary;
  filters: Filters;
  onFilterChange: (filters: Filters) => void;
  view: MapView;
  onView: (view: MapView) => void;
}

export default function SpaceBar({ graph, summary, filters, onFilterChange, view, onView }: SpaceBarProps) {
  const repos = (graph?.nodes ?? []).filter(n => n.type === 'repo' && isSpaceNode(n));
  const accounts = (graph?.nodes ?? []).filter(n => n.type === 'github_account' && isSpaceNode(n));
  const hasActivity = (graph?.nodes ?? []).some(n => typeof n.metadata?.activity === 'string');

  const toggleActivity = (activity: SpaceActivity) => {
    const activities = filters.activities.includes(activity)
      ? filters.activities.filter(a => a !== activity)
      : [...filters.activities, activity];
    onFilterChange({ ...filters, activities });
  };

  return (
    <div className={styles.bar}>
      <div className={styles.views} role="group" aria-label="Map view">
        <button type="button" className={view === 'cards' ? styles.on : ''} aria-pressed={view === 'cards'} onClick={() => onView('cards')}>
          Cards
        </button>
        <button type="button" className={view === 'map' ? styles.on : ''} aria-pressed={view === 'map'} onClick={() => onView('map')}>
          Map
        </button>
      </div>

      <label className={styles.field}>
        <span>Repo</span>
        <select
          aria-label="Filter by repo"
          value={filters.repos[0] ?? ''}
          onChange={e => onFilterChange({ ...filters, repos: e.target.value ? [e.target.value] : [] })}
        >
          <option value="">All repos</option>
          {repos.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
        </select>
      </label>

      <label className={styles.field}>
        <span>GitHub</span>
        <select
          aria-label="Filter by GitHub account"
          value={filters.accounts[0] ?? ''}
          onChange={e => onFilterChange({ ...filters, accounts: e.target.value ? [e.target.value] : [] })}
        >
          <option value="">All accounts</option>
          {accounts.map(a => <option key={a.id} value={a.name}>{a.name}</option>)}
        </select>
      </label>

      <button
        type="button"
        className={`${styles.chip} ${filters.unusedOnly ? styles.on : ''}`}
        aria-pressed={filters.unusedOnly}
        onClick={() => onFilterChange({ ...filters, unusedOnly: !filters.unusedOnly })}
      >
        Unused
      </button>

      {hasActivity && SPACE_ACTIVITIES.map(activity => (
        <button
          key={activity}
          type="button"
          className={`${styles.chip} ${styles[activity]} ${filters.activities.includes(activity) ? styles.on : ''}`}
          aria-pressed={filters.activities.includes(activity)}
          onClick={() => toggleActivity(activity)}
        >
          {activity}
        </button>
      ))}

      {hasSpaceFilter(filters) && (
        <button
          type="button"
          className={styles.clear}
          onClick={() => onFilterChange({ ...filters, repos: [], accounts: [], unusedOnly: false, activities: [] })}
        >
          Clear space
        </button>
      )}

      <span className={styles.source}>
        {summary
          ? `${summary.status || summary.source || 'space'} · ${summary.host || 'server'} · read-only${summary.listing_complete === false ? ' · inspect incomplete' : ''}${typeof summary.volumes === 'number' ? ` · ${summary.volumes} volumes` : ''}${typeof summary.images === 'number' ? ` · ${summary.images} images` : ''}${typeof summary.folders === 'number' ? ` · ${summary.folders} folders` : ''}${summary.sizes_partial ? ` · ${summary.sizes_partial} sizing in progress` : ''}${summary.sizes_timeout ? ` · ${summary.sizes_timeout} size timeouts` : ''}${summary.sizes_pending ? ` · ${summary.sizes_pending} sizes pending` : ''}${summary.sizes_unreadable ? ` · ${summary.sizes_unreadable} unreadable` : ''}`
          : 'read-only'}
      </span>
      <button
        type="button"
        className={styles.clear}
        onClick={() => { void api.logout().then(() => window.location.assign('/')); }}
      >
        Sign out
      </button>
    </div>
  );
}
