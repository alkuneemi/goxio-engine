import type { PortalLang } from './portalCopy';

export function formatWhen(iso: string | undefined, lang: PortalLang): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() < 2000) return '';
  return new Intl.DateTimeFormat(lang === 'ar' ? 'ar' : 'en-GB', {
    calendar: 'gregory',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(d);
}

export function formatBytes(n: number | undefined): string {
  if (!n || n <= 0) return '';
  const gib = n / 1024 ** 3;
  if (gib >= 1) return `${gib >= 10 ? gib.toFixed(0) : gib.toFixed(1)} GB`;
  const mib = n / 1024 ** 2;
  return `${mib >= 10 ? mib.toFixed(0) : mib.toFixed(1)} MB`;
}

export function hasStoredSSHKey(server: { ssh_fingerprint?: string; ssh_public_key?: string }): boolean {
  return Boolean((server.ssh_fingerprint ?? '').trim() || (server.ssh_public_key ?? '').trim());
}
