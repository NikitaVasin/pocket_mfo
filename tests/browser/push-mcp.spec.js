import { test, expect } from "@playwright/test";

test("MCP keys are scoped, one-time visible and revocable; push drafts are editable", async ({ page }, testInfo) => {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/mcp";
    });
    const mcp = page.locator('.mcp-page');
    await expect(mcp.getByRole("heading", { name: "MCP", exact: true })).toBeVisible();
    const permissionCatalog = await page.evaluate(() => app.pb.send("/api/mcp/admin/keys", { requestKey: null }));
    await mcp.getByRole("button", { name: "Полный доступ", exact: true }).click();
    for (const tool of permissionCatalog.tools) {
        await expect(mcp.getByLabel(tool.name + (tool.readOnly ? " · чтение" : " · изменение"), { exact: true })).toBeChecked();
    }
    await mcp.getByRole("button", { name: "Только чтение", exact: true }).click();
    for (const tool of permissionCatalog.tools) {
        await expect(mcp.getByLabel(tool.name + (tool.readOnly ? " · чтение" : " · изменение"), { exact: true })).toBeChecked({ checked: tool.readOnly });
    }
    for (const name of permissionCatalog.collections) await expect(mcp.getByLabel(name, { exact: true })).toBeChecked();
    await mcp.getByLabel("content_schema · чтение", { exact: true }).uncheck();
    await expect(mcp.getByRole("button", { name: "Только чтение", exact: true })).toHaveAttribute("aria-pressed", "false");
    await mcp.getByLabel("Название ключа", { exact: true }).fill("Browser content key");
    await mcp.getByLabel("content_list · чтение", { exact: true }).check();
    await mcp.getByLabel("demo_offers", { exact: true }).check();
    await mcp.getByRole("button", { name: "Создать ключ", exact: true }).click();
    const secret = mcp.getByRole("textbox", { name: "Созданный MCP ключ" });
    await expect(secret).toHaveValue(/^pmcp_[A-Za-z0-9_-]{43}$/);
    await expect(secret).toBeInViewport();
    const token = await secret.inputValue();
    const savedKey = await page.evaluate(async () => (await app.pb.send("/api/mcp/admin/keys", { requestKey: null })).items.find(key => key.name === "Browser content key"));
    expect(savedKey.tools.sort()).toEqual(permissionCatalog.tools.filter(tool => tool.readOnly && tool.name !== "content_schema").map(tool => tool.name).sort());
    expect(savedKey.collections.sort()).toEqual(permissionCatalog.collections.sort());
    const list = await page.evaluate(async token => {
        const response = await fetch("/api/mcp", { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", Accept: "application/json, text/event-stream" }, body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list", params: {} }) });
        return { status: response.status, text: await response.text() };
    }, token);
    // Browser Origin is intentionally not enabled; a stolen key cannot be used cross-origin.
    expect(list.status).toBe(403);
    await mcp.getByRole("button", { name: "Ключ сохранён", exact: true }).click();
    await expect(secret).toHaveCount(0);
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await page.evaluate(() => document.querySelectorAll("*").forEach(el => { if (el.scrollTop) el.scrollTop = 0; }));
            expect(await mcp.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await page.screenshot({ path: `test-results/mcp-${testInfo.project.name}-${theme}-${width}.png`, fullPage: true });
        }
    }
    await mcp.getByRole("button", { name: "Отозвать", exact: true }).last().click();
    await expect(mcp).toContainText("Отозван");
    await page.evaluate(() => { location.hash = "#/push"; });
    const push = page.locator('.push-page');
    await expect(push.getByRole("heading", { name: "Пуши", exact: true })).toBeVisible();
    await push.getByRole("button", { name: "Аудитории", exact: true }).click();
    await push.getByRole("button", { name: "Новая аудитория", exact: true }).click();
    await push.getByLabel("Название аудитории", { exact: true }).fill(`Browser audience ${testInfo.project.name}`);
    await push.getByRole("button", { name: "Сохранить аудиторию", exact: true }).click();
    await expect(push.locator(".push-notice")).toContainText("Аудитория сохранена");
    await push.getByRole("button", { name: "Обновить подсчёт", exact: true }).click();
    await expect(push.locator(".push-coverage")).toContainText("Доступных устройств: 0");
    await push.getByRole("dialog").getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(push.getByRole("dialog")).toHaveCount(0);
    await push.getByRole("button", { name: "Кампании", exact: true }).click();
    await push.getByRole("button", { name: "Новая кампания", exact: true }).click();
    await push.getByLabel("Название кампании", { exact: true }).fill("Browser draft");
    await push.getByLabel("Заголовок", { exact: true }).fill("Новые предложения");
    await push.getByLabel("Текст уведомления", { exact: true }).fill("Выберите подходящее предложение в приложении.");
    await push.getByRole("group", { name: "Получатели", exact: true }).getByLabel(`Browser audience ${testInfo.project.name}`).check();
    await push.getByRole("button", { name: "Сохранить кампанию", exact: true }).click();
    await expect(push.locator(".push-notice")).toContainText("Отправка ещё не запущена");
    await push.getByRole("button", { name: "Проверить аудиторию", exact: true }).click();
    await expect(push).toContainText("0 пользователей · 0 устройств");
    const state = await page.evaluate(() => app.pb.send("/api/push/admin/state"));
    expect(state.runs).toHaveLength(0);
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await page.evaluate(() => document.querySelectorAll("*").forEach(el => { if (el.scrollTop) el.scrollTop = 0; }));
            expect(await push.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await page.screenshot({ path: `test-results/push-${testInfo.project.name}-${theme}-${width}.png`, fullPage: true });
        }
    }
    await expect(push.getByRole("textbox", { name: "Заголовок", exact: true })).toHaveValue("Новые предложения");
    expect(errors).toEqual([]);
});

