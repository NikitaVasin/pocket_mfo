import { test, expect } from '@playwright/test';

test('AppMetrica asks only for OAuth and application, with code-owned values shown as status', async ({ page }, testInfo) => {
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto('/_/'); await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => { await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123'); location.hash = '#/appmetrica'; });
    await expect(page.getByRole('heading', { name: 'AppMetrica', exact: true })).toBeVisible();
    await expect(page.getByLabel('Application ID', { exact: true })).toHaveValue(testInfo.project.name === 'schemalock' ? '6361870' : '1234');
    for (const name of ['SDK API key', 'Post API key', 'OAuth Client ID']) await expect(page.getByLabel(name, { exact: true })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Полученные параметры' })).toContainText('Post API key');
    await expect(page.locator('body')).not.toContainText('fake-browser-key');
    if (testInfo.project.name === 'schemalock') {
        await expect(page.getByLabel('Application ID', { exact: true })).toHaveAttribute('readonly', '');
        await expect(page.getByLabel('OAuth-токен', { exact: true })).toHaveCount(0);
    } else {
        await expect(page.getByLabel('OAuth-токен', { exact: true })).toHaveAttribute('type', 'password');
        // UI-only fixture: Go tests verify discovery, atomic import and outbound GETs.
        let requests = 0;
        await page.route('**/api/appmetrica/admin/connect', async route => {
            const body = route.request().postDataJSON(); expect(body.oauthToken).toBe('ui-test-token');
            requests++;
            if (requests === 1) { await route.fulfill({ json: { applications: [{ id: 1234, name: 'First app' }, { id: 5678, name: 'Second app' }] } }); return; }
            const response = await page.request.get('/api/appmetrica/admin/config', { headers: { Authorization: await page.evaluate(() => app.pb.authStore.token) } });
            const settings = await response.json(); settings.config.sdkApiKey = 'public-sdk-key'; settings.hasPostApiKey = true; settings.hasOAuthToken = true;
            await route.fulfill({ json: { settings } });
        });
        await page.getByLabel('Application ID', { exact: true }).fill('');
        await page.getByLabel('OAuth-токен', { exact: true }).fill('ui-test-token');
        await expect(page.getByRole('button', { name: 'Проверить доступ', exact: true })).toBeDisabled();
        await page.getByRole('button', { name: 'Подключить AppMetrica', exact: true }).click();
        await expect(page.getByLabel('Выберите приложение', { exact: true })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Подключить AppMetrica', exact: true })).toBeDisabled();
        await page.getByLabel('Выберите приложение', { exact: true }).click();
        await page.locator('.select-option').filter({ hasText: 'First app' }).click();
        await page.getByRole('button', { name: 'Подключить AppMetrica', exact: true }).click();
        await expect(page.getByText('Подключение сохранено. Ключи получены из AppMetrica.', { exact: true })).toBeVisible();
        await expect(page.getByLabel('OAuth-токен', { exact: true })).toHaveValue('');
        await expect(page.getByRole('region', { name: 'Полученные параметры' })).toContainText('public-sdk-key');
    }
    await page.route('**/api/appmetrica/admin/check', route => route.fulfill({ json: {
        events: { configured: true, verified: false, message: 'Приём событий не проверен.' },
        push: { configured: true, verified: true, message: 'Доставка не проверена.' },
        reports: { configured: true, verified: false, message: 'Нет доступа к отчётам.' },
    } }));
    await page.getByRole('button', { name: 'Проверить доступ', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Готовность AppMetrica' })).toContainText('✓ Проверено');
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: testInfo.outputPath(`appmetrica-${theme}.png`), fullPage: true });
    }
    expect(errors).toEqual([]);
});
