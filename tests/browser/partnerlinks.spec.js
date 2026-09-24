import { test, expect } from "@playwright/test";
import { selectChoice } from "./select-choice.js";

test("dynamic link reload waits for collections and aligns switches", async ({ page }, testInfo) => {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const filter = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        const [policy] = await app.pb.collection("dynamic_link_settings").getFullList();
        return `content_set = '${policy.content_set}'`;
    });
    await page.goto("/_/#/dynamic-links?filter=" + encodeURIComponent(filter));
    const form = page.locator(".ps-record-form");
    await expect(form).toBeVisible();
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        let release;
        const loaded = new Promise(resolve => release = resolve);
        const hold = async route => { await loaded; await route.continue(); };
        await page.route("**/api/collections?**", hold);
        try {
            await page.reload();
            await expect(page.getByRole("status")).toHaveText("Загрузка настроек…");
            await expect(page.getByText(/Общие настройки ещё не подключены/)).toHaveCount(0);
        } finally { release(); }
        await expect(form).toBeVisible();
        await page.unroute("**/api/collections?**", hold);
        expect(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("filter")).toBe(filter);
        await expect(form.getByLabel("Режим открытия", { exact: true })).toBeVisible();
        for (const width of [1000, 390]) {
            await page.setViewportSize({ width, height: 900 });
            const grid = form.locator(".dl-grid").first();
            const geometry = await grid.locator(".dl-switch label").evaluateAll(labels => labels.map(label => {
                const rect = label.getBoundingClientRect();
                const track = getComputedStyle(label, "::before");
                const thumb = getComputedStyle(label, "::after");
                return { top: rect.top, left: rect.left, centered: Math.abs(parseFloat(track.top) + parseFloat(track.height) / 2 - rect.height / 2) < 1,
                    thumbCentered: Math.abs(parseFloat(thumb.top) + parseFloat(thumb.height) / 2 - rect.height / 2) < 1 };
            }));
            expect(geometry.every(item => item.centered && item.thumbCentered)).toBe(true);
            expect(new Set(geometry.map(item => item.left)).size).toBe(width > 600 ? 2 : 1);
            expect(await form.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await grid.screenshot({ path: `test-results/dynamiclink-switches-${testInfo.project.name}-${theme}-${width}.png` });
        }
    }
    expect(errors).toEqual([]);
});

test("dedicated link pages hide collections without removing record access", async ({ page }, testInfo) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=demo_offers";
    });
    const sidebar = page.locator(".collections-sidebar");
    await expect(sidebar).toBeVisible();
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await expect(sidebar.locator('[title="partner_links"]')).toBeHidden();
        await expect(sidebar.locator('[title="dynamic_link_settings"]')).toBeHidden();
        await expect(sidebar.locator('[title="demo_offers"]')).toBeVisible();
        await sidebar.screenshot({ path: `test-results/link-sidebar-${testInfo.project.name}-${theme}.png` });
    }
    expect(await page.evaluate(() => ["partner_links", "dynamic_link_settings"].every(name => app.store.collections.some(c => c.name === name)))).toBe(true);
    await page.getByRole("link", { name: "Dynamic Link", exact: true }).click();
    const form = page.locator(".ps-record-form");
    await expect(form).toBeVisible();
    await expect(form.getByRole("button", { name: "Удалить", exact: true })).toBeHidden();
    await expect(form.getByLabel("Сохранять cookies", { exact: true }).first()).toHaveAttribute("type", "checkbox");
    await expect(form.getByLabel("Сохранять cookies", { exact: true }).first()).toHaveClass("switch");
    expect(await form.locator(".dl-switch label").first().evaluate(el => getComputedStyle(el, "::before").width)).toBe("41px");
    const result = await page.evaluate(async () => {
        const before = await app.pb.collection("dynamic_link_settings").getFullList();
        let status;
        try { await app.pb.collection("dynamic_link_settings").delete(before[0].id); status = 204; }
        catch (error) { status = error.status; }
        return { status, preserved: JSON.stringify(before) === JSON.stringify(await app.pb.collection("dynamic_link_settings").getFullList()) };
    });
    expect(result).toEqual({ status: 403, preserved: true });
});

