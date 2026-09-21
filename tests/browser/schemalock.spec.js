import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
    page.errors = [];
    page.on("pageerror", error => page.errors.push(error.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=demo_offers";
    });
    await expect(page.getByRole("button", { name: "Variants", exact: true })).toBeVisible();
});

test.afterEach(async ({ page }) => expect(page.errors).toEqual([]));

test("schema actions stay hidden in both themes and closed routes redirect", async ({ page }) => {
    await expect(page.getByRole("button", { name: "New collection", exact: true })).toBeHidden();
    await expect(page.getByRole("button", { name: "Collection settings", exact: true })).toBeHidden();
    await expect(page.locator('.app-main-nav a[href="#/settings"]')).toBeVisible();
    await expect(page.locator('.app-main-nav a[href="#/logs"]')).toBeVisible();
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    const modal = page.locator(".sl-variants-modal");
    await expect(modal.getByText("Исходные правила доступа", { exact: true })).toHaveCount(0);
    await expect(modal.getByRole("button", { name: "New field", exact: true })).toHaveCount(0);
    await expect(modal.getByRole("button", { name: "Save changes", exact: true })).toHaveCount(0);
    await expect(modal.getByText("Singleton", { exact: true })).toHaveCount(0);
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await expect(modal.getByRole("button", { name: "Опубликовать варианты", exact: true })).toBeVisible();
            expect(await modal.locator(".modal-content").evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
            await page.screenshot({ path: `test-results/schemalock-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    await modal.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(modal).toHaveCount(0);
    for (const path of ["settings/sql", "settings/import-collections"]) {
        await page.evaluate(path => location.hash = "#/" + path, path);
        await expect(page).toHaveURL(/#\/settings$/);
    }
    await page.evaluate(() => app.modals.openCollectionUpsert({}));
    await expect(page.locator(".collection-upsert-modal")).toHaveCount(0);
});

test("settings, crons, export, backups and logs remain accessible", async ({ page }) => {
    const denied = [];
    page.on("response", response => { if (response.status() === 403) denied.push(response.url()); });
    for (const [path, pageName] of [
        ["settings", "pageApplicationSettings"], ["settings/mail", "pageMailSettings"],
        ["settings/storage", "pageStorageSettings"], ["settings/backups", "pageBackupsSettings"],
        ["settings/crons", "pageCronsSettings"], ["settings/export-collections", "pageExportCollections"],
        ["logs", "pageLogs"],
    ]) {
        await page.evaluate(path => location.hash = "#/" + path, path);
        await expect(page.locator(`[data-pb="${pageName}"]`)).toBeVisible();
        await expect(page.locator('.settings-sidebar a[href="#/settings/sql"], .settings-sidebar a[href="#/settings/import-collections"]')).toHaveCount(0);
    }
    await page.evaluate(() => location.hash = "#/settings/export-collections");
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download as JSON", exact: true }).click();
    expect((await download).suggestedFilename()).toMatch(/\.json$/);
    // A real archive verifies that only Restore is hidden; creation and download work.
    await page.evaluate(() => app.pb.backups.create("schemalock-browser.zip"));
    await page.evaluate(() => location.hash = "#/settings/backups");
    const backup = page.locator(".backups-list .list-item").filter({ hasText: "schemalock-browser.zip" });
    await expect(backup).toBeVisible();
    await expect(backup.getByRole("button", { name: "Restore", exact: true })).toBeHidden();
    await expect(backup.getByRole("button", { name: "Download", exact: true })).toBeVisible();
    await expect(backup.getByRole("button", { name: "Delete", exact: true })).toBeVisible();
    await page.evaluate(() => app.pb.backups.delete("schemalock-browser.zip"));
    expect(denied).toEqual([]);
});

test("variants publish through their own editor and cannot override API rules", async ({ page }) => {
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    const modal = page.locator(".sl-variants-modal");
    await modal.locator('[data-pv-section="variant/premium"] > summary').click();
    await modal.getByLabel("Вариант 2", { exact: true }).fill("Premium locked editor");
    let release;
    const gate = new Promise(resolve => release = resolve);
    await page.route("**/api/variants/admin/collections/*", async route => {
        if (route.request().method() === "PUT") await gate;
        await route.continue();
    });
    await modal.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(modal.getByRole("button", { name: "Закрыть", exact: true })).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(modal).toBeVisible();
    release();
    await expect(modal.locator('p[role="status"]')).toContainText("Опубликовано");
    const result = await page.evaluate(async () => {
        const path = "/api/variants/admin/collections/demo_offers";
        const { config } = await app.pb.send(path);
        let status;
        try { await app.pb.send(path, { method: "PUT", body: { ...config, listRule: null } }); }
        catch (error) { status = error.status; }
        return { name: config.variants[1].name, status, after: (await app.pb.send(path)).config.version, version: config.version };
    });
    expect(result.name).toBe("Premium locked editor");
    expect(result.status).toBe(403);
    expect(result.after).toBe(result.version);
    await modal.getByRole("button", { name: "Закрыть", exact: true }).click();
    await expect(modal).toHaveCount(0);
});

test("system records open read-only while superusers retain CRUD", async ({ page }) => {
    await page.evaluate(() => location.hash = "#/collections?collection=pv_sets");
    await expect(page.getByRole("button", { name: "New record", exact: true })).toBeHidden();
    await expect(page.getByRole("button", { name: "Variants", exact: true })).toBeHidden();
    const status = await page.evaluate(async () => {
        const collection = app.store.collections.find(c => c.name === "pv_sets");
        const { items } = await app.pb.collection(collection.id).getList(1, 1);
        app.modals.openRecordUpsert(collection, items[0]);
        try { await app.pb.collection(collection.id).update(items[0].id, { name: "direct edit" }); }
        catch (error) { return error.status; }
    });
    expect(status).toBe(403);
    await expect(page.locator(".record-preview-modal")).toBeVisible();
    await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
    await page.keyboard.press("Escape");
    await page.evaluate(() => location.hash = "#/collections?collection=_superusers");
    await expect(page.getByRole("button", { name: "New record", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "New record", exact: true }).click();
    await expect(page.locator(".record-upsert-modal")).toBeVisible();
    await expect(page.getByRole("button", { name: "Create", exact: true })).toBeVisible();
});

test("record editing, relation picker and singleton survive the lock", async ({ page }) => {
    await page.evaluate(() => location.hash = "#/collections?collection=comments");
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await page.locator('textarea[name="text"]').fill("Schema Lock browser CRUD");
    await page.getByRole("button", { name: "Open records picker", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "PocketBase plugins" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
    await expect(page.getByRole("cell", { name: "Schema Lock browser CRUD", exact: true })).toBeVisible();
    await page.evaluate(() => location.hash = "#/collections?collection=demo_homepage");
    await expect(page.locator(".ps-record-form")).toBeVisible();
    await page.locator('.ps-record-form [name="title"]').fill("Singleton under Schema Lock");
    await page.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(page.locator('.ps-record-form [role="status"]').filter({ hasText: "Сохранено." })).toBeVisible();
});

test("variants can be enabled on an existing collection without schema API writes", async ({ page }) => {
    await page.evaluate(() => location.hash = "#/collections?collection=articles");
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    const modal = page.locator(".sl-variants-modal");
    await modal.getByText("Variables — варианты контента", { exact: true }).click();
    await modal.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(modal.locator('p[role="status"]')).toContainText("Опубликовано");
    expect(await page.evaluate(async () => (await app.pb.collections.getOne("articles")).fields.some(f => f.name === "content_set"))).toBe(true);
});
