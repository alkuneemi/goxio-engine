import { createContext, useContext } from 'react';
import type { PortalProfile } from '../../types';
import type { PortalCopy, PortalLang } from './portalCopy';

export type MenuPlacement = 'header' | 'footer';

export interface AccountChromeValue {
  profile: PortalProfile;
  lang: PortalLang;
  copy: PortalCopy;
  open: MenuPlacement | null;
  setOpen: (next: MenuPlacement | null) => void;
  onSettings: () => void;
  onSignOut: () => void;
  onServers?: () => void;
  serversLabel?: string;
}

export const AccountChromeContext = createContext<AccountChromeValue | null>(null);

export function useAccountChrome(): AccountChromeValue | null {
  return useContext(AccountChromeContext);
}
