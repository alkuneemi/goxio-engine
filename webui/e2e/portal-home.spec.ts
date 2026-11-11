import { expect, test, type Page } from '@playwright/test';

const ACCOUNT_ID = 'acct_36e8d8f8c548336c4ec8c0f0865a52ab';

function freshProfile() {
  return {
    csrf: 'csrf-test',
    id: ACCOUNT_ID,
    login: 'sahabify',
    email: 'sahabify@gmail.com',
    company_name: 'سحاب الحلول الذكية',
    created_at: '2026-11-05T10:57:00Z',
    server_count: 2,
  };
}

function server(id: string, name: string) {
  return {
    id,
    name,
    address: '187.127.114.87',
    status: 'ready',
    partial: false,
    complete: true,
    ssh_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGoxioExampleKey sahabify',
    ssh_fingerprint: 'SHA256:examplefingerprint',
    containers: 22,
    databases: 146,
    volumes: 106,
    folders: 423,
    images: 201,
    disk_used_bytes: 353 * 1024 ** 3,
    disk_size_bytes: 386 * 1024 ** 3,
    shared_address: true,
    collected_at: '2026-11-09T11:33:00Z',
  };
}

async function mockPortal(page: Page, startAuthed: boolean) {
  let authed = startAuthed;
  const profile = freshProfile();
  await page.addInitScript(() => {
    localStorage.setItem('goxio-lang', 'ar');
  });
  await page.route(url => new URL(url).pathname.startsWith('/api/'), async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/portal/oidc') {
      await route.fulfill({ json: { data: { configured: false } } });
      return;
    }
    if (path === '/api/portal/login' && req.method() === 'POST') {
      authed = true;
      await route.fulfill({ json: { data: { login: 'sahabify', csrf: profile.csrf } } });
      return;
    }
    if (path === '/api/portal/logout') {
      authed = false;
      await route.fulfill({ json: { data: { ok: true } } });
      return;
    }
    if (path === '/api/portal/me') {
      if (!authed) {
        await route.fulfill({ status: 401, json: { error: 'invalid login or password' } });
        return;
      }
      await route.fulfill({ json: { data: profile } });
      return;
    }
    if (path === '/api/portal/profile' && req.method() === 'PUT') {
      const body = req.postDataJSON() as { company_name?: string };
      profile.company_name = body.company_name ?? profile.company_name;
      await route.fulfill({ json: { data: profile } });
      return;
    }
    if (path === '/api/portal/servers') {
      await route.fulfill({
        json: { data: [server('srv_one', 'sahabify-187'), server('srv_two', 'sahabify')] },
      });
      return;
    }
    if (path === '/api/graph') {
      await route.fulfill({
        json: {
          data: {
            nodes: [{ id: 'disk-a', type: 'disk', name: '/dev/sda1', health: 'unknown', metadata: {} }],
            edges: [],
          },
        },
      });
      return;
    }
    if (path === '/api/space') {
      await route.fulfill({
        json: {
          data: {
            host: '187.127.114.87',
            collected_at: '2026-11-09T11:33:00Z',
            host_writes: false,
            status: 'ready',
          },
        },
      });
      return;
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } });
  });
}

async function expectAccountChrome(page: Page) {
  const header = page.locator('[data-account-menu="header"]');
  const footer = page.locator('[data-account-menu="footer"]');
  await expect(header).toHaveCount(1);
  await expect(footer).toHaveCount(1);
  await expect(header).toBeInViewport();
  await expect(footer).toBeInViewport();
}

