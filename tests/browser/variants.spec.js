import { test, expect } from "@playwright/test";

test("records inherit the page preset and retain their set when edited", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const cid = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.create({ name: "preset_creation_offers", type: "base", fields: [{ name: "title", type: "text" }], listRule: "", viewRule: "" });
        const { config } = await app.pb.send("/api/variants/admin/collections/demo_offers");
        await app.pb.send(`/api/variants/admin/collections/${c.id}`, { method: "PUT", body: { ...config, collection: c.id, version: 0 } });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${c.id}`;
        return c.id;
    });
    const records = [];
    for (const variant of ["default", "Premium"]) {
        await page.getByLabel("Вариант", { exact: true }).click();
        await page.locator(".select-option:visible").filter({ hasText: new RegExp(`^${variant}$`) }).click();
        await page.getByRole("button", { name: "New record", exact: true }).first().click();
        await expect(page.getByLabel("Набор контента", { exact: true })).toHaveCount(0);
        await expect(page.locator(".pv-content-set-field").locator("..")).toBeHidden();
        await page.locator('[name="title"]').fill(`Created in ${variant}`);
        await page.getByRole("button", { name: "Create", exact: true }).click();
        await expect(page.getByRole("cell", { name: `Created in ${variant}`, exact: true })).toBeVisible();
        records.push(await page.evaluate(async ({ cid, variant }) => {
            const record = await app.pb.collection(cid).getFirstListItem(app.pb.filter("title = {:title}", { title: `Created in ${variant}` }));
            return { record, set: await app.pb.collection("pv_sets").getOne(record.content_set) };
        }, { cid, variant }));
    }
    expect(records.map(r => r.set.variant)).toEqual(["default", "premium"]);
    expect(records.map(r => r.set.experiment)).toEqual(["", ""]);
    // An existing record opened from a relation or direct link must keep its set,
    // even when a different preset is selected behind its modal.
    await page.evaluate(async ({ cid, id }) => {
        app.modals.openRecordUpsert(await app.pb.collections.getOne(cid), id);
    }, { cid, id: records[0].record.id });
    await expect(page.locator('[name="title"]')).toHaveValue("Created in default");
    await expect(page.getByLabel("Набор контента", { exact: true })).toHaveCount(0);
    await page.locator('[name="title"]').fill("Edited default");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
    expect(await page.evaluate(async ({ cid, id }) => (await app.pb.collection(cid).getOne(id)).content_set,
        { cid, id: records[0].record.id })).toBe(records[0].record.content_set);
    // A draft created in default must not move a new row out of the current preset.
    await page.evaluate(({ cid, set }) => localStorage.setItem(`draft_${cid}_`, JSON.stringify({ title: "Restored draft", content_set: set })),
        { cid, set: records[0].record.content_set });
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await page.getByRole("button", { name: "Restore draft", exact: true }).click();
    await expect(page.locator('[name="title"]')).toHaveValue("Restored draft");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("cell", { name: "Restored draft", exact: true })).toBeVisible();
    expect(await page.evaluate(async cid => (await app.pb.collection(cid).getFirstListItem('title = "Restored draft"')).content_set, cid))
        .toBe(records[1].record.content_set);
});

test("publishing variants preserves unsaved collection schema changes", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const cid = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.create({ name: "draft_offers", type: "base", fields: [{ name: "title", type: "text" }], listRule: "", viewRule: "" });
        const { config } = await app.pb.send("/api/variants/admin/collections/demo_offers");
        await app.pb.send(`/api/variants/admin/collections/${c.id}`, { method: "PUT", body: { ...config, collection: c.id, version: 0 } });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${c.id}`;
        return c.id;
    });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    const name = page.locator('.collection-upsert-modal input[name="name"]');
    await name.fill("draft_offers_renamed");
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    await page.locator('[data-pv-section="variant/premium"] > summary').click();
    await page.getByLabel("Вариант 2", { exact: true }).fill("Draft premium");
    await page.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(page.locator(".pv-editor [role=alert]")).toContainText("Сначала сохраните изменения схемы коллекции");
    await expect(name).toHaveValue("draft_offers_renamed");
    expect(await page.evaluate(async cid => (await app.pb.send(`/api/variants/admin/collections/${cid}`)).config.version, cid)).toBe(1);
    await name.press("Control+s");
    await page.getByRole("button", { name: "Yes, save changes", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save changes", exact: true })).toBeDisabled();
    await expect(page.getByLabel("Вариант 2", { exact: true })).toHaveValue("Draft premium");
    await page.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(page.locator('.pv-editor p[role="status"]')).toHaveText("Опубликовано, версия 2");
    const saved = await page.evaluate(async cid => ({
        schema: await app.pb.collections.getOne(cid),
        config: (await app.pb.send(`/api/variants/admin/collections/${cid}`)).config,
    }), cid);
    expect(saved.schema.name).toBe("draft_offers_renamed");
    expect(saved.config.variants[1].name).toBe("Draft premium");
});

