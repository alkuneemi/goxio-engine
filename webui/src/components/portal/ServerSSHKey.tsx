import { useState } from 'react';
import { api, portalFailure } from '../../api';
import type { PortalServer } from '../../types';
import styles from './PortalGate.module.css';
import type { PortalCopy, PortalLang } from './portalCopy';
import { hasStoredSSHKey } from './format';

function KeyIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <circle cx="8" cy="12" r="3.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" d="M11.2 12H21v2.4h-2.1V12" />
    </svg>
  );
}

interface ServerSSHKeyProps {
  server: PortalServer;
  lang: PortalLang;
  copy: PortalCopy;
  onUpdated: (server: PortalServer) => void;
}

export default function ServerSSHKey({ server, lang, copy: t, onUpdated }: ServerSSHKeyProps) {
  const [value, setValue] = useState(server.ssh_public_key ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const stored = hasStoredSSHKey(server);

  const explain = (err: unknown) => {
    const raw = err instanceof Error ? err.message : '';
    if (raw.includes('private ssh keys are not accepted')) return t.sshPrivate;
    if (raw.includes('invalid ssh public key') || raw.includes('unsupported ssh public key')) return t.sshInvalid;
    return portalFailure(err, lang);
  };

  const save = async () => {
    setError('');
    setNote('');
    setBusy(true);
    try {
      const res = await api.setServerSSHKey(server.id, value);
      onUpdated(res.data);
      setValue(res.data.ssh_public_key ?? value.trim());
      setNote(t.sshSaved);
    } catch (err) {
      setError(explain(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setError('');
    setNote('');
    setBusy(true);
    try {
      const res = await api.deleteServerSSHKey(server.id);
      onUpdated(res.data);
      setValue('');
      setNote(t.sshDeleted);
    } catch (err) {
      setError(explain(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={styles.ssh} aria-labelledby={`ssh-${server.id}`}>
      <div className={styles.sshHead}>
        <span className={styles.sshIcon}><KeyIcon /></span>
        <div>
          <h3 id={`ssh-${server.id}`} className={styles.sshTitle}>{t.sshTitle}</h3>
          <p className={styles.sshLead}>{t.sshLead}</p>
        </div>
      </div>
      {stored ? (
        <p className={styles.fingerprint}>
          <span>{t.sshFingerprint}</span>
          <code dir="ltr">{server.ssh_fingerprint}</code>
        </p>
      ) : (
        <p className={styles.sshEmpty}>{t.sshEmpty}</p>
      )}
      <label className={styles.field}>
        <span>{t.sshLabel}</span>
        <textarea
          className={styles.sshInput}
          value={value}
          onChange={e => setValue(e.target.value)}
          placeholder={t.sshPlaceholder}
          spellCheck={false}
          autoCapitalize="none"
          autoCorrect="off"
          rows={3}
          dir="ltr"
        />
      </label>
      {error && <p className={styles.err} role="alert">{error}</p>}
      {note && <p className={styles.sshNote}>{note}</p>}
      <div className={styles.sshActions}>
        <button type="button" className={styles.primary} disabled={busy || !value.trim()} onClick={save}>
          {stored ? t.sshReplace : t.sshSave}
        </button>
        <button type="button" className={styles.secondary} disabled={busy || !stored} onClick={remove}>
          {t.sshDelete}
        </button>
      </div>
    </section>
  );
}
