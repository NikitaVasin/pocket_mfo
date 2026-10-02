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
        await expect(sidebar.locator('[title="conversations"]')).toBeHidden();
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

test("partner settings hide code definitions and use shared AppMetrica", async ({ page }, testInfo) => {
    const errors = []; page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/"); await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => { await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123"); location.hash = "#/partner-links?tab=settings"; });
    const settings = page.locator('[data-pb="pagePartnerLinks"]');
    await expect(settings.getByRole("heading", { name: "Готовность подключения" })).toBeVisible();
    for (const label of ["Application ID", "Post API key", "Срок открытия ссылки, секунд", "Название провайдера", "Шаблон ссылки"]) await expect(settings.getByLabel(label, { exact: true })).toHaveCount(0);
    await expect(settings.getByRole("button", { name: "Добавить провайдера" })).toHaveCount(0);
    await expect(settings.getByRole("link", { name: "Настройки и проверка AppMetrica →" })).toBeVisible();
    if (testInfo.project.name === "schemalock") {
        await expect(settings.getByLabel("Публичный URL сервера")).toHaveCount(0);
        await expect(settings.getByRole("button", { name: "Сохранить настройки" })).toHaveCount(0);
    } else {
        const field = settings.getByLabel("Публичный URL сервера"); await expect(field).toBeVisible();
        const original = await field.inputValue(); await field.fill("https://updated.example.test");
        await settings.getByRole("button", { name: "Сохранить настройки" }).click();
        await expect(settings.getByText("Настройки сохранены.", { exact: true })).toBeVisible();
        await page.reload(); await expect(field).toHaveValue("https://updated.example.test");
        await field.fill(original); await settings.getByRole("button", { name: "Сохранить настройки" }).click();
    }
    for (const theme of ["light", "dark"]) { await page.evaluate(theme => app.store.userColorScheme = theme, theme); await settings.screenshot({ path: testInfo.outputPath(`partner-settings-${theme}.png`) }); }
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
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await form.getByRole("tab", { name: "Офферы", exact: true }).click();
        await expect(form.getByLabel("Код категории", { exact: true })).toHaveValue("offers");
        await expect(form.getByLabel("Название категории", { exact: true })).toHaveValue("Офферы");
        await expect(form.getByLabel("Режим открытия категории", { exact: true })).toContainText("Общий режим");
        await form.locator(".dl-category").screenshot({ path: `test-results/default-offers-${testInfo.project.name}-${theme}.png` });
    }
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

test("conversion cards support search, status, pagination and read-only preview", async ({ page }, testInfo) => {
    const errors = []; page.on("pageerror", error => errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const fixture = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const user = new app.pb.constructor(app.pb.baseURL);
        await user.collection("users").authWithPassword("default@variants.test", "demo-variants-123");
        const link = await user.collection("partner_links").getFirstListItem('provider = "demo"');
        let issued;
        for (let i = 0; i < 25; i++) issued = await user.send(`/api/partnerlinks/links/${link.id}/resolve`, { method: "POST", body: {} });
        const row = await user.collection("conversations").getFirstListItem(`clickId = "${issued.clickId}"`);
        await app.store.loadCollections();
        location.hash = "#/partner-links";
        return { id: row.id, userId: row.userId, name: link.name };
    });
    await page.getByRole("link", { name: "Конверсии", exact: true }).click();
    await expect(page.getByRole("link", { name: "Конверсии", exact: true })).toHaveAttribute("aria-current", "page");
    const section = page.getByRole("region", { name: "Конверсии", exact: true });
    const cards = section.locator(".pl-conversion-card");
    await expect(cards).toHaveCount(24);
    await section.getByRole("button", { name: "Далее", exact: true }).click();
    await expect(section.locator(".pl-link-pagination")).toContainText("2 /");
    await expect(cards.first()).toBeVisible();
    await section.getByLabel("Поиск конверсий").fill(fixture.id);
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toContainText(fixture.name);
    await expect(cards.first()).toContainText("Ожидает конверсии");
    await expect(section.locator(".pl-link-pagination")).toContainText("1 / 1");
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        for (const width of [1280, 390]) {
            await page.setViewportSize({ width, height: 900 });
            expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await section.screenshot({ path: `test-results/conversion-cards-${testInfo.project.name}-${theme}-${width}.png` });
        }
        await cards.first().click();
        const preview = page.locator(".record-preview-modal");
        await expect(preview).toBeVisible();
        await expect(preview).toContainText(fixture.id);
        await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
        await expect(preview.getByRole("button", { name: /^(Save|Delete|Duplicate)$/ })).toHaveCount(0);
        await page.keyboard.press("Escape");
        await expect(preview).toHaveCount(0);
    }
    await selectChoice(section.locator(".select"), "Одобрена");
    await expect(section.getByText("По выбранным условиям конверсий не найдено.")).toBeVisible();
    await expect(cards).toHaveCount(0);
    await selectChoice(section.locator(".select"), "Все статусы");
    await expect(cards).toHaveCount(1);
    await section.getByLabel("Поиск конверсий").fill('" || id != "');
    await expect(section.getByText("По выбранным условиям конверсий не найдено.")).toBeVisible();
    await section.getByLabel("Поиск конверсий").fill(fixture.userId);
    await expect(cards).toHaveCount(24);
    const fail = route => route.fulfill({ status: 500, json: { message: "Тестовая ошибка загрузки" } });
    await page.route("**/api/collections/conversations/records?**", fail);
    await section.getByRole("button", { name: "Обновить", exact: true }).click();
    await expect(section.getByRole("alert")).toContainText("Тестовая ошибка загрузки");
    await expect(cards.first()).toBeHidden();
    await page.unroute("**/api/collections/conversations/records?**", fail);
    await section.getByRole("button", { name: "Повторить", exact: true }).click();
    await expect(cards.first()).toBeVisible();
    await page.reload();
    await expect(page.getByRole("heading", { name: "Конверсии", exact: true })).toBeVisible();
    await expect(cards.first()).toBeVisible();
    // The native Collections link must not restore the hidden service collection.
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.evaluate(() => {
        app.store.activeCollection = "conversations";
        localStorage.setItem("pbLastActiveCollection", "conversations");
    });
    await page.getByRole("link", { name: "Collections", exact: true }).click();
    await expect(page.locator(".collections-sidebar")).toBeVisible();
    expect(await page.evaluate(() => app.store.activeCollection.name)).not.toBe("conversations");
    expect(errors).toEqual([]);
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
        const providerId = "browser-selector";
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
        await open();
        await expect(field.locator('.selected-container')).toHaveText("Другой партнёр (" + fixture.providerId + ")");
        expect(errors).toEqual([]);
    } finally {
        await page.evaluate(async ({ recordId, providerId, locked }) => {
            await app.pb.collection("partner_links").delete(recordId);
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

test("only code-defined Rafinad preset exists and structural HTTP edits fail", async ({ page }, testInfo) => {
    await page.goto("/_/"); await page.waitForFunction(() => window.app?.store?._ready);
    const result = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const config = await app.pb.send("/api/partnerlinks/admin/config");
        const preset = config.providers.find(p => p.id === "rafinad-new");
        const statuses = [];
        for (const mutate of [c => c.providers.push({ ...preset, id: "dynamic" }), c => c.providers.find(p => p.id === "rafinad-new").urlTemplate = "{url}?custom={clickData}", c => c.openTtlSeconds++]) {
            const copy = JSON.parse(JSON.stringify(config)); mutate(copy);
            try { await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: copy, requestKey: null }); statuses.push(200); } catch (e) { statuses.push(e.status); }
        }
        location.hash = "#/partner-links?tab=settings";
        return { preset, statuses, after: await app.pb.send("/api/partnerlinks/admin/config"), before: config.version };
    });
    expect(result.preset).toMatchObject({ preset: "rafinad_new", name: "Rafinad New" });
    expect(result.statuses).toEqual([1,2,3].map(() => testInfo.project.name === "schemalock" ? 403 : 400));
    expect(result.after.version).toBe(result.before);
    await expect(page.getByLabel("Шаблон ссылки", { exact: true })).toHaveCount(0);
});

