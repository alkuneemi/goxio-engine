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
    serversTitle: 'خوادمك',
    serversLead: 'هذه الخوادم مرتبطة بحسابك فقط. الخادم الفارغ يبقى قيد التجهيز بلا خريطة تجريبية وبلا جرد حساب آخر.',
    serversEmpty: 'لا توجد خوادم في هذا الحساب. الخريطة فارغة ولم يُنسخ أي جرد.',
    openServer: 'فتح الخريطة',
    pendingTitle: 'قيد التجهيز',
    pendingDefault: 'الخريطة قيد التجهيز. لم يصل جرد من هذا الخادم بعد.',
    refresh: 'طلب تحديث',
    signOut: 'خروج',
    backToServers: 'الخوادم',
    sshTitle: 'مفتاح SSH العام',
    sshLead: 'احفظ مفتاح الجهاز العام هنا لتعرفه بهذا الخادم. يمكنك استبداله أو حذفه في أي وقت.',
    sshLabel: 'المفتاح العام',
    sshPlaceholder: 'ssh-ed25519 أو ssh-rsa أو ecdsa-sha2-nistp256',
    sshFingerprint: 'البصمة',
    sshEmpty: 'لا يوجد مفتاح محفوظ',
    sshSave: 'حفظ المفتاح',
    sshReplace: 'استبدال المفتاح',
    sshDelete: 'حذف المفتاح',
    sshSaved: 'حُفظ المفتاح',
    sshDeleted: 'حُذف المفتاح',
    sshPrivate: 'لا يُقبل المفتاح الخاص هنا. الصق المفتاح العام فقط.',
    sshInvalid: 'هذا ليس مفتاحاً عاماً مقبولاً.',
    sshDetails: 'مفتاح SSH',
    sshStored: 'مفتاح محفوظ',
    companyName: 'اسم الشركة',
    companyLead: 'هذا الاسم يظهر هنا لتعرف العميل من أول نظرة.',
    companyPlaceholder: 'مثل سحاب الحلول الذكية',
    companySave: 'حفظ الاسم',
    companySaved: 'حُفظ اسم الشركة',
    clientLogin: 'الدخول',
    clientEmail: 'البريد',
    clientAccount: 'رقم الحساب',
    clientCreated: 'أُنشئ في',
    clientServers: 'الخوادم',
    statusReady: 'جاهز',
    statusPending: 'قيد التجهيز',
    lastSnapshot: 'آخر جرد',
    statContainers: 'الحاويات',
    statDatabases: 'القواعد',
    statVolumes: 'المجلدات',
    statImages: 'الصور',
    statDisk: 'القرص',
    diskOf: 'من',
    openTabs: 'فتح الخريطة بالتبويب',
    openNeural: 'فتح الخريطة بالعقد العصبية',
    sharedAddress: 'هذا العنوان مسجّل على أكثر من خادم. لكل سجل جرده ومفتاحه.',
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
    serversTitle: 'Your servers',
    serversLead: 'These servers belong only to this account. An empty server stays pending with no demo map and no inventory copied from another account.',
    serversEmpty: 'This account has no servers. The map is empty and no inventory was copied.',
    openServer: 'Open the map',
    pendingTitle: 'Pending',
    pendingDefault: 'The map is being prepared. No inventory has arrived from this server yet.',
    refresh: 'Request refresh',
    signOut: 'Sign out',
    backToServers: 'Servers',
    sshTitle: 'SSH public key',
    sshLead: 'Store this machine’s public key here so it stays with this server. You can replace it or remove it whenever you need.',
    sshLabel: 'Public key',
    sshPlaceholder: 'ssh-ed25519, ssh-rsa, or ecdsa-sha2-nistp256',
    sshFingerprint: 'Fingerprint',
    sshEmpty: 'No key is stored yet',
    sshSave: 'Save key',
    sshReplace: 'Replace key',
    sshDelete: 'Delete key',
    sshSaved: 'The key was saved',
    sshDeleted: 'The key was deleted',
    sshPrivate: 'A private key cannot be stored here. Paste the public key only.',
    sshInvalid: 'That is not an accepted public key.',
    sshDetails: 'SSH key',
    sshStored: 'A key is stored',
    companyName: 'Company name',
    companyLead: 'This name appears here so the client is clear at a glance.',
    companyPlaceholder: 'For example Sahab Smart Solutions',
    companySave: 'Save name',
    companySaved: 'The company name was saved',
    clientLogin: 'Login',
    clientEmail: 'Email',
    clientAccount: 'Account ID',
    clientCreated: 'Created',
    clientServers: 'Servers',
    statusReady: 'Ready',
    statusPending: 'Pending',
    lastSnapshot: 'Last snapshot',
    statContainers: 'Containers',
    statDatabases: 'Databases',
    statVolumes: 'Volumes',
    statImages: 'Images',
    statDisk: 'Disk',
    diskOf: 'of',
    openTabs: 'Open the map as tabs',
    openNeural: 'Open the neural node map',
    sharedAddress: 'This address is registered on more than one server. Each record keeps its own inventory and key.',
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
