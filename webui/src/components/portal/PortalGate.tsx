import { useEffect, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import { api, portalFailure, setPortalSession } from '../../api';
import type { PortalProfile, PortalServer } from '../../types';
import styles from './PortalGate.module.css';
import ClientHeader from './ClientHeader';
import ServerCard from './ServerCard';
import {
  applyPortalLang,
  portalCopy,
  readPortalLang,
  ssoErrorText,
  type PortalLang,
} from './portalCopy';

interface PortalGateProps {
  children: ReactNode;
}

function EyeIcon({ hidden }: { hidden: boolean }) {
  if (hidden) {
    return (
      <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
        <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z" />
        <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" strokeWidth="1.8" />
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" d="M3 3l18 18M10.5 10.7A3 3 0 0013.3 13.5M9.9 5.2A10.8 10.8 0 0121 12a10.8 10.8 0 01-2.2 3.1M6.2 6.2A10.8 10.8 0 003 12a10.8 10.8 0 0014.7 6.3" />
    </svg>
  );
}

export default function PortalGate({ children }: PortalGateProps) {
  const queryClient = useQueryClient();
  const [lang, setLang] = useState<PortalLang>('ar');
  const [phase, setPhase] = useState<'checking' | 'login' | 'servers' | 'pending' | 'map'>('checking');
  const [mode, setMode] = useState<'login' | 'signup'>('login');
  const [login, setLogin] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [showSecret, setShowSecret] = useState(false);
  const [ssoConfigured, setSsoConfigured] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const [error, setError] = useState('');
  const [ssoCode, setSsoCode] = useState('');
  const [csrf, setCsrf] = useState('');
  const [servers, setServers] = useState<PortalServer[]>([]);
  const [current, setCurrent] = useState<PortalServer | null>(null);
  const [pendingNote, setPendingNote] = useState('');

  const t = portalCopy(lang);
  const [, setSearchParams] = useSearchParams();
  const [profile, setProfile] = useState<PortalProfile | null>(null);

  useEffect(() => {
    const next = readPortalLang();
    setLang(next);
    applyPortalLang(next);
  }, []);

  const switchLang = () => {
    const next: PortalLang = lang === 'ar' ? 'en' : 'ar';
    setLang(next);
    applyPortalLang(next);
  };

  const loadRoster = async (token: string) => {
    const wanted = new URLSearchParams(window.location.search).get('server');
    setPortalSession('', token);
    const res = await api.servers();
    const list = res.data ?? [];
    setServers(list);
    const match = wanted ? list.find(item => item.id === wanted) : undefined;
    if (match && match.status === 'ready') {
      setCurrent(match);
      setPortalSession(match.id, token);
      setPhase('map');
      return;
    }
    setPhase('servers');
  };

  useEffect(() => {
    const code = new URLSearchParams(window.location.search).get('sso_error');
    if (code === 'unavailable' || code === 'rejected' || code === 'unconfigured') {
      setSsoCode(code);
    }
    if (code) window.history.replaceState(null, '', window.location.pathname);
    api.oidc().then(res => setSsoConfigured(res.data.configured)).catch(() => setSsoConfigured(false));
  }, []);

  useEffect(() => {
    let cancelled = false;
    api.me().then(res => {
      if (cancelled) return;
      setCsrf(res.data.csrf);
      setProfile(res.data);
      return loadRoster(res.data.csrf);
    }).catch(() => {
      if (!cancelled) setPhase('login');
    });
    return () => { cancelled = true; };
  }, []);

  const formError = error || (ssoCode ? ssoErrorText(ssoCode, lang) : '');

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSsoCode('');
    setBusy(true);
    try {
      const res = await api.login(login.trim(), password);
      setPassword('');
      setCsrf(res.data.csrf);
      const me = await api.me();
      setProfile(me.data);
      await loadRoster(res.data.csrf);
    } catch (err) {
      setError(portalFailure(err, lang));
    } finally {
      setBusy(false);
    }
  };

  const submitSignup = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSsoCode('');
    setBusy(true);
    try {
      const res = await api.register(login.trim(), password, confirm);
      setPassword('');
      setConfirm('');
      setCsrf(res.data.csrf);
      setServers(res.data.servers ?? []);
      setPortalSession('', res.data.csrf);
      try {
        const me = await api.me();
        setProfile(me.data);
      } catch {
        /* roster still opens */
      }
      setPhase('servers');
    } catch (err) {
      setError(portalFailure(err, lang));
    } finally {
      setBusy(false);
    }
  };

  const secretWarnings = (() => {
    const notes: string[] = [];
    if (password.length > 0 && password !== password.trim()) notes.push(t.spacePw);
    if (confirm.length > 0 && confirm !== confirm.trim()) notes.push(t.spaceConfirm);
    if (capsLock) notes.push(t.caps);
    return notes;
  })();
  const onSecretKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    setCapsLock(e.getModifierState('CapsLock'));
  };

  const openServer = (srv: PortalServer, view: 'cards' | 'map' = 'map') => {
    setCurrent(srv);
    if (srv.status !== 'ready') {
      setPortalSession('', csrf);
      setPhase('pending');
      return;
    }
    setSearchParams(prev => {
      const params = new URLSearchParams(prev);
      params.set('server', srv.id);
      params.set('view', view === 'cards' ? 'cards' : 'map');
      return params;
    }, { replace: true });
    setPortalSession(srv.id, csrf);
    setPhase('map');
  };

  const mergeServer = (next: PortalServer) => {
    setServers(list => list.map(item => item.id === next.id ? {
      ...item,
      ...next,
      ssh_public_key: next.ssh_public_key ?? '',
      ssh_fingerprint: next.ssh_fingerprint ?? '',
    } : item));
  };

  const refresh = async () => {
    if (!current) return;
    setError('');
    try {
      setPortalSession(current.id, csrf);
      await api.requestRefresh(current.id);
      setPendingNote('Receipt recorded. The map stays empty until this server’s collector posts a snapshot. GoXIO did not connect to the host.');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Refresh failed');
    }
  };

  const signOut = async () => {
    try { await api.logout(); } catch { /* cookie cleared locally */ }
    setPortalSession('', '');
    queryClient.clear();
    setCurrent(null);
    setServers([]);
    setProfile(null);
    setMode('login');
    setConfirm('');
    setPhase('login');
  };

  if (phase === 'checking') {
    return <p className={styles.center}>{t.checking}</p>;
  }

  if (phase === 'login') {
    const signup = mode === 'signup';
    return (
      <div className={styles.gate}>
        <button type="button" className={styles.lang} onClick={switchLang}>
          {t.langOther}
        </button>
        <form className={styles.card} onSubmit={signup ? submitSignup : submit}>
          <div className={styles.brand}>
            <img className={styles.logo} src="/omxio-icon.png" width={72} height={72} alt="" />
            <p className={styles.wm} translate="no"><b className={styles.wmo}>O</b>mxio</p>
            <p className={styles.product}>{t.productBy}</p>
          </div>
          <h1 className={styles.title}>{signup ? t.titleSignup : t.titleLogin}</h1>
          <label className={styles.field}>
            <span>{t.email}</span>
            <input
              value={login}
              onChange={e => setLogin(e.target.value)}
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              spellCheck={false}
              enterKeyHint="next"
              inputMode="email"
              required
            />
          </label>
          <label className={styles.field}>
            <span>{t.password}</span>
            {signup && <small className={styles.hint}>{t.passwordHint}</small>}
            <span className={styles.secret}>
              <input
                type={showSecret ? 'text' : 'password'}
                value={password}
                onChange={e => setPassword(e.target.value)}
                onKeyUp={onSecretKey}
                onKeyDown={onSecretKey}
                autoComplete={signup ? 'new-password' : 'current-password'}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                enterKeyHint={signup ? 'next' : 'go'}
                required
              />
              <button
                type="button"
                className={styles.peek}
                onClick={() => setShowSecret(v => !v)}
                aria-label={showSecret ? t.hidePassword : t.showPassword}
              >
                <EyeIcon hidden={!showSecret} />
              </button>
            </span>
          </label>
          {signup && (
            <label className={styles.field}>
              <span>{t.confirm}</span>
              <input
                type={showSecret ? 'text' : 'password'}
                value={confirm}
                onChange={e => setConfirm(e.target.value)}
                onKeyUp={onSecretKey}
                onKeyDown={onSecretKey}
                autoComplete="new-password"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                enterKeyHint="go"
                required
              />
            </label>
          )}
          {secretWarnings.map(note => <p key={note} className={styles.warn}>{note}</p>)}
          {formError && <p className={styles.err} role="alert">{formError}</p>}
          <button type="submit" className={styles.primary} disabled={busy}>
            {signup ? t.submitSignup : t.submitLogin}
          </button>
          {ssoConfigured && (
            <>
              <div className={styles.divider} role="separator"><span>{t.or}</span></div>
              <a className={styles.secondary} href="/api/portal/oidc/start">{t.oidc}</a>
            </>
          )}
          <button
            type="button"
            className={styles.footer}
            disabled={busy}
            onClick={() => { setMode(signup ? 'login' : 'signup'); setError(''); setSsoCode(''); setConfirm(''); }}
          >
            {signup ? (
              <>{t.toLoginPrefix} <span>{t.toLoginAction}</span></>
            ) : (
              <>{t.toSignupPrefix} <span>{t.toSignupAction}</span></>
            )}
          </button>
        </form>
      </div>
    );
  }

  if (phase === 'servers' || phase === 'pending') {
    return (
      <div className={styles.gate}>
        <button type="button" className={styles.lang} onClick={switchLang}>
          {t.langOther}
        </button>
        <div className={`${styles.card} ${styles.roster}`} dir={lang === 'ar' ? 'rtl' : 'ltr'} lang={lang}>
          <div className={styles.brand}>
            <img className={styles.logo} src="/omxio-icon.png" width={72} height={72} alt="" />
            <p className={styles.wm} translate="no"><b className={styles.wmo}>O</b>mxio</p>
            <p className={styles.product}>{t.productBy}</p>
          </div>
          {profile && (
            <ClientHeader
              profile={profile}
              lang={lang}
              copy={t}
              onUpdated={setProfile}
            />
          )}
          <h2 className={styles.serversHeading}>{t.serversTitle}</h2>
          <p className={styles.serversLead}>{t.serversLead}</p>
          {servers.length === 0 && <p>{t.serversEmpty}</p>}
          <ul className={styles.list}>
            {servers.map(srv => (
              <li key={srv.id}>
                <ServerCard
                  server={srv}
                  lang={lang}
                  copy={t}
                  onOpen={openServer}
                  onUpdated={mergeServer}
                />
              </li>
            ))}
          </ul>
          {phase === 'pending' && current && (
            <div className={styles.pending}>
              <h2>{current.name}</h2>
              <p>{current.detail || t.pendingDefault}</p>
              {pendingNote && <p>{pendingNote}</p>}
              {error && <p className={styles.err}>{error}</p>}
              <button type="button" className={styles.secondary} onClick={refresh}>{t.refresh}</button>
            </div>
          )}
          {ssoConfigured && (
            <form method="post" action="/api/portal/oidc/link">
              <input type="hidden" name="csrf" value={csrf} />
              <button type="submit" className={styles.link}>{t.oidc}</button>
            </form>
          )}
          <button type="button" className={styles.link} onClick={signOut}>{t.signOut}</button>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.shell}>
      <button type="button" className={styles.switch} onClick={() => {
        setPortalSession('', csrf);
        setSearchParams(prev => {
          const params = new URLSearchParams(prev);
          params.delete('server');
          params.delete('view');
          return params;
        }, { replace: true });
        setPhase('servers');
      }}>
        {t.backToServers}
      </button>
      {children}
    </div>
  );
}
