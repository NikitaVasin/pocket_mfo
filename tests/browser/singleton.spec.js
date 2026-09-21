import { test, expect } from "@playwright/test";

async function login(page) {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
    });
}

test("singleton is configured through the UI and edits inline", async ({ page }) => {
    const errors = []; page.on("pageerror", err => errors.push(err.message));
    await login(page);
    const cid = await page.evaluate(async () => {
        const c = await app.pb.collections.create({ name: "singleton_ui", type: "base", fields: [{ name: "title", type: "text", required: true }, { name: "attachment", type: "file", maxSelect: 1 }, { name: "article", type: "relation", collectionId: "demoarticles001", maxSelect: 1 }] });
        await app.store.loadCollections(); location.hash = `#/collections?collection=${c.id}`; return c.id;
    });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "Singleton", exact: true }).click();
    await page.getByLabel("Одна запись на коллекцию", { exact: true }).check();
    await page.getByRole("button", { name: "Применить", exact: true }).click();
    await expect(page.locator(".ps-settings [role=status]")).toHaveText("Настройка Singleton применена.");
    await page.getByRole("button", { name: "Close", exact: true }).click();
    const form = page.locator(".ps-record-form");
    await expect(form).toBeVisible();
    await expect(page.getByRole("button", { name: "New record", exact: true })).toBeHidden();
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form.locator('textarea[name="title"]:invalid')).toBeVisible();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Singleton value");
    await form.locator('input[type="file"]').setInputFiles({ name: "example.txt", mimeType: "text/plain", buffer: Buffer.from("singleton attachment") });
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form.getByRole("status").filter({ hasText: /^Сохранено\.$/ })).toBeVisible();
    const rows = await page.evaluate(cid => app.pb.collection(cid).getFullList(), cid);
    expect(rows).toHaveLength(1); expect(rows[0].title).toBe("Singleton value"); expect(rows[0].attachment).toBeTruthy();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Changed");
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form.getByRole("status").filter({ hasText: /^Сохранено\.$/ })).toBeVisible();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Unsaved");
    page.once("dialog", d => d.dismiss());
    await page.locator('[data-collection-id="demoarticles001"]').click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Unsaved");
    page.once("dialog", d => d.accept());
    await form.getByRole("button", { name: "Сбросить", exact: true }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Changed");
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            expect(await form.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await page.screenshot({ path: `test-results/singleton-${theme}-${width}.png`, fullPage: true });
        }
    }
    page.once("dialog", d => d.accept());
    await form.getByRole("button", { name: "Удалить", exact: true }).click();
    await expect(form).toContainText("Записи пока нет");
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "Singleton", exact: true }).click();
    await page.getByLabel("Одна запись на коллекцию", { exact: true }).uncheck();
    await page.getByRole("button", { name: "Применить", exact: true }).click();
    await expect(page.locator(".ps-settings [role=status]")).toHaveText("Настройка Singleton применена.");
    await page.getByRole("button", { name: "Close", exact: true }).click();
    await expect(form).toHaveCount(0);
    await expect(page.getByRole("button", { name: "New record", exact: true })).toBeVisible();
    expect(errors).toEqual([]);
});

test("singleton variants select independent records and preserve cancelled edits", async ({ page }) => {
    await login(page);
    const cid = await page.evaluate(async () => {
        const c = await app.pb.collections.create({ name: "singleton_variants", type: "base", fields: [{ name: "title", type: "text", required: true }] });
        const { config } = await app.pb.send("/api/variants/admin/collections/demo_offers");
        await app.pb.send(`/api/variants/admin/collections/${c.id}`, { method: "PUT", body: { ...config, collection: c.id, version: 0 } });
        await app.pb.send(`/api/singleton/admin/collections/${c.id}`, { method: "PUT", body: { enabled: true } });
        await app.store.loadCollections(); location.hash = `#/collections?collection=${c.id}`; return c.id;
    });
    const form = page.locator(".ps-record-form");
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toBeVisible();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Default content");
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form).toContainText("Сохранено.");
    await page.getByLabel("Вариант", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Premium$/ }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("");
    await page.getByLabel("Вариант эксперимента", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^A — коротко$/ }).click();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Group A");
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form).toContainText("Сохранено.");
    const groupURL = page.url();
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Unsaved A");
    await page.getByLabel("Вариант", { exact: true }).click();
    page.once("dialog", d => d.dismiss());
    await page.locator(".select-option:visible").filter({ hasText: /^default$/ }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Unsaved A");
    expect(page.url()).toBe(groupURL);
    await page.getByLabel("Вариант", { exact: true }).click();
    page.once("dialog", d => d.accept());
    await page.locator(".select-option:visible").filter({ hasText: /^default$/ }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Default content");
    await page.goto(groupURL);
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Group A");
    await page.goBack();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Default content");
    await page.goForward();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Group A");
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Keep on failed save");
    await page.route(`**/api/collections/${cid}/records/*`, route => route.request().method() === "PATCH"
        ? route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ status: 400, message: "Test save failure", data: { title: { code: "invalid", message: "Test field error" } } }) })
        : route.continue());
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form.locator("[role=alert]")).toContainText("Test save failure");
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Keep on failed save");
    await expect(form).toContainText("Test field error");
    page.once("dialog", d => d.dismiss());
    await page.goBack();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Keep on failed save");
    await page.unroute(`**/api/collections/${cid}/records/*`);
    page.once("dialog", d => d.accept());
    await form.getByRole("button", { name: "Сбросить", exact: true }).click();
    const rows = await page.evaluate(cid => app.pb.collection(cid).getFullList(), cid);
    expect(rows).toHaveLength(2); expect(new Set(rows.map(r => r.content_set)).size).toBe(2);
    const a = rows.find(r => r.title === "Group A");
    await page.goto(`/_/#/collections?collection=${cid}&record=${a.id}`);
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Group A");
    await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
    await page.route(`**/api/collections/${cid}/records?**`, route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ status: 503, message: "Test load failure" }) }));
    await page.getByLabel("Вариант", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^default$/ }).click();
    await expect(form.locator("[role=alert]")).toContainText("Test load failure");
    await expect(form.getByRole("button", { name: "Сохранить", exact: true })).toBeDisabled();
    await expect(form.getByRole("button", { name: "Удалить", exact: true })).toBeDisabled();
    await page.unroute(`**/api/collections/${cid}/records?**`);
    await form.getByRole("button", { name: "Обновить форму", exact: true }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Default content");
    await page.goto(groupURL);
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Group A");
    await page.evaluate(async cid => {
        const { config } = await app.pb.send(`/api/variants/admin/collections/${cid}`);
        config.variants = config.variants.filter(v => v.key !== "premium");
        await app.pb.send(`/api/variants/admin/collections/${cid}`, { method: "PUT", body: config });
    }, cid);
    await page.reload();
    await expect(page.getByLabel("Вариант", { exact: true })).toContainText("premium (архив)");
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Group A");
});