test.describe('post-login home', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('login opens the server cards, and settings holds the account', async ({ page }) => {
    await mockPortal(page, false);
    await page.goto('http://127.0.0.1:5173/');

    await expect(page.getByRole('heading', { name: 'تسجيل الدخول' })).toBeVisible();
    await expect(page.locator('[data-account-menu]')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'حفظ الاسم' })).toHaveCount(0);

    await page.getByLabel('البريد').fill('sahabify@gmail.com');
    await page.locator('input[type="password"]').fill('password-long-ok');
    await page.getByRole('button', { name: 'دخول' }).click();

    await expect(page.getByRole('heading', { name: 'خوادمك' })).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('dir', 'rtl');
    await expect(page.getByRole('heading', { name: 'سحاب الحلول الذكية' })).toHaveCount(0);
    await expect(page.getByText(ACCOUNT_ID)).toHaveCount(0);
    await expect(page.getByText('sahabify@gmail.com')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'حفظ الاسم' })).toHaveCount(0);

    const firstCard = page.getByRole('heading', { name: 'sahabify-187' });
    await expect(firstCard).toBeVisible();
    const box = await firstCard.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.y).toBeGreaterThan(40);
    expect(box!.y).toBeLessThan(700);

    await expect(page.getByRole('heading', { name: 'sahabify', exact: true })).toBeVisible();
    await expect(page.getByText('هذا العنوان مسجّل على أكثر من خادم. لكل سجل جرده ومفتاحه.')).toHaveCount(2);
    await expect(page.getByRole('button', { name: 'فتح الخريطة بالتبويب' })).toHaveCount(2);
    await expect(page.getByRole('button', { name: 'فتح الخريطة بالعقد العصبية' })).toHaveCount(2);
    await page.getByText('مفتاح محفوظ').first().click();
    await expect(page.getByRole('heading', { name: 'مفتاح SSH العام' })).toBeVisible();
    await expectAccountChrome(page);

    await page.locator('[data-account-menu="header"]').click();
    await expect(page.getByText('sahabify@gmail.com')).toBeVisible();
    await page.getByRole('menuitem', { name: 'الإعدادات' }).click();

    await expect(page.getByRole('heading', { name: 'الإعدادات' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'سحاب الحلول الذكية' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'اسم الشركة' })).toHaveValue('سحاب الحلول الذكية');
    await expect(page.getByText('sahabify', { exact: true })).toBeVisible();
    await expect(page.getByText('sahabify@gmail.com')).toBeVisible();
    await expect(page.getByText('2', { exact: true })).toBeVisible();
    await expectAccountChrome(page);

    const id = page.locator('[data-account-id]');
    await expect(id).toHaveText(ACCOUNT_ID);
    const metrics = await id.evaluate(node => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      const main = node.closest('main')!.getBoundingClientRect();
      return {
        whiteSpace: style.whiteSpace,
        height: rect.height,
        scrollWidth: node.scrollWidth,
        clientWidth: node.clientWidth,
        right: rect.right,
        left: rect.left,
        mainRight: main.right,
        mainLeft: main.left,
      };
    });
    expect(metrics.whiteSpace).toBe('nowrap');
    expect(metrics.height).toBeLessThan(44);
    expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 1);
    expect(metrics.left).toBeGreaterThanOrEqual(metrics.mainLeft - 1);
    expect(metrics.right).toBeLessThanOrEqual(metrics.mainRight + 1);

    await page.getByRole('textbox', { name: 'اسم الشركة' }).fill('سحاب');
    await page.getByRole('button', { name: 'حفظ الاسم' }).click();
    await expect(page.getByText('حُفظ اسم الشركة')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'سحاب' })).toBeVisible();

    await page.getByRole('button', { name: 'الخوادم' }).click();
    await expect(page.getByRole('heading', { name: 'خوادمك' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'حفظ الاسم' })).toHaveCount(0);

    await page.locator('[data-account-menu="footer"]').click();
    const footerItem = page.getByRole('menuitem', { name: 'الإعدادات' });
    await expect(footerItem).toBeVisible();
    await expect(footerItem).toBeInViewport();
    await page.keyboard.press('Escape');

    await page.getByRole('button', { name: 'فتح الخريطة بالعقد العصبية' }).first().click();
    await expect(page.getByRole('button', { name: 'Search nodes' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'الخوادم' })).toBeVisible();
    await expectAccountChrome(page);
    await page.locator('[data-account-menu="header"]').click();
    await expect(page.getByRole('menuitem', { name: 'الإعدادات' })).toBeVisible();
    await page.keyboard.press('Escape');

    await page.getByRole('button', { name: 'الخوادم' }).click();
    await page.getByRole('button', { name: 'فتح الخريطة بالتبويب' }).first().click();
    await expect(page.getByRole('button', { name: 'Cards', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expectAccountChrome(page);
    await page.getByRole('button', { name: 'Map', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Map', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.react-flow__node').first()).toBeVisible();
  });

  test('server card labels volumes and folders separately', async ({ page }) => {
    await mockPortal(page, true);
    await page.goto('http://127.0.0.1:5173/');
    const card = page.locator('article').first();
    await expect(card.locator('li', { hasText: 'وحدات التخزين' })).toContainText('106');
    await expect(card.locator('li', { hasText: 'المجلدات' })).toContainText('423');
    await page.getByRole('button', { name: 'English' }).click();
    await expect(card.locator('li', { hasText: 'Volumes' })).toContainText('106');
    await expect(card.locator('li', { hasText: 'Folders' })).toContainText('423');
  });

  test('an authenticated visit to the root opens the server cards', async ({ page }) => {
    await mockPortal(page, true);
    await page.goto('http://127.0.0.1:5173/');
    await expect(page.getByRole('heading', { name: 'خوادمك' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'الإعدادات' })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'sahabify-187' })).toBeVisible();
    await expectAccountChrome(page);
  });
});

test.describe('account id on a narrow screen', () => {
  test.use({ viewport: { width: 320, height: 700 } });

  test('the account id stays on one line inside the settings page', async ({ page }) => {
    await mockPortal(page, true);
    await page.goto('http://127.0.0.1:5173/?screen=settings');
    await expect(page.getByRole('heading', { name: 'الإعدادات' })).toBeVisible();
    const id = page.locator('[data-account-id]');
    await expect(id).toBeVisible();
    const metrics = await id.evaluate(node => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      const main = node.closest('main')!.getBoundingClientRect();
      return {
        whiteSpace: style.whiteSpace,
        height: rect.height,
        scrollWidth: node.scrollWidth,
        clientWidth: node.clientWidth,
        right: rect.right,
        left: rect.left,
        mainRight: main.right,
        mainLeft: main.left,
      };
    });
    expect(metrics.whiteSpace).toBe('nowrap');
    expect(metrics.height).toBeLessThan(44);
    expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 1);
    expect(metrics.left).toBeGreaterThanOrEqual(metrics.mainLeft - 1);
    expect(metrics.right).toBeLessThanOrEqual(metrics.mainRight + 1);
  });
});
