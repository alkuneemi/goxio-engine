import type { ReactElement } from 'react';
import type { PortalServer } from '../../types';
import styles from './PortalGate.module.css';
import ServerSSHKey from './ServerSSHKey';
import { formatBytes, formatWhen, hasStoredSSHKey } from './format';
import type { PortalCopy, PortalLang } from './portalCopy';
import { serverStatItems } from './serverStats';

function BoxIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M4 7.5 12 4l8 3.5V16.5L12 20l-8-3.5V7.5z" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M12 20V11M4 7.5 12 11l8-3.5" />
    </svg>
  );
}
function DbIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <ellipse cx="12" cy="7" rx="7" ry="3" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M5 7v10c0 1.7 3.1 3 7 3s7-1.3 7-3V7" />
    </svg>
  );
}
function VolumeIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <rect x="4" y="6" width="16" height="12" rx="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M8 10h8M8 14h5" />
    </svg>
  );
}
function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M3 7.5h6l2 2H21v9.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-11.5z" />
    </svg>
  );
}
function ImageIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <rect x="4" y="5" width="16" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="9" cy="10" r="1.6" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="m7 17 4-4 3 3 2-2 3 3" />
    </svg>
  );
}
function DiskIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="12" cy="12" r="2.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  );
}
function KeyMini() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <circle cx="8" cy="12" r="3.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" d="M11.2 12H21v2.4h-2.1V12" />
    </svg>
  );
}
function TabsIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M4 8h6l2 2h8v10H4V8z" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M8 8V6h6l2 2" />
    </svg>
  );
}
function NeuralIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <circle cx="6" cy="12" r="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="12" cy="6" r="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="18" cy="12" r="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="12" cy="18" r="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.6" d="M8 11 10.3 7.8M14 7.8 16.2 10.8M16 13.5 13.8 16.4M10.2 16.4 8 13.6" />
    </svg>
  );
}

interface ServerCardProps {
  server: PortalServer;
  lang: PortalLang;
  copy: PortalCopy;
  onOpen: (server: PortalServer, view: 'cards' | 'map') => void;
  onUpdated: (server: PortalServer) => void;
}

export default function ServerCard({ server, lang, copy: t, onOpen, onUpdated }: ServerCardProps) {
  const ready = server.status === 'ready';
  const stored = hasStoredSSHKey(server);
  const used = formatBytes(server.disk_used_bytes);
  const total = formatBytes(server.disk_size_bytes);
  const snapshotAt = formatWhen(server.collected_at || server.updated_at, lang);
  const icons: Record<string, ReactElement> = {
    containers: <BoxIcon />,
    databases: <DbIcon />,
    volumes: <VolumeIcon />,
    folders: <FolderIcon />,
    images: <ImageIcon />,
  };
  const stats = serverStatItems(server, t).map(stat => ({ ...stat, icon: icons[stat.key] }));

  return (
    <article className={styles.serverCard}>
      <header className={styles.cardTop}>
        <div>
          <h2 className={styles.serverName}>{server.name}</h2>
          <p className={styles.serverAddr} dir="ltr">{server.address}</p>
        </div>
        <p className={ready ? styles.statusReady : styles.statusPending}>
          {ready ? t.statusReady : t.statusPending}
        </p>
      </header>
      {snapshotAt && (
        <p className={styles.snapshot}>{t.lastSnapshot} {snapshotAt}</p>
      )}
      <ul className={styles.stats}>
        {stats.map(stat => (
          <li key={stat.key}>
            <span className={styles.statIcon}>{stat.icon}</span>
            <span>
              <strong>{stat.value}</strong>
              <em>{stat.label}</em>
            </span>
          </li>
        ))}
        {(used || total) && (
          <li className={styles.statWide}>
            <span className={styles.statIcon}><DiskIcon /></span>
            <span>
              <strong dir="ltr">{used}{used && total ? ` ${t.diskOf} ${total}` : total}</strong>
              <em>{t.statDisk}</em>
            </span>
          </li>
        )}
      </ul>
      {server.shared_address && <p className={styles.shared}>{t.sharedAddress}</p>}
      <div className={styles.entryActions}>
        <button type="button" className={styles.primary} disabled={!ready} onClick={() => onOpen(server, 'cards')}>
          <TabsIcon /> {t.openTabs}
        </button>
        <button type="button" className={styles.secondary} disabled={!ready} onClick={() => onOpen(server, 'map')}>
          <NeuralIcon /> {t.openNeural}
        </button>
      </div>
      <details className={styles.sshDetails}>
        <summary>
          <KeyMini />
          <span>{stored ? t.sshStored : t.sshEmpty}</span>
          {stored && server.ssh_fingerprint && (
            <code dir="ltr">{server.ssh_fingerprint}</code>
          )}
        </summary>
        <ServerSSHKey server={server} lang={lang} copy={t} onUpdated={onUpdated} />
      </details>
    </article>
  );
}
