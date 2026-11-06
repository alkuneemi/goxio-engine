import { useEffect, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api, portalFailure, setPortalSession } from '../../api';
import type { PortalServer } from '../../types';
import styles from './PortalGate.module.css';

interface PortalGateProps {
  children: ReactNode;
}

export default function PortalGate({ children }: PortalGateProps) {
  const queryClient = useQueryClient();
  const [phase, setPhase] = useState<'checking' | 'login' | 'servers' | 'pending' | 'map'>('checking');
  const [mode, setMode] = useState<'login' | 'signup'>('login');
  const [login, setLogin] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [showSecret, setShowSecret] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const [error, setError] = useState('');
  const [csrf, setCsrf] = useState('');
  const [servers, setServers] = useState<PortalServer[]>([]);
  const [current, setCurrent] = useState<PortalServer | null>(null);
  const [pendingNote, setPendingNote] = useState('');

  const loadServers = async (token: string) => {
    setPortalSession('', token);
    const res = await api.servers();
    setServers(res.data ?? []);
    setPhase('servers');
  };

  useEffect(() => {
    let cancelled = false;
    api.me().then(res => {
      if (cancelled) return;
      setCsrf(res.data.csrf);
      return loadServers(res.data.csrf);
    }).catch(() => {
      if (!cancelled) setPhase('login');
    });
    return () => { cancelled = true; };
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const res = await api.login(login.trim(), password);
      setPassword('');
      setCsrf(res.data.csrf);
      await loadServers(res.data.csrf);
    } catch (err) {
      setError(portalFailure(err));
    } finally {
      setBusy(false);
    }
  };

  const submitSignup = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const res = await api.register(login.trim(), password, confirm);
      setPassword('');
      setConfirm('');
      setCsrf(res.data.csrf);
      setServers(res.data.servers ?? []);
      setPortalSession('', res.data.csrf);
      setPhase('servers');
    } catch (err) {
      setError(portalFailure(err));
    } finally {
      setBusy(false);
    }
  };

  const secretWarnings = (() => {
    const notes: string[] = [];
    if (password.length > 0 && password !== password.trim()) {
      notes.push('هناك مسافة في طرف كلمة المرور ولن تُحذف. A space at the end or start is part of the password and is not removed.');
    }
    if (confirm.length > 0 && confirm !== confirm.trim()) {
      notes.push('هناك مسافة في طرف التأكيد ولن تُحذف. A space on the confirmation is kept.');
    }
    if (capsLock) {
      notes.push('مفتاح الأحرف الكبيرة يعمل. الحرف الكبير يُحسب. Caps Lock is on. Capitals count.');
    }
    return notes;
  })();
  const onSecretKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    setCapsLock(e.getModifierState('CapsLock'));
  };

  const openServer = (srv: PortalServer) => {
    setCurrent(srv);
    if (srv.status !== 'ready') {
      setPortalSession('', csrf);
      setPhase('pending');
      return;
    }
    setPortalSession(srv.id, csrf);
    setPhase('map');
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
    setMode('login');
    setConfirm('');
    setPhase('login');
  };

  if (phase === 'checking') {
    return <p className={styles.center}>Checking session…</p>;
  }

  if (phase === 'login') {
    const signup = mode === 'signup';
    return (
      <div className={styles.gate}>
      <form className={styles.card} dir="rtl" lang="ar" onSubmit={signup ? submitSignup : submit}>
        <h1 dir="ltr"><span className={styles.go}>Go</span><span className={styles.xio}>XIO</span></h1>
        {signup ? (
          <>
            <p>إنشاء حساب جديد وفارغ. لا يرى خوادم أي حساب آخر، ولا يطلب مفتاح SSH.</p>
            <p dir="ltr">A new account starts empty. It cannot see another account’s servers, and it does not ask for an SSH key.</p>
          </>
        ) : (
          <>
            <p>تسجيل الدخول إلى خوادمك. هذا حساب GoXIO، وليس منتجًا آخر.</p>
            <p dir="ltr">Sign in to your servers. This is the GoXIO portal, not another product.</p>
          </>
        )}
        <label>
          <span>البريد أو اسم الدخول</span>
          <span className={styles.hint} dir="ltr">Email or login name</span>
          <input value={login} onChange={e => setLogin(e.target.value)} autoComplete="username" autoCapitalize="none" autoCorrect="off" spellCheck={false} enterKeyHint="next" required />
        </label>
        <label>
          <span>كلمة المرور</span>
          <span className={styles.hint} dir="ltr">{signup ? 'Password, at least 12 characters' : 'Password'}</span>
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
            <button type="button" className={styles.peek} onClick={() => setShowSecret(v => !v)}>
              {showSecret ? 'إخفاء · Hide' : 'إظهار · Show'}
            </button>
          </span>
        </label>
        {signup && (
          <label>
            <span>تأكيد كلمة المرور</span>
            <span className={styles.hint} dir="ltr">Confirm password</span>
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
        {secretWarnings.map(note => <p key={note} className={styles.warn} dir="ltr">{note}</p>)}
        <div className={styles.help}>
          {signup ? (
            <>
              <p>الحساب لا يُربط بحساب موجود. إن كان الدخول مستخدمًا تُرفض المحاولة ولا تُفتح جلسة. لا يُرسل بريد.</p>
              <p dir="ltr">The account is not linked to an existing one. If the login is taken, nothing is opened. No email is sent.</p>
            </>
          ) : (
            <>
              <p>إن رُفض الدخول لا تُفتح جلسة. كلمة المرور تُرسل كما هي، والمسافة وحالة الأحرف جزء منها. لا يُرسل بريد لإعادة التعيين.</p>
              <p dir="ltr">A rejected sign-in opens no session. The password is sent unchanged. Spaces and capitals count. GoXIO does not email a reset.</p>
            </>
          )}
        </div>
        {error && <p className={styles.err} role="alert">{error}</p>}
        <button type="submit" disabled={busy}>{signup ? <><bdi>إنشاء الحساب</bdi> · <bdi dir="ltr">Create account</bdi></> : <><bdi>دخول</bdi> · <bdi dir="ltr">Sign in</bdi></>}</button>
        <button
          type="button"
          className={styles.link}
          disabled={busy}
          onClick={() => { setMode(signup ? 'login' : 'signup'); setError(''); setConfirm(''); }}
        >
          {signup ? <><bdi>لديك حساب؟ تسجيل الدخول</bdi> · <bdi dir="ltr">Sign in</bdi></> : <><bdi>إنشاء حساب</bdi> · <bdi dir="ltr">Create an account</bdi></>}
        </button>
      </form>
      </div>
    );
  }

  if (phase === 'servers' || phase === 'pending') {
    return (
      <div className={styles.gate}>
      <div className={styles.card} dir="rtl" lang="ar">
        <h1 dir="ltr"><span className={styles.go}>Go</span><span className={styles.xio}>XIO</span></h1>
        <p>خوادمك فقط. الخادم الفارغ يبقى قيد التجهيز، بلا خريطة تجريبية وبلا جرد حساب آخر.</p>
        <p dir="ltr">Your servers only. An empty server stays pending. There is no demo map and no other account’s inventory.</p>
        {servers.length === 0 && (
          <>
            <p>لا توجد خوادم في هذا الحساب. الخريطة فارغة ولم يُنسخ أي جرد.</p>
            <p dir="ltr">This account has no servers. The map is empty and no inventory was copied.</p>
          </>
        )}
        <ul className={styles.list}>
          {servers.map(srv => (
            <li key={srv.id}>
              <button type="button" onClick={() => openServer(srv)}>
                <strong>{srv.name}</strong>
                <span>{srv.address}</span>
                <span>{srv.status}{srv.partial ? ' · partial' : ''}{srv.complete ? '' : srv.status === 'ready' ? ' · incomplete' : ''}</span>
              </button>
            </li>
          ))}
        </ul>
        {phase === 'pending' && current && (
          <div className={styles.pending}>
            <h2>{current.name} is pending</h2>
            <p>{current.detail || 'Map is being prepared. No inventory has been received for this server.'}</p>
            <p>خريطتك تتجهز</p>
            {pendingNote && <p>{pendingNote}</p>}
            {error && <p className={styles.err}>{error}</p>}
            <button type="button" onClick={refresh}>Request refresh</button>
          </div>
        )}
        <button type="button" className={styles.link} onClick={signOut}><bdi>خروج</bdi> · <bdi dir="ltr">Sign out</bdi></button>
      </div>
      </div>
    );
  }

  return (
    <div className={styles.shell}>
      <button type="button" className={styles.switch} onClick={() => { setPortalSession('', csrf); setPhase('servers'); }}>
        Servers
      </button>
      {children}
    </div>
  );
}
