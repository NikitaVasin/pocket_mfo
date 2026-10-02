import { test, expect } from "@playwright/test";

test("currency rates stay hidden in navigation but available by direct URL", async ({ page }) => {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const schema = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        return app.pb.collections.getOne("currency_rates");
    });
    expect(schema.system).toBe(true);
    expect(schema.fields.map(field => field.name)).toEqual(expect.arrayContaining(["date", "currency", "nominal", "value", "rate"]));
    // The UI smoke check also works offline, when the source has no rows yet.
    for (const theme of ["light", "dark"]) {
        await page.evaluate(({ theme, id }) => {
            app.store.userColorScheme = theme;
            location.hash = "#/collections?collection=" + id;
        }, { theme, id: schema.id });
        await expect(page.locator('.collections-sidebar').getByText("currency_rates", { exact: true })).toBeHidden();
        await expect(page.locator('[data-pb="pageCollections"]')).toBeVisible();
        await page.screenshot({ path: `test-results/currencyrates-${theme}-${test.info().project.name}.png`, fullPage: true, animations: "disabled" });
    }
    expect(errors).toEqual([]);
});

test("all plugin collections are hidden in regular, system, pinned and search navigation", async ({ page }, testInfo) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const collections = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=demo_offers";
        return app.store.collections.map(c => ({ id: c.id, name: c.name }));
    });
    const owned = ["appmetrica_config", "currency_rates", "dynamic_link_settings", "mcp_keys", "partner_links", "conversations", "pl_config", "ps_configs", "pv_configs", "pv_sets", "pv_states", "pv_history", "push_config", "push_devices", "push_audiences", "push_campaigns", "push_runs", "push_jobs", "push_opens"];
    expect(collections.map(c => c.name)).toEqual(expect.arrayContaining(owned));
    const generated = collections.filter(c => c.name.startsWith("pv_choice_"));
    expect(generated.length).toBeGreaterThan(0);
    const hidden = [...owned, ...generated.map(c => c.name)];
    const sidebar = page.locator(".collections-sidebar");
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        const system = sidebar.locator(".nav-group-system-collections");
        if (!await system.evaluate(el => el.open)) await system.locator("summary").click();
        for (const name of hidden) await expect(sidebar.locator(`.nav-item[title="${name}"]`)).toBeHidden();
        await expect(sidebar.locator('.nav-item[title="_superusers"]')).toBeVisible();
        await expect(sidebar.locator('.nav-item[title="demo_offers"]')).toBeVisible();
        await sidebar.screenshot({ path: `test-results/plugin-collections-${testInfo.project.name}-${theme}.png` });
        await sidebar.getByPlaceholder("Search collections...").fill("push");
        await expect(sidebar.locator(".nav-item:visible")).toHaveCount(0);
        await expect(system).toBeHidden();
        await sidebar.getByPlaceholder("Search collections...").fill("");
    }
    await page.evaluate(ids => localStorage.setItem("pbPinnedCollections", JSON.stringify(ids)), collections.filter(c => hidden.includes(c.name) || c.name === "demo_offers").map(c => c.id));
    await page.reload();
    await expect(sidebar.locator('.nav-group-pinned-collections .nav-item[title="demo_offers"]')).toBeVisible();
    for (const name of hidden) await expect(sidebar.locator(`.nav-item[title="${name}"]`)).toBeHidden();
    expect(await page.evaluate(() => app.store.collections.map(c => c.name))).toEqual(expect.arrayContaining(hidden));
    // Older bookmarks and stored collection IDs must not trap header navigation.
    for (const key of ["currency_rates", collections.find(c => c.name === "push_devices").id, generated[0].id]) {
        await page.goto("/_/#/collections?collection=" + key);
        await expect(page.locator('[data-pb="pageCollections"]')).toBeVisible();
        await page.getByRole("link", { name: "Collections", exact: true }).click();
        await expect.poll(() => page.evaluate(hidden => hidden.includes(app.store.activeCollection?.name), hidden)).toBe(false);
    }
});

const rateRows = [
    { id: 'usd', currency: 'USD', name: 'Доллар США', nominal: 1, value: 83.2454, rate: 83.2454, numCode: '840' },
    { id: 'eur', currency: 'EUR', name: 'Евро', nominal: 1, value: 94.5252, rate: 94.5252, numCode: '978' },
    { id: 'cny', currency: 'CNY', name: 'Юань', nominal: 1, value: 12.4028, rate: 12.4028, numCode: '156' },
    { id: 'irr', currency: 'IRR', name: 'Иранских риалов', nominal: 1000000, value: 47.8647, rate: 0.0000478647, numCode: '364' },
];
async function openRates(page, handler) {
    await page.goto('/_/');
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        await app.store.loadCollections();
        location.hash = '#/collections?collection=demo_offers';
    });
    await page.route('**/api/collections/currency_rates/records?**', handler);
    await page.getByRole('link', { name: 'Курсы валют', exact: true }).click();
}
function ratesResponse(route, rows) {
    return route.fulfill({ json: { page: 1, perPage: 500, totalItems: rows.length, totalPages: rows.length ? 1 : 0, items: rows } });
}
function fixtureRates(route) {
    const params = new URL(route.request().url()).searchParams;
    const filter = params.get('filter') || '';
    if (!filter) return ratesResponse(route, [{ date: '2026-10-02 00:00:00.000Z' }]);
    if (filter.includes('<')) return ratesResponse(route, filter.includes('2026-10-02') ? [{ date: '2026-10-01 00:00:00.000Z' }] : []);
    if (filter.includes('2026-10-02')) return ratesResponse(route, rateRows.map(r => ({ ...r, date: '2026-10-02 00:00:00.000Z' })));
    if (filter.includes('2026-10-01')) return ratesResponse(route, rateRows.map(r => ({ ...r, rate: r.currency === 'USD' ? 82 : r.rate, date: '2026-10-01 00:00:00.000Z' })));
    return ratesResponse(route, []);
}

