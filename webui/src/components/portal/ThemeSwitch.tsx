import { useEffect, useState, type ReactNode } from 'react';
import { chooseTheme, paintTheme, readThemeChoice, type ThemeChoice } from '../../theme';
import { portalCopy, type PortalLang } from './portalCopy';
import styles from './ThemeSwitch.module.css';

function SunIcon() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <circle cx="12" cy="12" r="3.2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" d="M12 3.5v2.2M12 18.3v2.2M3.5 12h2.2M18.3 12h2.2M6 6l1.6 1.6M16.4 16.4 18 18M18 6l-1.6 1.6M7.6 16.4 6 18" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" d="M15.5 3.8A7.8 7.8 0 1 0 20.2 14 6.2 6.2 0 0 1 15.5 3.8z" />
    </svg>
  );
}

function DeviceIcon() {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
      <rect x="4" y="4" width="16" height="12" rx="2" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" d="M9 20h6" />
    </svg>
  );
}

function useThemeChoice() {
  const [choice, setChoice] = useState<ThemeChoice>(() => readThemeChoice());
  const [lang, setLang] = useState<PortalLang>(() => (document.documentElement.lang === 'en' ? 'en' : 'ar'));

  useEffect(() => {
    const sync = () => {
      const next = readThemeChoice();
      setChoice(next);
      paintTheme(next);
    };
    sync();
    const onTheme = () => setChoice(readThemeChoice());
    const onLang = () => setLang(document.documentElement.lang === 'en' ? 'en' : 'ar');
    window.addEventListener('goxio-theme', onTheme);
    const langObserver = new MutationObserver(onLang);
    langObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
    const query = window.matchMedia('(prefers-color-scheme: light)');
    query.addEventListener('change', sync);
    return () => {
      window.removeEventListener('goxio-theme', onTheme);
      langObserver.disconnect();
      query.removeEventListener('change', sync);
    };
  }, []);

  return { choice, lang, pick: (next: ThemeChoice) => { setChoice(next); chooseTheme(next); } };
}

export default function ThemeSwitch({ variant = 'compact' }: { variant?: 'compact' | 'full' }) {
  const { choice, lang, pick } = useThemeChoice();
  const t = portalCopy(lang);

  if (variant === 'compact') {
    return (
      <div className={styles.compact} role="group" aria-label={t.themeTitle} data-theme-switch="compact">
        <button
          type="button"
          className={choice === 'light' ? styles.compactOn : ''}
          aria-pressed={choice === 'light'}
          aria-label={t.themeDay}
          title={t.themeDay}
          onClick={() => pick('light')}
        >
          <SunIcon />
        </button>
        <button
          type="button"
          className={choice === 'dark' ? styles.compactOn : ''}
          aria-pressed={choice === 'dark'}
          aria-label={t.themeNight}
          title={t.themeNight}
          onClick={() => pick('dark')}
        >
          <MoonIcon />
        </button>
      </div>
    );
  }

  const options: { id: ThemeChoice; label: string; icon: ReactNode }[] = [
    { id: 'light', label: t.themeDay, icon: <SunIcon /> },
    { id: 'dark', label: t.themeNight, icon: <MoonIcon /> },
    { id: 'system', label: t.themeSystem, icon: <DeviceIcon /> },
  ];

  return (
    <section className={styles.panel} data-theme-switch="full" aria-labelledby="goxio-theme-title">
      <h2 id="goxio-theme-title" className={styles.title}>{t.themeTitle}</h2>
      <p className={styles.lead}>{t.themeLead}</p>
      <div className={styles.choices} role="radiogroup" aria-label={t.themeTitle}>
        {options.map(option => (
          <button
            key={option.id}
            type="button"
            role="radio"
            aria-checked={choice === option.id}
            className={choice === option.id ? styles.choiceOn : styles.choice}
            onClick={() => pick(option.id)}
          >
            {option.icon}
            <span>{option.label}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
