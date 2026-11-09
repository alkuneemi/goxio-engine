import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { PortalProfile } from '../../types';
import type { PortalCopy, PortalLang } from './portalCopy';
import { AccountChromeContext, useAccountChrome, type AccountChromeValue, type MenuPlacement } from './useAccountChrome';
import styles from './AccountChrome.module.css';

interface ProviderProps {
  profile: PortalProfile;
  lang: PortalLang;
  copy: PortalCopy;
  onSettings: () => void;
  onSignOut: () => void;
  onServers?: () => void;
  serversLabel?: string;
  children: ReactNode;
}

export function AccountChromeProvider({
  profile,
  lang,
  copy,
  onSettings,
  onSignOut,
  onServers,
  serversLabel,
  children,
}: ProviderProps) {
  const [open, setOpen] = useState<MenuPlacement | null>(null);
  const openSettings = useCallback(() => {
    setOpen(null);
    onSettings();
  }, [onSettings]);
  const signOut = useCallback(() => {
    setOpen(null);
    onSignOut();
  }, [onSignOut]);
  const value = useMemo<AccountChromeValue>(() => ({
    profile,
    lang,
    copy,
    open,
    setOpen,
    onSettings: openSettings,
    onSignOut: signOut,
    onServers,
    serversLabel,
  }), [profile, lang, copy, open, openSettings, signOut, onServers, serversLabel]);

  return <AccountChromeContext.Provider value={value}>{children}</AccountChromeContext.Provider>;
}

function AccountIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
      <circle cx="12" cy="8.2" r="3.1" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" d="M5.2 19.2c1.3-2.8 3.6-4.2 6.8-4.2s5.5 1.4 6.8 4.2" />
    </svg>
  );
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" d="M4 8h9M17 8h3M4 16h3M11 16h9" />
      <circle cx="15.5" cy="8" r="2.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <circle cx="8.5" cy="16" r="2.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  );
}

export function AccountMenu({ placement }: { placement: MenuPlacement }) {
  const chrome = useAccountChrome();
  const wrapRef = useRef<HTMLDivElement>(null);
  const open = chrome?.open === placement;
  const setOpen = chrome?.setOpen;

  useEffect(() => {
    if (!open || !setOpen) return;
    const onPointer = (event: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(event.target as Node)) setOpen(null);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(null);
    };
    document.addEventListener('mousedown', onPointer, true);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onPointer, true);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, setOpen]);

  if (!chrome) return null;
  const { profile, copy: t } = chrome;
  const menuId = `account-menu-${placement}`;

  return (
    <div className={styles.wrap} ref={wrapRef}>
      <button
        type="button"
        className={styles.button}
        aria-label={t.accountMenu}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        data-account-menu={placement}
        onClick={() => chrome.setOpen(open ? null : placement)}
      >
        <AccountIcon />
      </button>
      {open && (
        <div
          id={menuId}
          className={`${styles.menu} ${placement === 'footer' ? styles.menuUp : styles.menuDown}`}
          role="menu"
        >
          <p className={styles.identity} dir="ltr">{profile.email || profile.login}</p>
          <button type="button" className={styles.item} role="menuitem" onClick={chrome.onSettings}>
            <GearIcon />
            <span>{t.settings}</span>
          </button>
          <button type="button" className={`${styles.item} ${styles.itemQuiet}`} role="menuitem" onClick={chrome.onSignOut}>
            <span>{t.signOut}</span>
          </button>
        </div>
      )}
    </div>
  );
}