test("singleton supports polymorphic fields and keeps relation pickers native", async ({ page }) => {
    await login(page);
    const cid = await page.evaluate(async () => {
        const c = await app.pb.collections.create({ name: "singleton_relations", type: "base", fields: [
            { name: "title", type: "text", required: true },
            { name: "subject", type: "polymorphicRelation", collectionIds: ["demoarticles001", "demovideos00001"], onDelete: "restrict" },
            { name: "homepage", type: "relation", collectionId: "demosingleton01", maxSelect: 1 },
        ] });
        await app.pb.send(`/api/singleton/admin/collections/${c.id}`, { method: "PUT", body: { enabled: true } });
        await app.store.loadCollections(); location.hash = `#/collections?collection=${c.id}`; return c.id;
    });
    const form = page.locator(".ps-record-form");
    await form.getByRole('textbox', { name: 'title *', exact: true }).fill("Relations");
    const subject = form.locator(".record-field-input").filter({ has: page.locator('output[name="subject"]') });
    await subject.getByRole("button", { name: "Open records picker", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "PocketBase plugins" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    const homepage = form.locator(".record-field-input").filter({ has: page.locator('output[name="homepage"]') });
    await homepage.getByRole("button", { name: "Open records picker", exact: true }).click();
    await expect(page.locator(".records-picker-list > .list-item.handle")).toHaveCount(6);
    await page.locator(".records-picker-list > .list-item.handle").first().getByRole("button", { name: "Edit", exact: true }).click();
    await expect(page.locator(".record-upsert-modal")).toBeVisible();
    await page.locator(".record-upsert-modal").getByRole("button", { name: "Close", exact: true }).click();
    await expect(form.getByRole('textbox', { name: 'title *', exact: true })).toHaveValue("Relations");
    await page.locator(".records-picker-list > .list-item.handle").first().click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form).toContainText("Сохранено.");
    const saved = await page.evaluate(cid => app.pb.collection(cid).getFirstListItem("", { expand: "subject,homepage" }), cid);
    expect(saved.subject.collectionId).toBe("demoarticles001"); expect(saved.homepage).toBeTruthy();
    expect(saved.expand.subject.title).toBe("PocketBase plugins");
    await subject.getByRole("button", { name: "Remove relation", exact: true }).click();
    await form.getByRole("button", { name: "Сохранить", exact: true }).click();
    await expect(form).toContainText("Сохранено.");
    expect(await page.evaluate(cid => app.pb.collection(cid).getFirstListItem(""), cid)).toMatchObject({ subject: null });
});

test("singleton settings report conflicting content sets without deleting records", async ({ page }) => {
    await login(page);
    await page.evaluate(() => location.hash = "#/collections?collection=demooffers00001");
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "Singleton", exact: true }).click();
    await page.getByLabel("Одна запись на коллекцию", { exact: true }).check();
    await page.getByRole("button", { name: "Применить", exact: true }).click();
    await expect(page.locator(".ps-settings [role=alert]")).toContainText("больше одной записи");
    await expect(page.locator(".ps-settings li")).toHaveCount(6);
    expect(await page.evaluate(async () => (await app.pb.collection("demo_offers").getList(1, 1)).totalItems)).toBe(12);
});