async function openPlugin(page, route) {
    await page.goto('/_/');
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async route => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        await app.store.loadCollections();
        location.hash = route;
    }, route);
}

async function checkBottom(page, root, last, screenshot) {
    await root.evaluate(el => {
        const scroll = el.querySelector('.modal-content') || el.closest('.page');
        scroll.scrollTop = scroll.scrollHeight;
    });
    const gap = await last.evaluate(el => (el.closest('.modal-content') || el.closest('.page')).getBoundingClientRect().bottom - el.getBoundingClientRect().bottom);
    expect(gap).toBeGreaterThanOrEqual(await root.locator('.modal-content').count() ? 20 : 48);
    expect(await root.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    await page.screenshot({ path: screenshot });
}

test('plugin pages keep bottom breathing room and explain empty states', async ({ page }, testInfo) => {
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await expect(push.getByRole('heading', { name: 'Пуши', exact: true })).toBeVisible();
    await push.getByRole('button', { name: 'Новая кампания', exact: true }).click();
    const launch = push.getByRole('button', { name: 'Запустить рассылку', exact: true });
    await expect(launch).toBeDisabled();
    await expect(push.locator('.push-readiness')).toContainText('Сначала сохраните кампанию');
    await push.getByText('Тестовая отправка', { exact: true }).click();
    await expect(push.locator('.push-test')).toContainText('Нет устройств с разрешёнными уведомлениями');
    await expect(push.getByRole('button', { name: 'Отправить тест', exact: true })).toBeDisabled();
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await checkBottom(page, push, push.locator('.push-test'), `test-results/push-bottom-${testInfo.project.name}-${theme}-${width}.png`);
        }
    }
    await page.evaluate(() => location.hash = '#/mcp');
    const mcp = page.locator('.mcp-page');
    await expect(mcp.getByRole('heading', { name: 'MCP', exact: true })).toBeVisible();
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await checkBottom(page, mcp, mcp.locator(':scope > section').last(), `test-results/mcp-bottom-${testInfo.project.name}-${theme}-${width}.png`);
        }
    }
});