test("fully configured partner settings show status without code-owned inputs", async ({ page }) => {
    await page.route('**/api/partnerlinks/admin/config', async route => {
        const response = await route.fetch(); const config = await response.json();
        config.locks = { all: true, fields: [], providerSecrets: [] }; config.readiness = { ready: true, missing: [] };
        await route.fulfill({ response, json: config });
    });
    await page.goto("/_/"); await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => { await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123"); location.hash = "#/partner-links?tab=settings"; });
    const settings = page.locator('[data-pb="pagePartnerLinks"]');
    await expect(settings).toContainText("Все обязательные параметры заданы");
    await expect(settings.locator('input')).toHaveCount(0);
    await expect(settings.getByRole("button", { name: "Сохранить настройки" })).toHaveCount(0);
});

test("provider postback guides are collapsed and reflect configured parameters", async ({ page }, testInfo) => {
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto('/_/'); await page.waitForFunction(() => window.app?.store?._ready);
    const config = await page.evaluate(async () => {
        await app.pb.collection('_superusers').authWithPassword('browser@example.test', 'browser-test-password-123');
        const config = await app.pb.send('/api/partnerlinks/admin/config');
        location.hash = '#/partner-links?tab=settings';
        return config;
    });
    const guides = page.locator('.pl-postback-guide');
    await expect(guides).toHaveCount(config.providers.length);
    for (const guide of await guides.all()) expect(await guide.evaluate(el => el.open)).toBe(false);
    const guide = guides.filter({ has: page.locator('summary', { hasText: 'Rafinad New' }) });
    await guide.locator('summary').focus(); await page.keyboard.press('Enter');
    await expect(guide.getByRole('button', { name: 'Скопировать адрес' })).toBeVisible();
    await expect(guide).toContainText('/api/partnerlinks/postbacks/rafinad-new');
    for (const macro of ['{p_click_id}', '{status}', '{order_id}', '{publisher_commission}', '{currency}']) await expect(guide).toContainText(macro);
    await expect(guide).toContainText('Произвольный тестовый токен не создаёт конверсию');
    await expect(guide.locator('input, select, textarea')).toHaveCount(0);
    const secret = config.providers.find(p => p.id === 'rafinad-new').secret;
    expect(secret).toMatch(/^[A-Za-z0-9_-]{43}$/);
    await expect(guide.locator('.pl-postback-secret code')).toHaveText(secret);
    await expect(guide.getByRole('button', { name: 'Скопировать секрет' })).toBeVisible();
    for (const theme of ['light', 'dark']) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        for (const width of [1280, 375]) {
            await page.setViewportSize({ width, height: 1000 });
            expect(await guide.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await guide.locator('.pl-guide-table').first().scrollIntoViewIfNeeded();
            await page.screenshot({ path: testInfo.outputPath(`postback-guide-${theme}-${width}.png`) });
        }
    }
    await guide.locator('summary').click();
    await expect(guide.getByRole('button', { name: 'Скопировать адрес' })).toBeHidden();
    expect(errors).toEqual([]);
});
