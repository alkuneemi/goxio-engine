import { useState } from 'react';
import { api, portalFailure } from '../../api';
import type { PortalProfile } from '../../types';
import styles from './PortalGate.module.css';
import { formatWhen } from './format';
import type { PortalCopy, PortalLang } from './portalCopy';

function BuildingIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" d="M4 20V8l8-4 8 4v12" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" d="M9 20v-6h6v6M10 10h.01M14 10h.01M10 13h.01M14 13h.01" />
    </svg>
  );
}

interface ClientHeaderProps {
  profile: PortalProfile;
  lang: PortalLang;
  copy: PortalCopy;
  onUpdated: (next: PortalProfile) => void;
}

export default function ClientHeader({ profile, lang, copy: t, onUpdated }: ClientHeaderProps) {
  const [name, setName] = useState(profile.company_name ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');

  const save = async () => {
    setError('');
    setNote('');
    setBusy(true);
    try {
      const res = await api.updateProfile(name);
      onUpdated(res.data);
      setName(res.data.company_name ?? '');
      setNote(t.companySaved);
    } catch (err) {
      setError(portalFailure(err, lang));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={styles.client} aria-label={t.companyName}>
      <div className={styles.clientHead}>
        <span className={styles.sshIcon}><BuildingIcon /></span>
        <div className={styles.clientTitles}>
          <h1 className={styles.clientTitle}>{profile.company_name || t.companyName}</h1>
          <p className={styles.clientLead}>{t.companyLead}</p>
        </div>
      </div>
      <label className={styles.field}>
        <span>{t.companyName}</span>
        <input
          className={styles.companyInput}
          value={name}
          onChange={e => setName(e.target.value)}
          placeholder={t.companyPlaceholder}
          maxLength={80}
          dir={lang === 'ar' ? 'rtl' : 'ltr'}
        />
      </label>
      {error && <p className={styles.err} role="alert">{error}</p>}
      {note && <p className={styles.sshNote}>{note}</p>}
      <button type="button" className={styles.secondary} disabled={busy} onClick={save}>
        {t.companySave}
      </button>
      <dl className={styles.metaList}>
        <div>
          <dt>{t.clientLogin}</dt>
          <dd dir="ltr">{profile.login}</dd>
        </div>
        {profile.email && (
          <div>
            <dt>{t.clientEmail}</dt>
            <dd dir="ltr">{profile.email}</dd>
          </div>
        )}
        <div>
          <dt>{t.clientAccount}</dt>
          <dd className={styles.mono} dir="ltr">{profile.id}</dd>
        </div>
        <div>
          <dt>{t.clientCreated}</dt>
          <dd>{formatWhen(profile.created_at, lang)}</dd>
        </div>
        <div>
          <dt>{t.clientServers}</dt>
          <dd>{profile.server_count}</dd>
        </div>
      </dl>
    </section>
  );
}