test('push partner selector searches by name and preserves the selected ID', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await openPlugin(page, '#/push');
    const fixture = await page.evaluate(async () => {
        const demo = await app.pb.collection('partner_links').getOne('demopartner0001');
        const a = await app.pb.collection('partner_links').create({ name: 'Селектор: выгодное предложение', provider: 'demo', active: true, link: demo.link });
        const b = await app.pb.collection('partner_links').create({ name: 'Селектор: отключённое', provider: 'demo', active: false, link: demo.link });
        const audience = await app.pb.send('/api/push/admin/audience_save', { method: 'POST', body: { name: 'Selector audience', version: 0, authCollection: 'users' } });
        return { id: a.id, inactive: b.id, audience: audience.id };
    });
    await page.reload();
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Новая кампания', exact: true }).click();
    await push.getByLabel('При нажатии', { exact: true }).selectOption('partner');
    const selector = push.locator('.push-partner');
    await expect(push.getByLabel('ID партнёрской ссылки', { exact: true })).toHaveCount(0);
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await selector.getByLabel('Партнёрская ссылка', { exact: true }).click();
        const search = selector.getByPlaceholder('Search...');
        await search.pressSequentially('выгодное', { delay: 25 });
        await expect(search).toBeFocused();
        await expect(selector.locator('.select-option:visible')).toHaveCount(1);
        await page.screenshot({ path: `test-results/push-select-${testInfo.project.name}-${theme}.png` });
        await selector.locator('.select-option:visible').click();
        await expect(selector.locator('.selected-container')).toContainText('Селектор: выгодное предложение');
    }
    await push.getByLabel('Название кампании', { exact: true }).fill('UI link selection');
    await push.getByLabel('Заголовок', { exact: true }).fill('Предложение');
    await push.getByLabel('Текст уведомления', { exact: true }).fill('Посмотрите условия');
    await push.getByRole('group', { name: 'Получатели', exact: true }).getByLabel('Selector audience', { exact: true }).check();
    await push.getByRole('button', { name: 'Сохранить кампанию', exact: true }).click();
    await expect(push.locator('.push-notice')).toContainText('Кампания сохранена');
    const saved = await page.evaluate(async () => (await app.pb.send('/api/push/admin/state')).campaigns.find(c => c.name === 'UI link selection'));
    expect(saved.message.target).toBe(fixture.id);
    await page.reload();
    await push.locator('.push-campaign-card').filter({ has: page.getByRole('heading', { name: 'UI link selection', exact: true }) }).getByRole('button', { name: 'Редактировать', exact: true }).click();
    await expect(selector.locator('.selected-container')).toContainText('Селектор: выгодное предложение');
    expect(errors).toEqual([]);
});

test('partner selector has retry and empty states without raw ID entry', async ({ page }) => {
    await openPlugin(page, '#/push');
    await page.route('**/api/collections/partner_links/records?**', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"message":"offline"}' }));
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Новая кампания', exact: true }).click();
    await push.getByLabel('При нажатии', { exact: true }).selectOption('partner');
    await expect(push.locator('.push-partner')).toContainText('Не удалось загрузить партнёрские ссылки');
    await page.unroute('**/api/collections/partner_links/records?**');
    await page.route('**/api/collections/partner_links/records?**', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ page: 1, perPage: 500, totalItems: 0, totalPages: 0, items: [] }) }));
    await push.getByRole('button', { name: 'Повторить загрузку ссылок', exact: true }).click();
    await expect(push.locator('.push-partner')).toContainText('Нет активных партнёрских ссылок');
    await expect(push.getByLabel('Партнёрская ссылка', { exact: true })).toBeDisabled();
});

