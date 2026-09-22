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
    await push.getByRole("button", { name: "Кампании", exact: true }).click();
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
        const scroll = el.closest('.page');
        scroll.scrollTop = scroll.scrollHeight;
    });
    const gap = await last.evaluate(el => el.closest('.page').getBoundingClientRect().bottom - el.getBoundingClientRect().bottom);
    expect(gap).toBeGreaterThanOrEqual(48);
    expect(await root.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    await page.screenshot({ path: screenshot });
}

test('plugin pages keep bottom breathing room and explain empty states', async ({ page }, testInfo) => {
    await openPlugin(page, '#/push');
    const push = page.locator('.push-page');
    await expect(push.getByRole('heading', { name: 'Пуши', exact: true })).toBeVisible();
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
    await push.getByRole('button', { name: 'UI link selection', exact: true }).click();
    await expect(selector.locator('.selected-container')).toContainText('Селектор: выгодное предложение');
    expect(errors).toEqual([]);
});

test('partner selector has retry and empty states without raw ID entry', async ({ page }) => {
    await openPlugin(page, '#/push');
    await page.route('**/api/collections/partner_links/records?**', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"message":"offline"}' }));
    const push = page.locator('.push-page');
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
    await push.getByRole('button', { name: 'Everyone broadcast', exact: true }).click();
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