test("Collections returns to content after visiting Dynamic Link", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=demo_offers";
    });
    await expect(page.locator(".collections-sidebar")).toBeVisible();
    await expect(page).toHaveURL(/collection=demo_offers&filter=/);
    const contentURL = page.url();
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.getByRole("link", { name: "Dynamic Link", exact: true }).click();
        await expect(page.locator(".dl-settings-page .ps-record-form")).toBeVisible();
        await expect(page).toHaveURL(/#\/dynamic-links\?/);
        await page.getByRole("link", { name: "Collections", exact: true }).click();
        await expect(page).toHaveURL(contentURL);
        await expect(page.locator(".collections-sidebar")).toBeVisible();
        await expect(page.locator(".dl-settings-page")).toHaveCount(0);
    }
    await page.getByRole("link", { name: "Dynamic Link", exact: true }).click();
    await expect(page.locator(".dl-settings-page .ps-record-form")).toBeVisible();
    await page.reload();
    await expect(page.locator(".dl-settings-page .ps-record-form")).toBeVisible();
    await page.getByRole("link", { name: "Collections", exact: true }).click();
    await expect(page).toHaveURL(contentURL);

    // Recover history written by an older extension, including a cold load.
    await page.evaluate(() => {
        localStorage.setItem("pbLastActiveCollection", app.store.collections.find(c => c.name === "dynamic_link_settings").id);
        location.hash = "#/dynamic-links";
    });
    await expect(page.locator(".dl-settings-page .ps-record-form")).toBeVisible();
    await page.evaluate(() => localStorage.setItem("pbLastActiveCollection", "dynamic_link_settings"));
    await page.reload();
    await expect(page.locator(".dl-settings-page .ps-record-form")).toBeVisible();
    await page.getByRole("link", { name: "Collections", exact: true }).click();
    await expect(page.locator(".collections-sidebar")).toBeVisible();
    await expect(page).not.toHaveURL(/collection=(dynamic_link_settings|partner_links)/);
    await expect(page).not.toHaveURL(/filter=/);
});

test("Collections leaves a service collection opened by an old URL", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const settings = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        return app.store.collections.find(c => c.name === "dynamic_link_settings").id;
    });
    for (const collection of ["dynamic_link_settings", settings]) {
        await page.goto("/_/#/collections?collection=" + collection);
        await expect(page.locator(".ps-record-form")).toBeVisible();
        await page.reload();
        await expect(page.locator(".ps-record-form")).toBeVisible();
        await expect(page.getByRole("link", { name: "Collections", exact: true })).toHaveClass(/active/);
        await page.getByRole("link", { name: "Collections", exact: true }).click();
        await expect(page).not.toHaveURL(/collection=dynamic_link_settings/);
        await expect(page.locator(".collections-sidebar")).toBeVisible();
        await expect(page.locator(".ps-record-form")).toHaveCount(0);
        expect(await page.evaluate(() => app.store.activeCollection.name)).not.toBe("dynamic_link_settings");
    }
});