test('audience coverage updates automatically and discards stale responses', async ({ page }, testInfo) => {
    await openPlugin(page, '#/push');
    let releaseFirst;
    const held = new Promise(resolve => { releaseFirst = resolve; });
    let firstStarted;
    const started = new Promise(resolve => { firstStarted = resolve; });
    let firstCompleted;
    const completed = new Promise(resolve => { firstCompleted = resolve; });
    let requests = 0;
    await page.route('**/api/push/admin/audience_preview', async route => {
        requests++;
        const value = route.request().postDataJSON().condition.children[0].value;
        if (value === 'android') {
            firstStarted();
            await held;
            await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ users: 3, devices: 5, totalUsers: 4 }) });
            firstCompleted();
        } else {
            await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ users: 1, devices: 2, totalUsers: 4 }) });
        }
    });
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Аудитории', exact: true }).click();
    await push.getByRole('button', { name: 'Новая аудитория', exact: true }).click();
    await started;
    const value = push.getByLabel('Значение', { exact: true });
    await value.fill('');
    await value.pressSequentially('ios', { delay: 25 });
    await expect(value).toBeFocused();
    await expect(push.locator('.push-coverage')).toContainText('1 из 4 пользователей · 25%');
    await expect(push.locator('.push-coverage')).toContainText('Доступных устройств: 2');
    releaseFirst();
    await completed;
    // An explicit second query waits behind delivery of the earlier response.
    await push.getByRole('button', { name: 'Обновить подсчёт', exact: true }).click();
    await expect(push.locator('.push-coverage')).toContainText('1 из 4 пользователей · 25%');
    expect(requests).toBe(3);
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.locator('.push-shell').evaluate(el => { el.scrollTop = 0; });
        await page.screenshot({ path: `test-results/push-coverage-${testInfo.project.name}-${theme}.png` });
    }
});

test("Push settings pinned by server code are read-only", async ({ page }, testInfo) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/push";
    });
    const push = page.locator(".push-page");
    await push.getByRole("button", { name: "Настройки", exact: true }).click();
    const locked = testInfo.project.name === "schemalock";
    for (const label of ["Application ID", "Скорость отправки в секунду", "OAuth token"]) {
        const input = push.getByLabel(label, { exact: true });
        if (locked) await expect(input).toBeDisabled();
        else await expect(input).toBeEnabled();
    }
    if (locked) {
        await expect(push.getByLabel("Application ID", { exact: true })).toHaveValue("6361870");
        await expect(push.getByLabel("OAuth ClientID", { exact: true })).toHaveValue("8e1f79cf905d4a70b30507ea80e0730f");
        await expect(push.getByLabel("OAuth ClientID", { exact: true })).toBeDisabled();
        await expect(push.getByRole("link", { name: "Получить OAuth-токен ↗" })).toHaveAttribute("href", "https://oauth.yandex.ru/authorize?response_type=token&client_id=8e1f79cf905d4a70b30507ea80e0730f");
        await expect(push).toContainText("Настройки закреплены в Go-коде");
        await expect(push.getByRole("button", { name: "Сохранить настройки", exact: true })).toHaveCount(0);
        const status = await page.evaluate(async () => {
            try {
                await app.pb.send("/api/push/admin/config", { method: "PUT", body: { applicationId: 1, sendRate: 1000 }, requestKey: null });
                return 200;
            } catch (error) { return error.status; }
        });
        expect(status).toBe(403);
    } else {
        await expect(push.getByRole("button", { name: "Сохранить настройки", exact: true })).toBeEnabled();
    }
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: `test-results/push-settings-${testInfo.project.name}-${theme}.png`, fullPage: true });
    }
});

