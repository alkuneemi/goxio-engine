import { expect, test, type Page } from '@playwright/test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));

const CYAN = /#(?:00e5c3|00d4c8|00d8ff|5ad8cf|22d3ee|38bdf8|06b6d4|0891b2|14b8a6|2dd4bf|5eead4|67e8f9|a5f3fc|041716|0d9488)/i;

function walk(dir: string, out: string[]) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name === 'react.svg') continue;
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.(css|tsx|ts|html|svg)$/.test(name)) out.push(full);
  }
}

test('theme tokens do not keep a turquoise or cyan brand hex', () => {
  const root = path.resolve(here, '../..');
  const files: string[] = [];
  walk(path.join(root, 'webui/src'), files);
  walk(path.join(root, 'webui/public'), files);
  files.push(path.join(root, 'webui/index.html'), path.join(root, 'icon.svg'));
  const hits: string[] = [];
  for (const file of files) {
    const text = readFileSync(file, 'utf8');
    const match = text.match(CYAN);
    if (match) hits.push(`${path.relative(root, file)}: ${match[0]}`);
  }
  expect(hits).toEqual([]);
});

async function mockPortal(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('goxio-lang', 'ar');
  });
  await page.route(url => new URL(url).pathname.startsWith('/api/'), async route => {
    const req = route.request();
    const reqPath = new URL(req.url()).pathname;
    if (reqPath === '/api/portal/oidc') {
      await route.fulfill({ json: { data: { configured: false } } });
      return;
    }
    if (reqPath === '/api/portal/me') {
      await route.fulfill({
        json: {
          data: {
            csrf: 'csrf-test',
            id: 'acct_theme',
            login: 'sahabify',
            email: 'sahabify@gmail.com',
            company_name: 'سحاب الحلول الذكية',
            created_at: '2026-11-05T10:57:00Z',
            server_count: 1,
          },
        },
      });
      return;
    }
    if (reqPath === '/api/portal/servers') {
      await route.fulfill({
        json: {
          data: [{
            id: 'srv_one',
            name: 'sahabify-187',
            address: '187.127.114.87',
            status: 'ready',
            ssh_fingerprint: 'SHA256:examplefingerprint',
            containers: 22,
            databases: 146,
            volumes: 64,
            images: 201,
            disk_used_bytes: 353 * 1024 ** 3,
            disk_size_bytes: 386 * 1024 ** 3,
            collected_at: '2026-11-09T11:33:00Z',
          }],
        },
      });
      return;
    }
    if (reqPath === '/api/graph') {
      await route.fulfill({
        json: {
          data: {
            nodes: [
              { id: 'disk-a', type: 'disk', name: '/dev/sda1', health: 'unknown', metadata: {} },
              { id: 'vol-a', type: 'volume', name: 'data', health: 'healthy', metadata: {} },
            ],
            edges: [{ id: 'e1', source: 'disk-a', target: 'vol-a', type: 'contains' }],
          },
        },
      });
      return;
    }
    if (reqPath === '/api/space') {
      await route.fulfill({ json: { data: { host: '187.127.114.87', status: 'ready', host_writes: false } } });
      return;
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } });
  });
}

test.describe('day and night', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('night is the default and day mode keeps its Arabic name', async ({ page }) => {
    await mockPortal(page);
    await page.goto('http://127.0.0.1:5173/');

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(page.getByRole('heading', { name: 'خوادمك' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'وضع نهاري' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'وضع ليلي' })).toHaveCount(0);

    const word = page.locator('b', { hasText: /^O$/ }).first();
    await expect(word).toHaveCSS('color', 'rgb(160, 160, 181)');

    await page.locator('[data-account-menu="header"]').click();
    await expect(page.getByRole('menuitem', { name: 'خروج' })).toBeVisible();
    await page.getByRole('menuitem', { name: 'الإعدادات' }).click();
    await expect(page.getByRole('radio', { name: 'وضع نهاري' })).toBeVisible();
    await expect(page.getByRole('radio', { name: 'وضع ليلي' })).toBeVisible();
    await expect(page.getByRole('radio', { name: 'حسب الجهاز' })).toBeVisible();
    await expect(page.getByRole('radio', { name: 'وضع ليلي' })).toHaveAttribute('aria-checked', 'true');

    await page.getByRole('radio', { name: 'وضع نهاري' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await expect(word).toHaveCSS('color', 'rgb(74, 74, 92)');
    const dayButton = page.getByRole('button', { name: 'فتح الخريطة بالتبويب' });
    await page.getByRole('button', { name: 'الخوادم' }).click();
    await expect(dayButton).toHaveCSS('background-color', 'rgb(28, 28, 28)');
    await expect(dayButton).toHaveCSS('color', 'rgb(255, 255, 255)');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await expect(page.getByRole('heading', { name: 'خوادمك' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'وضع ليلي' })).toHaveCount(0);

    await page.locator('[data-account-menu="header"]').click();
    await page.getByRole('menuitem', { name: 'الإعدادات' }).click();
    await page.getByRole('radio', { name: 'وضع ليلي' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.getByRole('button', { name: 'الخوادم' }).click();
    await expect(page.getByRole('button', { name: 'فتح الخريطة بالتبويب' })).toHaveCSS('background-color', 'rgb(236, 236, 236)');

    await page.getByRole('button', { name: 'فتح الخريطة بالعقد العصبية' }).click();
    const mapPill = page.getByRole('button', { name: 'Map', exact: true });
    await expect(mapPill).toHaveAttribute('aria-pressed', 'true');
    await expect(mapPill).toHaveCSS('background-color', 'rgb(160, 160, 181)');
    await expect(mapPill).not.toHaveCSS('background-color', 'rgb(0, 212, 200)');
    const node = page.locator('.react-flow__node').first();
    await expect(node).toBeVisible();
    const fills = await node.evaluate(el => [...el.querySelectorAll('div')].map(div => getComputedStyle(div).backgroundColor));
    expect(fills).toContain('rgb(45, 45, 45)');
    await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0);
    await page.locator('[data-account-menu="header"]').click();
    await expect(page.getByRole('menuitem', { name: 'خروج' })).toBeVisible();
  });
});