test("partner settings use native top navigation and profile field selector", async ({ page }, testInfo) => {
    const errors = [], recordRequests = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("request", request => { if (request.url().includes("/api/collections/partner_links/records")) recordRequests.push(request.url()); });
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/settings";
    });
    await expect(page.getByLabel("Application name", { exact: true })).toBeVisible();
    const icons = await page.locator('.settings-sidebar .nav-item i').evaluateAll(els => els.map(el => el.className));
    expect(icons.length).toBeGreaterThan(3);
    await page.getByRole("link", { name: "Партнёрские ссылки", exact: true }).click();
    const settings = page.locator('[data-pb="pagePartnerLinks"]');
    await expect(settings.getByRole("button", { name: "Добавить ссылку", exact: true })).toBeVisible();
    await expect(settings.getByRole("heading", { name: "AppMetrica", exact: true })).toHaveCount(0);
    await settings.getByRole("link", { name: "Настройки", exact: true }).click();
    await expect(settings.getByRole("heading", { name: "AppMetrica", exact: true })).toBeVisible();
    await expect(page.locator('.settings-sidebar')).toHaveCount(0);
    await expect(page.locator('.app-main-nav .header-link.active')).toHaveText("Партнёрские ссылки");
    await expect(settings.getByRole("button", { name: "Добавить ссылку", exact: true })).toHaveCount(0);
    if (testInfo.project.name === "schemalock") {
        await expect(settings.getByLabel("Application ID", { exact: true })).toBeDisabled();
        await expect(settings.getByRole("button", { name: "Сохранить настройки", exact: true })).toBeDisabled();
        await expect(settings.getByRole("button", { name: "Добавить провайдера", exact: true })).toBeDisabled();
        const provider = settings.locator(".pl-provider").first();
        await provider.locator(":scope > summary").click();
        await expect(provider.getByLabel("Название провайдера", { exact: true })).toBeDisabled();
        const status = await page.evaluate(async () => {
            const config = await app.pb.send("/api/partnerlinks/admin/config");
            try { await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: { ...config, applicationId: 999, locks: { all: false } } }); return 200; }
            catch (error) { return error.status; }
        });
        expect(status).toBe(403);
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await settings.screenshot({ path: `test-results/partner-settings-locked-${theme}.png` });
        }
        expect(errors).toEqual([]);
        return;
    }
    await settings.getByLabel("Публичный URL сервера", { exact: true }).fill("https://links.example.test");
    await settings.getByLabel("Application ID", { exact: true }).fill("1234");
    await expect(settings.getByLabel("Без заявки: хранить дней", { exact: true })).toHaveValue("14");
    await expect(settings.getByLabel("С заявкой: хранить дней", { exact: true })).toHaveValue("0");
    await settings.getByLabel("Без заявки: хранить дней", { exact: true }).fill("21");
    await settings.getByLabel("С заявкой: хранить дней", { exact: true }).fill("90");
    await settings.getByLabel("Post API key", { exact: true }).fill("browser-fake-appmetrica-key");
    await settings.getByRole("button", { name: "Добавить провайдера", exact: true }).click();
    const provider = settings.locator('.pl-provider').last();
    const id = `browser-${Date.now()}`;
    await provider.getByLabel("Название провайдера", { exact: true }).fill("Тестовый партнёр");
    await provider.getByLabel("ID провайдера", { exact: true }).fill(id);
    await expect(provider.getByLabel("Когда начислять Revenue", { exact: true })).toContainText('Подтверждение (апрув)');
    await provider.getByLabel("Передавать доход в Revenue", { exact: true }).check();
    await selectChoice(provider.getByLabel("Когда начислять Revenue", { exact: true }), 'Холд / ожидание');
    await provider.getByLabel("Сумма", { exact: true }).fill("payout");
    await provider.getByLabel("Валюта", { exact: true }).fill("currency");
    await expect(provider.locator('.pl-revenue')).toContainText("offer_click → offer_lead → offer_hold");
    await provider.getByLabel("Секрет постбека", { exact: true }).fill("browser-provider-secret-123");
    await selectChoice(provider.getByLabel("Где передавать секрет", { exact: true }), 'Тело запроса (POST JSON / form)');
    await provider.getByLabel("Имя параметра / заголовка секрета", { exact: true }).fill("auth.secret");
    const rows = provider.locator('.pl-status-mapping .pl-mapping-row');
    await rows.first().getByLabel("Статус партнёра", { exact: true }).fill("1");
    await selectChoice(rows.first().getByLabel("Значение статуса", { exact: true }), 'Подтверждение');
    await expect(rows.first()).toContainText("offer_approved");
    await provider.getByRole("button", { name: "Добавить параметр", exact: true }).click();
    await provider.getByLabel("Имя параметра в конверсии", { exact: true }).fill("offerId");
    await provider.getByLabel("Поле партнёра", { exact: true }).fill("data.offer_id");
    await settings.getByRole("button", { name: "Сохранить настройки", exact: true }).click();
    await expect(settings.locator('.alert[role="status"]')).toHaveText("Настройки сохранены.");
    const cfg = await page.evaluate(() => app.pb.send("/api/partnerlinks/admin/config"));
    expect(cfg).toMatchObject({ pendingRetentionDays: 21, conversionRetentionDays: 90 });
    expect(cfg.hasPostApiKey && !cfg.postApiKey && cfg.providers.every(p => p.hasSecret && p.secret)).toBe(true);
    expect(cfg.providers.at(-1)).toMatchObject({ sendRevenue: true, revenueStatus: "hold", secretLocation: "body", secretName: "auth.secret", statuses: { "1": "approved" }, extraFields: { offerId: "data.offer_id" } });
    await provider.locator(':scope > summary').click(); // may already be expanded after save
    if (!(await provider.evaluate(el => el.open))) await provider.locator(':scope > summary').click();
    await provider.getByRole("button", { name: "Добавить соответствие", exact: true }).click();
    await rows.last().getByLabel("Статус партнёра", { exact: true }).fill("1");
    await settings.getByRole("button", { name: "Сохранить настройки", exact: true }).click();
    await expect(settings.getByRole("alert")).toContainText("повторяется");
    await rows.last().getByRole("button", { name: /^Удалить соответствие/ }).click();
    await settings.getByRole("button", { name: "Сохранить настройки", exact: true }).click();
    await expect(settings.locator('.alert[role="status"]')).toHaveText("Настройки сохранены.");
    await provider.locator('.pl-examples > summary').click();
    await expect(provider.locator('.pl-examples')).toContainText('"auth": {"secret": "YOUR_SECRET"}');
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            expect(await settings.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            const overlaps = await provider.evaluate(el => {
                const groups = [el.querySelector('.pl-provider-body'), ...el.querySelectorAll('.pl-mapping-list')];
                return groups.flatMap(group => [...group.children].slice(1).filter((child, i) => child.getBoundingClientRect().top < group.children[i].getBoundingClientRect().bottom - 1).map(child => child.className));
            });
            expect(overlaps).toEqual([]);
            const controls = await rows.first().evaluate(row => {
                const rect = el => { const r = el.getBoundingClientRect(); return { top: r.top, height: r.height }; };
                return [...row.querySelectorAll('input, select, button')].filter(el => el.checkVisibility()).map(rect);
            });
            expect(controls.map(c => c.height)).toEqual([44, 44, 44]);
            if (width > 600) expect(new Set(controls.map(c => c.top)).size).toBe(1);
            await provider.locator('.pl-status-mapping').screenshot({ path: `test-results/statuses-${testInfo.project.name}-${theme}-${width}.png`, animations: "disabled" });
            await provider.locator('.pl-status-mapping').scrollIntoViewIfNeeded();
            await provider.locator('.pl-revenue').screenshot({ path: `test-results/revenue-${testInfo.project.name}-${theme}-${width}.png`, animations: "disabled" });
            await page.screenshot({ path: `test-results/partnerlinks-${testInfo.project.name}-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    await page.reload(); // direct opening on a narrow viewport preserves the native toggle
    await expect(settings.getByRole("heading", { name: "AppMetrica", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Партнёрские ссылки", exact: true })).toBeVisible();
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.reload();
    await expect(settings.getByLabel("Без заявки: хранить дней", { exact: true })).toHaveValue("21");
    await expect(settings.getByLabel("С заявкой: хранить дней", { exact: true })).toHaveValue("90");
    await provider.locator(':scope > summary').click();
    await expect(provider.getByLabel("Когда начислять Revenue", { exact: true })).toContainText('Холд / ожидание');
    await expect(provider.getByLabel("Передавать доход в Revenue", { exact: true })).toBeChecked();
    await page.getByRole("link", { name: "Settings", exact: true }).click();
    await expect(page.getByLabel("Application name", { exact: true })).toBeVisible();
    await expect(settings).toHaveCount(0);
    expect(await page.locator('.settings-sidebar .nav-item i').evaluateAll(els => els.map(el => el.className))).toEqual(icons);
    await page.evaluate(() => location.hash = "#/settings/partner-links");
    await expect(settings.getByRole("heading", { name: "AppMetrica", exact: true })).toBeVisible();
    await expect(page.locator('.app-main-nav .header-link.active')).toHaveText("Партнёрские ссылки");
    expect(recordRequests.length).toBeGreaterThan(0);
    expect(errors).toEqual([]);
});

test("dynamic link field uses native record form with typed controls", async ({ page }, testInfo) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=" + app.store.collections.find(c => c.name === "partner_links").id;
    });
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    const field = page.locator('.dl-input');
    await expect(field.getByLabel("URL", { exact: true })).toBeVisible();
    await expect(page.locator('[name="opening"]')).toHaveCount(0);
    await expect(field.getByLabel("Сохранять cookies", { exact: true })).toHaveCount(0);
    await expect(field.locator(".dl-link-options")).not.toHaveAttribute("open", "");
    await page.locator('[name="name"]').fill("Typed dynamic link");
    await page.locator(".pl-provider-input").getByLabel("provider", { exact: true }).click();
    await page.locator('.pl-provider-input .select-option').filter({ hasText: "(demo)" }).click();
    await page.locator('[name="active"]').check();
    await field.getByLabel("URL", { exact: true }).fill("https://partner.example/typed?campaign=1");
    await field.getByText("Настроить Dynamic Link", { exact: true }).click();
    await expect(field.getByLabel("Категория", { exact: true })).toBeVisible();
    await field.getByLabel("Заголовок WebView", { exact: true }).fill("Заголовок для браузерной ссылки");
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        expect(await field.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
        await field.screenshot({ path: `test-results/dynamiclink-${testInfo.project.name}-${theme}.png`, animations: "disabled" });
        for (const width of [390, 1280]) {
            await page.setViewportSize({ width, height: 1000 });
            const selector = field.getByLabel("Категория", { exact: true });
            await selector.click();
            const popup = selector.locator('..').locator('.dropdown');
            await expect(popup).toBeVisible();
            await expect(popup).toHaveCSS('opacity', '1');
            const bounds = await popup.evaluate(el => {
                const r = el.getBoundingClientRect();
                return { left: r.left, right: r.right, width: innerWidth };
            });
            expect(bounds.left).toBeGreaterThanOrEqual(0);
            expect(bounds.right).toBeLessThanOrEqual(bounds.width);
            await page.screenshot({ path: `test-results/link-category-${testInfo.project.name}-${theme}-${width}.png`, animations: "disabled" });
            await page.keyboard.press('Escape');
            const spacing = await field.getByLabel("Заголовок WebView", { exact: true }).evaluate(el => {
                const field = el.closest('.field');
                return field.getBoundingClientRect().top - field.previousElementSibling.getBoundingClientRect().bottom;
            });
            expect(spacing).toBeGreaterThanOrEqual(16);
        }
    }
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(field).toHaveCount(0);
    const record = await page.evaluate(() => app.pb.collection("partner_links").getFirstListItem('name="Typed dynamic link"'));
    expect(record).not.toHaveProperty("opening");
    expect(record).not.toHaveProperty("url");
    expect(record.link).toMatchObject({ url: "https://partner.example/typed?campaign=1", mode: "browser", title: "Заголовок для браузерной ссылки", saveCooke: true, showLoader: true });
    await page.evaluate(() => location.hash = "#/partner-links");
    const cards = page.locator(".pl-links");
    await cards.getByLabel("Поиск ссылок", { exact: true }).fill("Typed dynamic link");
    await cards.getByRole("button", { name: "Редактировать: Typed dynamic link", exact: true }).click();
    await field.getByLabel("URL", { exact: true }).fill("https://partner.example/updated");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(field).toHaveCount(0);
    await expect(cards.locator(".pl-link-card")).toContainText("https://partner.example/updated");
    expect(errors).toEqual([]);
});

test("global dynamic link policy uses native Singleton and Variants in both themes", async ({ page }, testInfo) => {
    await page.goto('/_/');
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        await app.store.loadCollections();
        location.hash = '#/dynamic-links';
    });
    const form = page.locator('.ps-record-form');
    await expect(form).toBeVisible();
    await expect(form.locator(".json-editor, .cm-editor")).toHaveCount(0);
    await form.getByRole("button", { name: "Добавить категорию", exact: true }).click();
    const category = form.locator(".dl-category").last();
    await category.getByLabel("Название категории", { exact: true }).pressSequentially("Партнёры");
    await expect(category.getByLabel("Название категории", { exact: true })).toHaveValue("Партнёры");
    await category.getByLabel("Код категории", { exact: true }).fill("partners");
    await selectChoice(category.getByLabel("Режим открытия категории", { exact: true }), 'WebView приложения');
    await expect(category.getByLabel("Сохранять cookies", { exact: true })).toBeChecked();
    await category.getByLabel("Сохранять cookies", { exact: true }).uncheck();
    await category.getByRole("button", { name: "Вернуть общую настройку: Сохранять cookies", exact: true }).click();
    await expect(category.getByLabel("Сохранять cookies", { exact: true })).toBeChecked();
    await category.getByLabel("Сохранять cookies", { exact: true }).uncheck();
    await expect(form.getByRole("button", { name: "Удалить", exact: true })).toBeHidden();

    const tabs = form.getByRole("tablist", { name: "Категории ссылок", exact: true });
    await expect(tabs.getByRole("tab", { name: "Партнёры", exact: true })).toHaveAttribute("aria-selected", "true");
    await form.getByRole("button", { name: "Добавить категорию", exact: true }).click();
    await expect(tabs.getByRole("tab", { name: "Новая категория", exact: true })).toHaveAttribute("aria-selected", "true");
    await category.getByLabel("Название категории", { exact: true }).pressSequentially("Резервная категория с длинным названием");
    await selectChoice(category.getByLabel("Режим открытия категории", { exact: true }), 'Встроенный браузер ОС');
    await tabs.getByRole("tab", { name: "Партнёры", exact: true }).click();
    await expect(form.getByRole("tabpanel")).toHaveCount(1);
    await expect(category.getByLabel("Код категории", { exact: true })).toHaveValue("partners");
    await expect(category.getByLabel("Режим открытия категории", { exact: true })).toContainText('WebView приложения');
    await expect(category.getByLabel("Сохранять cookies", { exact: true })).not.toBeChecked();
    await tabs.getByRole("tab", { name: "Партнёры", exact: true }).press("ArrowRight");
    await expect(tabs.getByRole("tab", { name: "Резервная категория с длинным названием", exact: true })).toBeFocused();
    await expect(category.getByLabel("Режим открытия категории", { exact: true })).toContainText('Встроенный браузер ОС');
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        for (const width of [390, 1280]) {
            await page.setViewportSize({ width, height: 1000 });
            expect(await form.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await tabs.screenshot({ path: `test-results/category-tabs-${testInfo.project.name}-${theme}-${width}.png` });
        }
    }
    page.once("dialog", dialog => dialog.accept());
    await category.getByRole("button", { name: "Удалить категорию", exact: true }).click();
    await expect(tabs.getByRole("tab", { name: "Партнёры", exact: true })).toHaveAttribute("aria-selected", "true");
    await expect(category.getByLabel("Код категории", { exact: true })).toHaveValue("partners");

    await expect(form.getByLabel('Предупреждение', { exact: true })).toBeVisible();
    if (testInfo.project.name === 'schemalock') {
        await page.getByRole('button', { name: 'Variants', exact: true }).click();
    } else {
        await page.getByRole('button', { name: 'Collection settings', exact: true }).click();
        await page.getByRole('button', { name: 'Variants', exact: true }).click();
    }
    await expect(page.locator('.pv-editor')).toBeVisible();
    await page.getByRole('button', { name: testInfo.project.name === 'schemalock' ? 'Закрыть' : 'Close', exact: true }).click();
    await expect(page.locator('.pv-editor')).not.toBeVisible();
    // The policy remains an ordinary typed record in the shared Singleton UI.
    await selectChoice(form.getByLabel('Режим открытия', { exact: true }), 'Внешний браузер');
    await form.getByRole('button', { name: 'Сохранить', exact: true }).click();
    await expect(form).toContainText('Сохранено.');
    const policy = await page.evaluate(async () => (await app.pb.collection("dynamic_link_settings").getFullList())[0]);
    expect(policy.categories).toContainEqual({ key: "partners", label: "Партнёры", options: { mode: "appView", saveCooke: false } });

    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        expect(await form.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
        await page.locator('.page-content').evaluate(el => el.scrollTop = 0);
        await page.screenshot({ path: `test-results/dynamiclink-policy-${testInfo.project.name}-${theme}.png`, animations: "disabled" });
        await category.screenshot({ path: `test-results/dynamiclink-category-${testInfo.project.name}-${theme}.png`, animations: "disabled" });
        for (const width of [390, 1280]) {
            await page.setViewportSize({ width, height: 1000 });
            expect(await form.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
        }

    }
    await page.evaluate(() => location.hash = "#/partner-links");
    await page.getByRole("button", { name: "Добавить ссылку", exact: true }).click();
    const link = page.locator(".dl-input");
    await link.getByLabel("URL", { exact: true }).fill("https://example.com/category");
    await link.getByText("Настроить Dynamic Link", { exact: true }).click();
    await selectChoice(link.getByLabel("Категория", { exact: true }), 'Партнёры');
    await expect(link.getByLabel("Заголовок WebView", { exact: true })).toBeVisible();
    await link.getByLabel("Заголовок WebView", { exact: true }).fill("Предложение");
    await page.locator('[name="name"]').fill("Category link");
    await page.locator(".pl-provider-input").getByLabel("provider", { exact: true }).click();
    await page.locator('.pl-provider-input .select-option').filter({ hasText: "(demo)" }).click();
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(link).toHaveCount(0);
    const saved = await page.evaluate(() => app.pb.collection("partner_links").getFirstListItem('name="Category link"'));
    expect(saved.link.category).toBe("partners");
    expect(saved.link.title).toBe("Предложение");
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        const cards = page.locator(".pl-links");
        await expect(cards.locator(".pl-link-card").first()).toBeVisible();
        expect(await cards.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
        await cards.screenshot({ path: `test-results/partner-cards-${testInfo.project.name}-${theme}.png` });
    }
});

test("conversations expose only native record preview and no Variants", async ({ page }, testInfo) => {
    await page.goto('/_/');
    await page.waitForFunction(() => window.app?.store?._ready);
    const record = await page.evaluate(async () => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        const user = new app.pb.constructor(app.pb.baseURL);
        await user.collection('users').authWithPassword('default@variants.test', 'demo-variants-123');
        const link = await user.collection('partner_links').getFirstListItem('provider = "demo"');
        const issued = await user.send(`/api/partnerlinks/links/${link.id}/resolve`, { method: 'POST', body: {} });
        const row = await user.collection('conversations').getFirstListItem(`clickId = "${issued.clickId}"`);
        await app.store.loadCollections();
        location.hash = '#/collections?collection=conversations';
        return { id: row.id, collectionId: row.collectionId };
    });
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await expect(page.getByRole('button', { name: 'New record', exact: true })).toBeHidden();
        await expect(page.getByRole('button', { name: 'Variants', exact: true })).toBeHidden();
        await expect(page.getByRole('button', { name: 'Collection settings', exact: true })).toBeHidden();
        await expect(page.locator('.page-content .pv-toolbar')).toHaveCount(0);
        await page.evaluate(record => {
            const collection = app.store.collections.find(c => c.id === record.collectionId);
            if (!collection.system || collection.fields.some(f => f.name === 'content_set')) throw new Error('Variants must not be available');
            app.modals.openRecordUpsert(collection, record.id);
        }, record);
        const preview = page.locator('.record-preview-modal');
        await expect(preview).toBeVisible();
        await expect(page.locator('.record-upsert-modal')).toHaveCount(0);
        await expect(preview.getByRole('button', { name: /^(Save|Delete|Duplicate)$/ })).toHaveCount(0);
        await expect(preview).toContainText('pending');
        await page.screenshot({ path: `test-results/conversations-${testInfo.project.name}-${theme}.png` });
        await page.keyboard.press('Escape');
        await expect(preview).toHaveCount(0);
    }
    await page.evaluate(() => location.hash = '#/collections?collection=partner_links');
    await expect(page.getByRole('button', { name: 'New record', exact: true })).toBeVisible();
});

test("user picker search preserves width, focus and results while typing", async ({ page }, testInfo) => {
    await page.goto('/_/');
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        await app.store.loadCollections();
    });
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 900 });
      for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.evaluate(() => app.modals.openRecordsPicker({ collection: 'users', maxSelect: 1, onselect: () => {} }));
        const modal = page.locator('.records-picker-modal');
        await expect(modal).toBeVisible();
        const search = modal.locator('.editor-content');
        await expect(search).toBeVisible();
        const before = await search.boundingBox();
        expect(before.width).toBeGreaterThan(width > 600 ? 250 : 80);
        await search.pressSequentially('default@variants.test', { delay: 25 });
        await expect(search).toBeFocused();
        await expect(search).toHaveText('default@variants.test');
        expect((await search.boundingBox()).width).toBeGreaterThan(width > 600 ? 250 : 80);
        await search.press('Enter');
        await expect(modal.locator('.records-picker-list > .list-item.handle')).toHaveCount(1);
        await expect(modal.locator('.records-picker-list')).toContainText('Обычный пользователь');
        await expect(search).toHaveText('default@variants.test');
        await page.screenshot({ path: `test-results/picker-search-${testInfo.project.name}-${theme}-${width}.png` });
        await modal.getByRole('button', { name: 'Clear', exact: true }).click();
        await expect(search).toHaveText('');
        await expect.poll(() => modal.locator('.records-picker-list > .list-item.handle').count()).toBeGreaterThan(1);
        await modal.getByRole('button', { name: 'Close', exact: true }).click();
        await expect(modal).toHaveCount(0);
      }
    }
});

test("provider selector loads configured names, searches and saves IDs", async ({ page }, testInfo) => {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const fixture = await page.evaluate(async locked => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        const config = await app.pb.send("/api/partnerlinks/admin/config");
        const providerId = locked ? "browser-selector" : "selector-" + Date.now();
        if (!locked) {
            config.providers.push({ ...config.providers.find(p => p.id === "demo"), id: providerId, name: "Другой партнёр", secret: "selector-test-secret-123" });
            await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: config });
        }
        const demo = await app.pb.collection("partner_links").getOne("demopartner0001");
        const record = await app.pb.collection("partner_links").create({ name: "Provider selector", provider: "demo", active: true, link: demo.link });
        location.hash = "#/collections?collection=" + record.collectionId;
        return { providerId, recordId: record.id, locked };
    }, testInfo.project.name === "schemalock");
    const open = () => page.evaluate(async id => {
        const record = await app.pb.collection("partner_links").getOne(id);
        app.modals.openRecordUpsert(app.store.collections.find(c => c.name === "partner_links"), record);
    }, fixture.recordId);
    try {
        await open();
        const field = page.locator('.pl-provider-input');
        await expect(field.locator('.selected-container')).toContainText("(demo)");
        await expect(field.locator('textarea')).toHaveCount(0);
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await page.locator(".pl-provider-input").getByLabel("provider", { exact: true }).click();
            await field.getByPlaceholder("Search...").fill("Другой");
            const option = field.locator('.select-option:visible');
            await expect(option).toHaveCount(1);
            await expect(option).toHaveText("Другой партнёр (" + fixture.providerId + ")");
            await field.screenshot({ path: "test-results/provider-" + testInfo.project.name + "-" + theme + ".png" });
            await option.click();
        }
        await page.getByRole("button", { name: "Save changes", exact: true }).click();
        await expect(field).toHaveCount(0);
        expect(await page.evaluate(async id => (await app.pb.collection("partner_links").getOne(id)).provider, fixture.recordId)).toBe(fixture.providerId);
        if (!fixture.locked) {
        await page.evaluate(async id => {
            const config = await app.pb.send("/api/partnerlinks/admin/config");
            config.providers.find(p => p.id === id).name = "Обновлённый партнёр";
            await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: config });
        }, fixture.providerId);
        await open();
        await expect(field.locator('.selected-container')).toHaveText("Обновлённый партнёр (" + fixture.providerId + ")");
        }
        expect(errors).toEqual([]);
    } finally {
        await page.evaluate(async ({ recordId, providerId, locked }) => {
            await app.pb.collection("partner_links").delete(recordId);
            if (locked) return;
            const config = await app.pb.send("/api/partnerlinks/admin/config");
            config.providers = config.providers.filter(p => p.id !== providerId);
            await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: config });
        }, fixture);
    }
});

test("provider selector explains empty configuration and retries loading errors", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
    });
    let mode = "error";
    await page.route("**/api/partnerlinks/admin/config", route => mode === "real" ? route.continue() : route.fulfill({
        status: mode === "error" ? 503 : 200,
        contentType: "application/json",
        body: JSON.stringify(mode === "error" ? { message: "Временно недоступно" } : { providers: [] }),
    }));
    const open = () => page.evaluate(() => app.modals.openRecordUpsert(app.store.collections.find(c => c.name === "partner_links")));
    await open();
    const field = page.locator('.pl-provider-input');
    await expect(field.getByRole("alert")).toContainText("Временно недоступно");
    await expect(field.locator('.selected-container')).toBeDisabled();
    mode = "real";
    await field.getByRole("button", { name: "Повторить" }).click();
    await expect(field.locator('.selected-container')).toBeEnabled();
    await expect(field.getByRole("alert")).toHaveCount(0);
    await page.evaluate(() => app.modals.close(null, true));
    await expect(field).toHaveCount(0);
    mode = "empty";
    await open();
    await expect(field).toContainText("Сначала добавьте и сохраните провайдера");
    await expect(field.locator('.selected-container')).toBeDisabled();
});

test("Rafinad preset supplies visible secret, mappings and setup guide", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === "schemalock", "Schema Lock prohibits provider editing; readonly settings are tested separately.");
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        location.hash = "#/partner-links?tab=settings";
    });
    const settings = page.locator('[data-pb="pagePartnerLinks"]');
    await settings.getByRole("button", { name: "Добавить провайдера", exact: true }).click();
    const provider = settings.locator('.pl-provider').last();
    const id = `rafinad-${Date.now()}`;
    await provider.getByLabel("ID провайдера", { exact: true }).fill(id);
    await selectChoice(provider.getByLabel("Пресет партнёра", { exact: true }), 'Rafinad New');
    await expect(provider.getByLabel("Название провайдера", { exact: true })).toHaveValue("Rafinad New");
    await expect(provider.getByLabel("ID провайдера", { exact: true })).toHaveValue(id);
    await expect(provider.getByLabel("Шаблон ссылки", { exact: true })).toHaveValue("{url}?p_click_id={clickData}");
    const secret = provider.getByLabel("Секрет постбека", { exact: true });
    await expect(secret).toHaveAttribute("type", "text");
    const value = await secret.inputValue();
    expect(value).toMatch(/^[0-9a-f]{48}$/);
    await expect(provider.locator('.pl-preset-guide')).toContainText(value);
    await expect(provider.locator('.pl-preset-guide')).toContainText(`/api/partnerlinks/postbacks/${id}`);
    await expect(provider.locator('.pl-preset-table')).toContainText("{publisher_commission}");
    await expect(provider.locator('.pl-preset-guide')).toContainText("Одобрено = 4");
    await expect(provider.getByLabel("Передавать доход в Revenue", { exact: true })).not.toBeChecked();
    await settings.getByRole("button", { name: "Сохранить настройки", exact: true }).click();
    await expect(settings.locator('.alert[role="status"]')).toHaveText("Настройки сохранены.");
    await page.reload();
    await provider.locator(':scope > summary').click();
    await expect(secret).toHaveValue(value);
    const config = await page.evaluate(() => app.pb.send("/api/partnerlinks/admin/config"));
    expect(config.providers.at(-1)).toMatchObject({ preset: "rafinad_new", id, statuses: { "1": "lead", "2": "hold", "3": "rejected", "4": "approved" } });
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            expect(await settings.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await provider.locator('.pl-preset-guide').screenshot({ path: `test-results/rafinad-${testInfo.project.name}-${theme}-${width}.png`, animations: "disabled" });
        }
    }
    await provider.getByLabel("clickData", { exact: true }).fill("custom_token");
    await expect(provider.locator('.pl-preset-guide')).toContainText("Поля пресета изменены");
    expect(errors).toEqual([]);
});

test("managed settings and full admin lock disable editing but keep guide readable", async ({ page }) => {
    let fullLock = false;
    await page.route('**/api/partnerlinks/admin/config', async route => {
        const response = await route.fetch();
        const config = await response.json();
        const preset = config.presets.find(p => p.id === "rafinad_new");
        config.providers = [{ ...preset.provider, id: "code-rafinad", secret: "code-partner-visible-secret" }];
        config.locks = { all: fullLock, fields: ["applicationId", "postApiKey"], eventNames: ["lead"], providers: ["code-rafinad"] };
        await route.fulfill({ response, json: config });
    });
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        location.hash = "#/partner-links?tab=settings";
    });
    const settings = page.locator('[data-pb="pagePartnerLinks"]');
    await expect(settings.getByLabel("Application ID", { exact: true })).toBeDisabled();
    await expect(settings.getByLabel("Post API key", { exact: true })).toBeDisabled();
    await expect(settings.getByLabel("Публичный URL сервера", { exact: true })).toBeEnabled();
    await settings.getByText("Дополнительно: собственные имена событий", { exact: true }).click();
    await expect(settings.getByLabel("Заявка создана (лид)", { exact: true })).toBeDisabled();
    await expect(settings.getByLabel("Клик по офферу", { exact: true })).toBeEnabled();
    const provider = settings.locator('.pl-provider');
    await provider.locator(':scope > summary').click();
    await expect(provider.getByLabel("Пресет партнёра", { exact: true })).toBeDisabled();
    await expect(provider.getByLabel("Секрет постбека", { exact: true })).toHaveValue("code-partner-visible-secret");
    await expect(provider.getByLabel("Секрет постбека", { exact: true })).toBeDisabled();
    await expect(provider.getByRole("button", { name: "Убрать провайдера", exact: true })).toBeDisabled();
    await expect(provider.locator('.pl-preset-guide')).toContainText("code-partner-visible-secret");
    await expect(settings.getByRole("button", { name: "Добавить провайдера", exact: true })).toBeEnabled();
    fullLock = true;
    await page.reload();
    await expect(settings.locator('.alert[role="status"]')).toContainText("только для просмотра");
    await expect(settings.getByLabel("Публичный URL сервера", { exact: true })).toBeDisabled();
    await expect(settings.getByRole("button", { name: "Добавить провайдера", exact: true })).toBeDisabled();
    await expect(settings.getByRole("button", { name: "Сохранить настройки", exact: true })).toBeDisabled();
    await provider.locator(':scope > summary').click();
    await expect(provider.locator('.pl-preset-guide')).toBeVisible();
});