test('campaign can target everyone without an audience and preserves recipient mode', async ({ page }, testInfo) => {
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Новая кампания', exact: true }).click();
    await push.getByLabel('Название кампании', { exact: true }).fill('Everyone broadcast');
    await push.getByLabel('Заголовок', { exact: true }).fill('Новости для всех');
    await push.getByLabel('Текст уведомления', { exact: true }).fill('Сообщение всем пользователям приложения.');
    await push.getByRole('radio', { name: 'Все пользователи', exact: true }).check();
    await expect(push.getByRole('group', { name: 'Получатели', exact: true })).toHaveCount(0);
    await push.getByRole('button', { name: 'Сохранить кампанию', exact: true }).click();
    await expect(push.locator('.push-notice')).toContainText('Отправка ещё не запущена');
    const saved = await page.evaluate(async () => (await app.pb.send('/api/push/admin/state', { requestKey: null })).campaigns.find(c => c.name === 'Everyone broadcast'));
    expect(saved.allUsers).toBe(true);
    expect(saved.audienceIds).toEqual([]);
    await page.reload();
    await push.locator('.push-campaign-card').filter({ has: page.getByRole('heading', { name: 'Everyone broadcast', exact: true }) }).getByRole('button', { name: 'Редактировать', exact: true }).click();
    await expect(push.getByRole('radio', { name: 'Все пользователи', exact: true })).toBeChecked();
    await push.getByRole('button', { name: 'Проверить аудиторию', exact: true }).click();
    await expect(push.locator('.push-preview')).toContainText('0 пользователей · 0 устройств');
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await push.getByRole('group', { name: 'Кому отправить', exact: true }).scrollIntoViewIfNeeded();
        await page.screenshot({ path: `test-results/push-everyone-${testInfo.project.name}-${theme}.png` });
    }
    await push.getByRole('radio', { name: 'Выбранные аудитории', exact: true }).check();
    await expect(push.getByRole('group', { name: 'Получатели', exact: true })).toBeVisible();
    await expect(push.getByRole('button', { name: 'Проверить аудиторию', exact: true })).toBeDisabled();
});

test('switching audience condition types initializes valid field values', async ({ page }) => {
    await openPlugin(page, '#/push');
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Аудитории', exact: true }).click();
    await push.getByRole('button', { name: 'Новая аудитория', exact: true }).click();
    await push.getByLabel('Название аудитории', { exact: true }).fill('Switched condition');
    await push.getByLabel('Тип условия', { exact: true }).first().selectOption('field');
    await expect(push.getByLabel('Источник', { exact: true })).toHaveValue('device');
    await expect(push.getByLabel('Поле', { exact: true })).toHaveValue('platform');
    await expect(push.getByLabel('Сравнение', { exact: true })).toHaveValue('eq');
    await expect(push.locator('.push-coverage')).toContainText('Доступных устройств: 0');
    await push.getByLabel('Источник', { exact: true }).selectOption('user');
    await expect(push.getByLabel('Поле', { exact: true })).toHaveValue('id');
    await push.getByLabel('Тип условия', { exact: true }).first().selectOption('conversion');
    await expect(push.getByLabel('Источник', { exact: true })).toHaveValue('conversion');
    await expect(push.getByLabel('Поле', { exact: true })).toHaveValue('status');
    await push.getByRole('button', { name: 'Обновить подсчёт', exact: true }).click();
    await expect(push.locator('.push-coverage')).toContainText('Доступных устройств: 0');
    await push.getByLabel('Тип условия', { exact: true }).first().selectOption('field');
    await push.getByRole('button', { name: 'Сохранить аудиторию', exact: true }).click();
    await expect(push.locator('.push-notice')).toContainText('Аудитория сохранена');
    expect(errors).toEqual([]);
});

