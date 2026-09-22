// Run after `fvm flutter build web --no-web-resources-cdn` in the example.
// Uses only a disposable PocketBase database; no AppMetrica requests or keys.
import { chromium, expect } from '@playwright/test';
import { mkdtempSync, rmSync, readFileSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve, extname } from 'node:path';
import { execFileSync, spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { createServer as createTcpServer } from 'node:net';

const root = resolve('examples/partner_links_app/build/web');
if (!existsSync(join(root, 'index.html'))) throw new Error('Build the Flutter web example first.');
const dir = mkdtempSync(join(tmpdir(), 'pocket-mfo-flutter-'));
let backend, browser, web, page;
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
try {
    const binary = join(dir, 'pocketbase');
    execFileSync('go', ['build', '-o', binary, './example'], { env: { ...process.env, GOTOOLCHAIN: 'auto' }, stdio: 'inherit' });
    const probe = createTcpServer();
    await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve));
    const port = probe.address().port;
    await new Promise(resolve => probe.close(resolve));
    const apiURL = `http://127.0.0.1:${port}`;
    backend = spawn(binary, ['serve', `--http=127.0.0.1:${port}`, '--dir', join(dir, 'pb_data')], { env: process.env, stdio: 'ignore' });
    let ready = false;
    for (let i = 0; i < 100; i++) {
        if (await fetch(apiURL + '/api/health').then(r => r.ok).catch(() => false)) { ready = true; break; }
        await wait(100);
    }
    if (!ready) throw new Error('Disposable PocketBase did not start.');
    const mime = { '.html': 'text/html', '.js': 'text/javascript', '.json': 'application/json', '.wasm': 'application/wasm', '.png': 'image/png' };
    web = createServer((req, res) => {
        const file = resolve(root, '.' + new URL(req.url, 'http://localhost').pathname);
        if (!file.startsWith(root + '/') && file !== root) { res.writeHead(403); res.end(); return; }
        const target = file === root ? join(root, 'index.html') : file;
        try { res.setHeader('Content-Type', mime[extname(target)] || 'application/octet-stream'); res.end(readFileSync(target)); }
        catch { res.writeHead(404); res.end(); }
    });
    await new Promise(resolve => web.listen(0, '127.0.0.1', resolve));
    browser = await chromium.launch({ headless: true });
    page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    // Redirect the example's default backend address to this disposable instance.
    await page.route('http://127.0.0.1:8090/**', async route => {
        const response = await route.fetch({ url: route.request().url().replace('http://127.0.0.1:8090', apiURL) });
        await route.fulfill({ response });
    });
    await page.goto(`http://127.0.0.1:${web.address().port}`);
    await page.locator('flt-semantics-placeholder').waitFor({ state: 'attached', timeout: 30000 });
    await page.locator('flt-semantics-placeholder').dispatchEvent('click');
    await expect(page.getByText(/Пользователь: .* · гость/)).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole('button', { name: 'Демонстрационный оффер demo', exact: true })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('Мои заказы', { exact: false })).toBeVisible();
    await page.getByRole('button', { name: 'Отправить тестовое событие', exact: true }).click();
    await expect(page.locator('flt-semantics').getByText('Событие отправлено', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Демонстрационный оффер demo', exact: true }).click();
    await expect(page.getByText('Сервер ещё не настроен:', { exact: false })).toBeVisible();
    mkdirSync('test-results', { recursive: true });
    await page.screenshot({ path: 'test-results/flutter-example-desktop.png' });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: 'test-results/flutter-example-mobile.png' });
    await page.setViewportSize({ width: 1100, height: 900 });
    await page.mouse.wheel(0, -2000);
    await expect(page.getByText(/Пользователь: .* · гость/)).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Email', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Войти', exact: true })).toHaveCount(0);
    expect(errors).toEqual([]);
    console.log('Flutter web smoke passed: SDK guest initialization without login form, enriched event, offers, own orders and resolve error.');
} catch (error) {
    if (page) {
        console.error(await page.locator('body').ariaSnapshot());
        mkdirSync('test-results', { recursive: true });
        await page.screenshot({ path: 'test-results/flutter-smoke-failure.png' });
    }
    throw error;
} finally {
    await browser?.close();
    if (web) await new Promise(resolve => web.close(resolve));
    if (backend && backend.exitCode === null) {
        const stopped = new Promise(resolve => backend.once('exit', resolve));
        backend.kill('SIGTERM'); await stopped;
    }
    rmSync(dir, { recursive: true, force: true });
}