test("variant editor has progressive disclosure and compact condition rows", async ({ page }) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const cid = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.create({ name: "editor_offers", type: "base", fields: [{ name: "title", type: "text" }], listRule: "", viewRule: "" });
        const { config } = await app.pb.send("/api/variants/admin/collections/demo_offers");
        await app.pb.send(`/api/variants/admin/collections/${c.id}`, { method: "PUT", body: { ...config, collection: c.id, version: 0 } });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${c.id}`;
        return c.id;
    });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    const editor = page.locator(".pv-editor");
    const premium = editor.locator('[data-pv-section="variant/premium"]');
    const subscriber = editor.locator('[data-pv-section="variant/subscriber"]');
    await expect(editor.locator(".pv-variant")).toHaveCount(4);
    await expect(editor.locator(".pv-variant[open]")).toHaveCount(0);
    await expect(premium.locator("summary .pv-experiment-mark").first()).toBeVisible();
    await expect(subscriber.locator(".pv-experiment-mark")).toHaveCount(0);
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await page.screenshot({ path: `test-results/variants-overview-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await premium.locator(":scope > summary").click();
    const row = premium.locator(".pv-condition-row");
    await expect(row).toBeVisible();
    const cells = await row.locator(":scope > .field").evaluateAll(els => els.map(el => { const r = el.getBoundingClientRect(); return { x: r.x, y: r.y }; }));
    expect(cells).toHaveLength(3);
    expect(Math.max(...cells.map(c => c.y)) - Math.min(...cells.map(c => c.y))).toBeLessThan(2);
    await premium.getByLabel("Вариант 2", { exact: true }).fill("Premium renamed");
    await premium.locator(":scope > summary").click();
    await premium.locator(":scope > summary").click();
    await expect(premium.getByLabel("Вариант 2", { exact: true })).toHaveValue("Premium renamed");
    await premium.getByRole("button", { name: "Добавить условие", exact: true }).click();
    await premium.getByLabel("Поле", { exact: true }).nth(1).click();
    await page.locator(".select-option:visible").filter({ hasText: /^tier$/ }).click();
    await premium.getByLabel("Значение", { exact: true }).nth(1).click();
    await page.locator(".select-option:visible").filter({ hasText: /^new$/ }).click();
    await premium.getByLabel("Совпадение условий", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Любое условие \(ИЛИ\)$/ }).click();
    await premium.locator(".pv-experiment > summary").click();
    await expect(premium.getByLabel("Название эксперимента", { exact: true })).toBeVisible();
    await subscriber.locator(":scope > summary").click();
    await subscriber.locator('[data-pv-section="subscriber/condition"] > summary').click();
    await subscriber.locator('[data-pv-section="subscriber/condition/related"] > summary').click();
    await expect(subscriber.locator(".pv-condition-row")).toHaveCount(2);
    await subscriber.getByLabel("Значение", { exact: true }).last().click();
    await page.locator(".select-option:visible").filter({ hasText: /^Нет$/ }).click();
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await subscriber.scrollIntoViewIfNeeded();
            expect(await editor.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await page.screenshot({ path: `test-results/variants-conditions-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    await page.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(editor.locator('p[role="status"]')).toHaveText("Опубликовано, версия 2");
    const config = await page.evaluate(async cid => (await app.pb.send(`/api/variants/admin/collections/${cid}`)).config, cid);
    expect(config.variants[1].name).toBe("Premium renamed");
    expect(config.variants[1].condition.kind).toBe("any");
    expect(config.variants[1].condition.children.map(c => c.value)).toEqual(["premium", "new"]);
    expect(config.variants[0].condition.children[0].children[1].value).toBe(false);
    expect(errors).toEqual([]);
});

test("default-only collections show only experiment choices when present", async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const cid = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.create({ name: "default_only_offers", type: "base", fields: [{ name: "title", type: "text" }], listRule: "", viewRule: "" });
        await app.pb.send(`/api/variants/admin/collections/${c.id}`, { method: "PUT", body: {
            collection: c.id, authCollection: "demomembers0001", variables: true, experiments: false,
            default: { key: "default", name: "A custom name must not replace default" }, variants: [],
        } });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${c.id}`;
        return c.id;
    });
    const variant = page.getByLabel("Вариант", { exact: true });
    const group = page.getByLabel("Вариант эксперимента", { exact: true });
    await expect(page.locator(".total-count")).toHaveText("Total: 0");
    await expect(variant).toBeHidden();
    await expect(group).toBeHidden();
    await page.evaluate(async cid => {
        const { config } = await app.pb.send(`/api/variants/admin/collections/${cid}`);
        config.experiments = true;
        config.default.experiments = [{ key: "test", name: "Default test", active: true, groups: [
            { key: "a", name: "A", from: 1, to: 5000 }, { key: "b", name: "B", from: 5001, to: 10000 },
        ] }];
        await app.pb.send(`/api/variants/admin/collections/${cid}`, { method: "PUT", body: config });
        const { sets } = await app.pb.send(`/api/variants/admin/collections/${cid}`);
        for (const set of sets) await app.pb.collection(cid).create({ title: set.group || "base", content_set: set.id });
    }, cid);
    await page.reload();
    await expect(variant).toBeHidden();
    await expect(group).toBeVisible();
    await group.click();
    await expect(page.locator(".select-option:visible")).toHaveText(["default", "A", "B"]);
    await page.locator(".select-option:visible").filter({ hasText: /^B$/ }).click();
    await expect(page.locator(".total-count")).toHaveText("Total: 1");
    await page.reload();
    await expect(variant).toBeHidden();
    await expect(group).toContainText("B");
    await expect(page.locator(".total-count")).toHaveText("Total: 1");
    await group.click();
    await page.locator(".select-option:visible").filter({ hasText: /^default$/ }).click();
    await expect(page.locator(".total-count")).toHaveText("Total: 1");
    await expect(page.getByText("base", { exact: true })).toBeVisible();
});