test('push history shows colored statuses and recovers legacy failure diagnostics', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const statuses = ['failed', 'sent', 'unknown', 'sending', 'scheduled', 'empty', 'cancelled'];
    const failure = 'AppMetrica отклонила отправку: Android sender is not configured <img src=x onerror=alert(1)>';
    const runs = statuses.map((status, i) => ({ id: `history${i}`, name: `Проверка ${status}`, status, recipients: 1, created: '2026-09-23 09:00:00.000Z', error: '' }));
    const report = { ...runs[0], opens: 0, appmetricaGroupId: '1901277', jobCounts: { failed: 1 }, jobs: [{ id: 'batch1', status: 'failed', recipients: 1, clientTransferId: '123456789123456789', transferId: '81', error: '', deferrals: 0, updated: '2026-09-23 09:00:30.000Z' }] };
    await page.route('**/api/push/admin/state', async route => {
        const response = await route.fetch();
        await route.fulfill({ response, json: { ...await response.json(), runs } });
    });
    await page.route('**/api/push/admin/report', route => route.fulfill({ json: report }));
    await page.route('**/api/push/admin/refresh_report', route => {
        report.error = failure; report.jobs[0].error = failure; runs[0].error = failure;
        return route.fulfill({ json: report });
    });
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'История', exact: true }).click();
    for (const [tone, label] of [['danger', 'Ошибка'], ['success', 'Отправлена'], ['warning', 'Статус неизвестен'], ['info', 'Отправляется'], ['neutral', 'Отменена']]) {
        await expect(push.locator(`.push-status-${tone}`).filter({ hasText: label }).first()).toBeVisible();
    }
    await push.getByRole('button', { name: 'Результаты', exact: true }).first().click();
    await expect(push.getByRole('dialog', { name: 'Результаты запуска', exact: true })).toBeVisible();
    const result = push.getByRole('region', { name: 'Результаты запуска', exact: true });
    await expect(result).toContainText('Причина не сохранена');
    await result.getByRole('button', { name: 'Запросить причину в AppMetrica', exact: true }).click();
    await expect(result.locator('.push-diagnostic')).toContainText(failure);
    await expect(result).toContainText('123456789123456789');
    await expect(result.locator('img')).toHaveCount(0);
    await expect(result).not.toContainText('Следующая попытка');
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await result.scrollIntoViewIfNeeded();
            expect(await push.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            const colors = await push.evaluate(el => ['danger', 'success', 'warning', 'info'].map(tone => getComputedStyle(el.querySelector(`.push-status-${tone}`), '::before').backgroundColor));
            expect(new Set(colors).size).toBe(4);
            await page.screenshot({ path: `test-results/push-history-${testInfo.project.name}-${theme}-${width}.png`, fullPage: true });
        }
    }
    expect(errors).toEqual([]);
});

test('campaign cards use native side panels and protect unsaved edits', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await expect(push.getByRole('button', { name: 'Устройства', exact: true })).toHaveCount(0);
    await expect(push.getByLabel('Название кампании', { exact: true })).toHaveCount(0);
    const create = push.getByRole('button', { name: 'Новая кампания', exact: true });
    await create.click();
    const dialog = push.getByRole('dialog');
    await expect(dialog).toHaveAttribute('data-modal-state', 'open');
    await expect(push.locator(':scope > div').first()).toHaveJSProperty('inert', true);
    await dialog.getByLabel('Название кампании', { exact: true }).fill(`Panel ${testInfo.project.name}`);
    await dialog.getByLabel('Заголовок', { exact: true }).fill('Тест панели');
    await dialog.getByLabel('Текст уведомления', { exact: true }).fill('Сохранённый текст');
    await dialog.getByRole('radio', { name: 'Все пользователи', exact: true }).check();
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await page.getByRole('button', { name: 'Продолжить редактирование', exact: true }).click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole('button', { name: 'Сохранить кампанию', exact: true }).click();
    await expect(dialog.locator('.push-notice')).toContainText('Кампания сохранена');
    await expect(dialog).toHaveAccessibleName('Редактирование кампании');
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    // Saving updates cards while the panel is open; the background remains usable on close.
    const card = push.locator('.push-campaign-card').filter({ hasText: `Panel ${testInfo.project.name}` });
    await expect(card).toContainText('Тест панели');
    await expect(card).toContainText('Черновик');
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: `test-results/push-cards-${testInfo.project.name}-${theme}.png` });
    }
    await card.getByRole('button', { name: 'Редактировать', exact: true }).click();
    await dialog.getByLabel('Заголовок', { exact: true }).fill('Не сохранять');
    await dialog.getByLabel('Заголовок', { exact: true }).press('Escape');
    await page.getByRole('button', { name: 'Не сохранять', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(card).toContainText('Тест панели');
    await expect(card).not.toContainText('Не сохранять');
    await card.getByRole('button', { name: 'Редактировать', exact: true }).click();
    await expect(dialog.getByLabel('Заголовок', { exact: true })).toHaveValue('Тест панели');
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(errors).toEqual([]);
});