test('currency dashboard shows normalized rates, dated changes and searchable history', async ({ page }, testInfo) => {
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await openRates(page, fixtureRates);
    const dashboard = page.locator('.cr-dashboard');
    const table = dashboard.getByRole('table');
    await expect(dashboard.getByRole('heading', { name: 'Курсы к рублю' })).toBeVisible();
    await expect(table.getByRole('row')).toHaveCount(5);
    await expect(dashboard.locator('.cr-highlight').first()).toContainText('+1,2454');
    await expect(dashboard.locator('.cr-comparison')).toContainText('1 октября 2026');
    await expect(table.getByRole('row').filter({ hasText: 'IRR' })).toContainText('0,00004786');
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        for (const width of [1280, 375]) {
            await page.setViewportSize({ width, height: 1000 });
            expect(await dashboard.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await dashboard.screenshot({ path: `test-results/rates-dashboard-${testInfo.project.name}-${theme}-${width}.png` });
            if (width === 375) {
                await table.scrollIntoViewIfNeeded();
                await expect(table.getByRole('row').filter({ hasText: 'IRR' })).toBeInViewport();
                await page.screenshot({ path: `test-results/rates-table-${testInfo.project.name}-${theme}-${width}.png` });
            }
        }
    }
    await dashboard.getByLabel('Поиск валюты').fill('риал');
    await expect(table.getByRole('row')).toHaveCount(2);
    await expect(table).toContainText('1000000 IRR');
    await dashboard.getByLabel('Поиск валюты').fill('нет валюты');
    await expect(dashboard.getByText('Валюта не найдена', { exact: true })).toBeVisible();
    await dashboard.getByRole('button', { name: 'Сбросить поиск' }).click();
    await expect(table.getByRole('row')).toHaveCount(5);
    await dashboard.getByLabel('Дата действия курса').fill('2026-10-01');
    await expect(dashboard.locator('.cr-highlight').first()).toContainText('82,00');
    await expect(dashboard.locator('.cr-comparison')).toContainText('Для сравнения нужна');
    await page.reload();
    await expect(dashboard.getByLabel('Дата действия курса')).toHaveValue('2026-10-01');
    await expect(dashboard.locator('.cr-highlight').first()).toContainText('82,00');
    await dashboard.getByLabel('Дата действия курса').fill('2026-09-20');
    await expect(dashboard.getByRole('heading', { name: 'На эту дату курсов нет' })).toBeVisible();
    await dashboard.getByRole('button', { name: 'Последние курсы' }).click();
    await expect(dashboard.getByLabel('Дата действия курса')).toHaveValue('2026-10-02');
    await expect(table.getByRole('row')).toHaveCount(5);
    expect(errors).toEqual([]);
});

test('currency dashboard distinguishes no data, failed requests and unavailable comparison', async ({ page }) => {
    let mode = 'empty';
    await openRates(page, route => {
        if (mode === 'empty') return ratesResponse(route, []);
        if (mode === 'error' || (mode === 'comparison-error' && (new URL(route.request().url()).searchParams.get('filter') || '').includes('<'))) return route.fulfill({ status: 503, json: { message: 'Тестовая ошибка подключения' } });
        return fixtureRates(route);
    });
    const dashboard = page.locator('.cr-dashboard');
    await expect(dashboard.getByRole('heading', { name: 'Курсы пока не загружены' })).toBeVisible();
    mode = 'error';
    await dashboard.getByRole('button', { name: 'Обновить данные' }).click();
    await expect(dashboard.getByRole('alert')).toContainText('Тестовая ошибка подключения');
    mode = 'comparison-error';
    await dashboard.getByRole('button', { name: 'Повторить', exact: true }).click();
    await expect(dashboard.getByRole('table').getByRole('row')).toHaveCount(5);
    await expect(dashboard.locator('.cr-comparison')).toContainText('сравнение недоступно');
    mode = 'success';
    await dashboard.getByRole('button', { name: 'Обновить данные' }).click();
    await expect(dashboard.locator('.cr-comparison')).toContainText('1 октября 2026');
});
