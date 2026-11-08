export type PortalLang = 'ar' | 'en';

export const PORTAL_LANG_KEY = 'goxio-lang';

const copy = {
  ar: {
    checking: 'جارٍ التحقق',
    titleLogin: 'تسجيل الدخول',
    titleSignup: 'إنشاء حساب',
    productBy: 'GoXIO من أومكسيو',
    email: 'البريد',
    password: 'كلمة المرور',
    confirm: 'تأكيد كلمة المرور',
    passwordHint: '١٢ حرفاً على الأقل',
    submitLogin: 'دخول',
    submitSignup: 'إنشاء الحساب',
    or: 'أو',
    oidc: 'المتابعة بحساب أومكسيو',
    toSignupPrefix: 'ما عندك حساب؟',
    toSignupAction: 'أنشئ حساب',
    toLoginPrefix: 'عندك حساب؟',
    toLoginAction: 'سجل الدخول',
    showPassword: 'إظهار كلمة المرور',
    hidePassword: 'إخفاء كلمة المرور',
    caps: 'مفتاح الأحرف الكبيرة يعمل',
    spacePw: 'مسافة في طرف كلمة المرور',
    spaceConfirm: 'مسافة في طرف التأكيد',
    ssoUnavailable: 'تعذر الدخول بحساب أومكسيو',
    ssoRejected: 'رُفض الدخول بحساب أومكسيو',
    ssoUnconfigured: 'الدخول بحساب أومكسيو غير متاح',
    langOther: 'English',
  },
  en: {
    checking: 'Checking',
    titleLogin: 'Sign in',
    titleSignup: 'Create account',
    productBy: 'GoXIO by Omxio',
    email: 'Email',
    password: 'Password',
    confirm: 'Confirm password',
    passwordHint: 'At least 12 characters',
    submitLogin: 'Sign in',
    submitSignup: 'Create account',
    or: 'or',
    oidc: 'Continue with Omxio',
    toSignupPrefix: 'No account?',
    toSignupAction: 'Create one',
    toLoginPrefix: 'Have an account?',
    toLoginAction: 'Sign in',
    showPassword: 'Show password',
    hidePassword: 'Hide password',
    caps: 'Caps Lock is on',
    spacePw: 'A space at the start or end is kept',
    spaceConfirm: 'A space on the confirmation is kept',
    ssoUnavailable: 'Could not continue with Omxio',
    ssoRejected: 'Omxio declined this sign-in',
    ssoUnconfigured: 'Omxio sign-in is not available',
    langOther: 'العربية',
  },
} as const;

export type PortalCopy = (typeof copy)[PortalLang];

export function readPortalLang(): PortalLang {
  try {
    const stored = localStorage.getItem(PORTAL_LANG_KEY);
    if (stored === 'en' || stored === 'ar') return stored;
  } catch {
    /* ignore */
  }
  return 'ar';
}

export function applyPortalLang(lang: PortalLang) {
  document.documentElement.lang = lang;
  document.documentElement.dir = lang === 'ar' ? 'rtl' : 'ltr';
  try { localStorage.setItem(PORTAL_LANG_KEY, lang); } catch { /* ignore */ }
}

export function portalCopy(lang: PortalLang): PortalCopy {
  return copy[lang];
}

export function ssoErrorText(code: string, lang: PortalLang): string {
  const t = copy[lang];
  if (code === 'unavailable') return t.ssoUnavailable;
  if (code === 'rejected') return t.ssoRejected;
  if (code === 'unconfigured') return t.ssoUnconfigured;
  return '';
}