test('push devices appear in native system collections with permission and read-only records', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await openPlugin(page, '#/push');
    const collection = await page.evaluate(async () => {
        const user = new app.pb.constructor(app.pb.baseURL);
        await user.collection('users').authWithPassword('default@variants.test', 'demo-variants-123');
        const device = await user.send('/api/push/devices', { method: 'POST', body: {
            deviceId: '987654321', secret: 'a'.repeat(43), platform: 'android',
            enabled: false, notificationPermission: 'denied', language: 'ru',
        } });
        const collection = app.store.collections.find(c => c.name === 'push_devices');
        location.hash = "#/collections?collection=push_devices";
        return { id: collection.id, deviceId: device.id, system: collection.system, fields: collection.fields.map(f => f.name) };
    });
    expect(collection.system).toBe(true);
    expect(collection.fields).toContain('notificationPermission');
    await expect(page.locator('body')).toHaveClass(/push-devices-active/);
    await expect(page.locator('.collections-sidebar').getByText('push_devices', { exact: true })).toBeVisible();
    await expect(page.locator('.new-record-btn:visible')).toHaveCount(0);
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.evaluate(data => app.modals.openRecordUpsert(app.store.activeCollection, data.deviceId), collection);
        const preview = page.locator('.record-preview-modal');
        await expect(preview).toBeVisible();
        await expect(preview).toContainText('notificationPermission');
        await expect(preview).toContainText('denied');
        await expect(page.locator('.record-upsert-modal')).toHaveCount(0);
        await expect(preview.getByRole('button', { name: /^(Save|Delete|Duplicate)$/ })).toHaveCount(0);
        await page.screenshot({ path: `test-results/push-device-${testInfo.project.name}-${theme}.png` });
        await page.keyboard.press('Escape');
        await expect(preview).toHaveCount(0);
    }
    expect(errors).toEqual([]);
});

test('audience cards open a side editor, preserve saved conditions and discard unsaved changes', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await push.getByRole('button', { name: 'Аудитории', exact: true }).click();
    await expect(push.getByLabel('Название аудитории', { exact: true })).toHaveCount(0);
    await push.getByRole('button', { name: 'Новая аудитория', exact: true }).click();
    const dialog = push.getByRole('dialog');
    await expect(dialog).toHaveAccessibleName('Новая аудитория');
    await dialog.getByLabel('Название аудитории', { exact: true }).fill(`Audience panel ${testInfo.project.name}`);
    await dialog.getByLabel('Значение', { exact: true }).fill('android');
    await expect(dialog.locator('.push-coverage')).toContainText('Доступных устройств: 0');
    await dialog.getByRole('button', { name: 'Сохранить аудиторию', exact: true }).click();
    await expect(dialog.locator('.push-notice')).toContainText('Аудитория сохранена');
    await expect(dialog).toHaveAccessibleName('Редактирование аудитории');
    await expect(dialog.locator('.push-coverage')).toContainText('Доступных устройств: 0');
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ['light', 'dark']) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            expect(await dialog.locator('.modal-content').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await expect(dialog.getByRole('button', { name: 'Сохранить аудиторию', exact: true })).toBeInViewport();
            await dialog.locator('.modal-content').evaluate(el => { el.scrollTop = 0; });
            await page.screenshot({ path: `test-results/push-audience-editor-${testInfo.project.name}-${theme}-${width}.png` });
        }
    }
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const card = push.locator('.push-audience-card').filter({ hasText: `Audience panel ${testInfo.project.name}` });
    await expect(card).toContainText('Условий отбора: 1');
    await expect(card).toContainText('Коллекция: users');
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: `test-results/push-audiences-${testInfo.project.name}-${theme}.png` });
    }
    await card.getByRole('button', { name: 'Редактировать', exact: true }).click();
    await expect(dialog.getByLabel('Значение', { exact: true })).toHaveValue('android');
    await dialog.getByRole('button', { name: 'Все пользователи', exact: true }).click();
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await expect(page.getByText('Закрыть без сохранения изменений аудитории?', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Продолжить редактирование', exact: true }).click();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel('Значение', { exact: true })).toHaveCount(0);
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await page.getByRole('button', { name: 'Не сохранять', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await card.getByRole('button', { name: 'Редактировать', exact: true }).click();
    await expect(dialog.getByLabel('Значение', { exact: true })).toHaveValue('android');
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(errors).toEqual([]);
});