test("collection presets preserve search and URLs and allow additional API rules", async ({ page, request }) => {
    test.setTimeout(60_000);
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=demo_offers";
    });
    const preset = page.getByLabel("Вариант", { exact: true });
    const group = page.getByLabel("Вариант эксперимента", { exact: true });
    const search = page.locator(".records-searchbar-wrapper .editor-content");
    const total = page.locator(".total-count");
    async function choose(label, selector = preset) {
        await selector.click();
        await page.locator(".select-option:visible").filter({ hasText: label }).click();
    }
    await expect(preset).toBeVisible();
    await expect(preset).toContainText("default");
    await expect(page.getByRole("columnheader", { name: "content_set", exact: true })).toHaveCount(0);
    await expect(total).toHaveText("Total: 2");
    await choose(/^default$/);
    await expect(total).toHaveText("Total: 2");
    await expect(group).toBeHidden();
    await choose(/^Premium$/);
    await expect(group).toContainText("default");
    await choose(/^A — коротко$/, group);
    await expect(total).toHaveText("Total: 2");
    await expect(page.getByText("Premium A: краткое предложение", { exact: true })).toBeVisible();
    await expect(search).toHaveText("");
    await search.fill("position = 1");
    await search.press("Enter");
    await expect(total).toHaveText("Total: 1");
    await choose(/^B — подробно$/, group);
    await expect(page.getByText("Premium B: подробное предложение", { exact: true })).toBeVisible();
    await expect(total).toHaveText("Total: 1");
    await expect(search).toHaveText("position = 1");
    await choose(/^default$/);
    await expect(total).toHaveText("Total: 1");
    await expect(search).toHaveText("position = 1");
    await choose(/^Premium$/);
    await choose(/^B — подробно$/, group);
    await page.reload();
    await expect(preset).toContainText("Premium");
    await expect(group).toContainText("B — подробно");
    await expect(search).toHaveText("position = 1");
    await expect(total).toHaveText("Total: 1");
    await page.getByRole("button", { name: "Clear", exact: true }).click();
    await expect(total).toHaveText("Total: 2");
    await expect(preset).toContainText("Premium");
    await expect(group).toContainText("B — подробно");
    await search.fill("сравнение");
    await search.press("Enter");
    await expect(total).toHaveText("Total: 1");
    await expect(search).toHaveText("сравнение");
    await page.getByRole("button", { name: "Clear", exact: true }).click();
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await expect(preset).toBeVisible();
            expect(await page.locator(".pv-records-presets").evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await preset.click();
            await expect(page.locator(".select-option:visible")).toHaveCount(4);
            await page.screenshot({ path: `test-results/variants-presets-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
            await page.keyboard.press("Escape");
            await group.click();
            await expect(page.locator(".select-option:visible")).toHaveCount(3);
            await page.screenshot({ path: `test-results/variants-groups-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
            await page.keyboard.press("Escape");
        }
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await choose(/^Новички$/);
    await expect(group).toBeHidden();
    await expect(total).toHaveText("Total: 2");
    await expect(page.getByText("Добро пожаловать", { exact: true })).toBeVisible();
    await choose(/^Premium$/);
    await expect(group).toContainText("default");
    await expect(page.getByText("Premium: базовая подборка", { exact: true })).toBeVisible();
    await choose(/^default$/);
    await expect(total).toHaveText("Total: 2");
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "API rules", exact: true }).click();
    const rules = page.locator('.tab-content-wrapper[data-tab="API rules"]');
    const editors = rules.locator(".editor-content");
    await expect(editors).toHaveCount(2);
    await expect(editors.nth(0)).toHaveText("");
    await expect(editors.nth(1)).toHaveText("");
    for (const editor of await editors.all()) await editor.fill('@request.auth.id != ""');
    await page.getByRole("button", { name: "Опубликовать правила", exact: true }).click();
    await expect(rules.getByRole("status")).toHaveText("Опубликовано, версия 2");
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await page.screenshot({ path: `test-results/variants-access-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    const guestList = await request.get("/api/collections/demo_offers/records");
    expect((await guestList.json()).totalItems).toBe(0);
    expect((await request.get("/api/collections/demo_offers/records/demooffer000001")).status()).toBe(404);
    const auth = await request.post("/api/collections/users/auth-with-password", { data: { identity: "premium-a@variants.test", password: "demo-variants-123" } });
    const headers = { Authorization: (await auth.json()).token };
    const memberList = await request.get("/api/collections/demo_offers/records", { headers });
    const memberRows = await memberList.json();
    expect(memberRows.totalItems).toBe(2);
    expect(memberRows.items.map(r => r.title).every(title => title.startsWith("Premium A:"))).toBe(true);
    expect((await request.get("/api/collections/demo_offers/records/demooffer000009", { headers })).status()).toBe(404);
    expect((await request.get("/api/collections/demo_offers/records/demooffer000007", { headers })).status()).toBe(200);
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.getByRole("button", { name: "Variants", exact: true }).click();
    await expect(page.locator('.tab-content-wrapper[data-tab="Variants"] .rule-field .editor-content').first()).toHaveText('@request.auth.id != ""');
    await page.evaluate(() => {
        app.modals.close(null, true);
        app.store.activeCollection = "demo_subscriptions";
    });
    await expect(preset).toBeHidden();
    await expect(total).toHaveText("Total: 3");
    expect(errors).toEqual([]);
});

test("variants use native settings, publish, preview and edit sets in both themes", async ({ page }) => {
    test.setTimeout(90_000);
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const fixture = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const auth = await app.pb.collections.create({ name: "variant_members", type: "auth", fields: [{ name: "premium", type: "bool" }] });
        const collection = await app.pb.collections.create({ name: "variant_offers", type: "base", fields: [{ name: "title", type: "text" }], listRule: "", viewRule: "" });
        await app.pb.collection(collection.id).create({ title: "Baseline offer" });
        const user = await app.pb.collection(auth.id).create({ email: "variants@example.test", password: "variants-password-123", passwordConfirm: "variants-password-123", premium: true });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${collection.id}`;
        return { auth, collection, user };
    });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByText("Variants", { exact: true }).click();
    await expect(page.getByText("Variables — варианты контента", { exact: true })).toBeVisible();
    await page.getByLabel("Auth-коллекция", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^variant_members$/ }).click();
    await page.getByText("Variables — варианты контента", { exact: true }).click();
    await page.getByText("Experiments — распределение по бакетам", { exact: true }).click();
    await page.getByRole("button", { name: "Добавить вариант", exact: true }).click();
    await page.getByLabel("Вариант 1", { exact: true }).fill("Premium");
    await page.getByLabel("Поле", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^premium$/ }).click();
    await page.getByRole("button", { name: "Добавить эксперимент", exact: true }).last().click();
    await page.getByLabel("Название эксперимента", { exact: true }).fill("Offer test");
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 900 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => app.store.userColorScheme = theme, theme);
            await expect(page.getByLabel("Вариант 1", { exact: true })).toBeVisible();
            const tab = page.locator(".collection-tab-content").filter({ has: page.getByLabel("Вариант 1", { exact: true }) });
            expect(await tab.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            await page.screenshot({ path: `test-results/variants-${theme}-${width}.png`, fullPage: true, animations: "disabled" });
        }
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.getByLabel("Бакет от", { exact: true }).nth(1).fill("5000");
    await page.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "overlapping bucket ranges" })).toBeVisible();
    await page.getByLabel("Бакет от", { exact: true }).nth(1).fill("5001");
    await page.getByRole("button", { name: "Опубликовать варианты", exact: true }).click();
    await expect(page.locator('.tab-content-wrapper[data-tab="Variants"] p[role="status"]')).toHaveText("Опубликовано, версия 1");
    await page.locator('[data-pv-section="preview"] > summary').click();
    await page.getByRole("button", { name: "Выбрать пользователя", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "variants@example.test" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    await expect(page.getByText(/История: 0 назначений/)).toBeVisible();
    await page.locator('[data-pv-section="sets"] > summary').click();
    await page.getByRole("button", { name: "Открыть записи набора", exact: true }).first().click();
    await expect(page.getByRole("button", { name: "New record", exact: true }).first()).toBeVisible();
    await page.getByLabel("Вариант", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Premium$/ }).click();
    await page.getByLabel("Вариант эксперимента", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^B$/ }).click();
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await expect(page.getByLabel("Набор контента", { exact: true })).toHaveCount(0);
    await page.locator('[name="title"]').fill("Experiment offer");
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: `test-results/variants-record-auto-set-${theme}.png`, fullPage: true, animations: "disabled" });
    }
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const result = await page.evaluate(async fixture => {
        const r = await app.pb.collection(fixture.collection.id).getFirstListItem('title = "Experiment offer"');
        const set = await app.pb.collection("pv_sets").getOne(r.content_set);
        return { set, history: await app.pb.send(`/api/variants/admin/users/${fixture.auth.id}/${fixture.user.id}/history`) };
    }, fixture);
    expect(result.set.group).toBe("b");
    expect(result.history.totalItems).toBe(0);
    expect(errors).toEqual([]);
});
