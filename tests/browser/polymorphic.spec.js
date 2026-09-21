import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        await app.store.loadCollections();
        location.hash = "#/collections?collection=democomments001";
    });
});

test("native relation editor and both themes", async ({ page }) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await expect(page.getByRole("button", { name: "New record", exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await expect(page.getByText("Open records picker", { exact: true })).toBeVisible();
    await expect(page.locator('[name^="pmr_"]')).toHaveCount(0);
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => { app.store.userColorScheme = theme; }, theme);
        await expect(page.locator(".record-field-input.field-type-relation")).toBeVisible();
        await page.screenshot({ path: `test-results/polymorphic-${theme}.png`, fullPage: true, animations: "disabled" });
    }
    expect(errors).toEqual([]);
});

test("select, switch, save and clear the parent using native dialogs", async ({ page }) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await page.locator('textarea[name="text"]').fill("Browser relation test");
    await page.getByRole("button", { name: "Open records picker", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "PocketBase plugins" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    const field = page.locator('.record-field-input').filter({ has: page.locator('output[name="subject"]') });
    await expect(field).toContainText("PocketBase plugins");
    await field.locator(".selected-container").click();
    await page.locator(".select-option:visible").filter({ hasText: /^videos$/ }).click();
    await expect(field.getByRole("button", { name: "Remove relation" })).toHaveCount(0);
    await page.getByRole("button", { name: "Open records picker", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "Polymorphic relations demo" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    await expect(field).toContainText("Polymorphic relations demo");
    for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 800 });
        for (const theme of ["light", "dark"]) {
            await page.evaluate(theme => { app.store.userColorScheme = theme; }, theme);
            const selected = field.locator(".list > .list-item");
            await expect(selected).toBeVisible();
            await expect(selected.locator(".record-summary").first()).toBeVisible();
            const content = await selected.locator(".content").boundingBox();
            const remove = await field.getByRole("button", { name: "Remove relation" }).boundingBox();
            const bounds = await field.boundingBox();
            // The clear action stays beside the preview, inside the field.
            expect(Math.abs(content.y + content.height / 2 - remove.y - remove.height / 2)).toBeLessThan(2);
            expect(remove.x).toBeGreaterThanOrEqual(content.x + content.width);
            expect(remove.x + remove.width).toBeLessThanOrEqual(bounds.x + bounds.width);
            expect(await field.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
            await field.screenshot({ path: `test-results/selected-relation-${theme}-${width}.png`, animations: "disabled" });
        }
    }
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.locator('textarea[name="text"]')).toHaveCount(0);
    const record = await page.evaluate(async () => app.pb.collection("comments").getFirstListItem('text="Browser relation test"'));
    expect(record.subject.collectionId).toBe("demovideos00001");
    expect(Object.keys(record).filter(key => key.startsWith("pmr_"))).toEqual([]);
    await page.evaluate(async record => {
        app.modals.openRecordUpsert(app.store.collections.find(c => c.name === "comments"), record);
    }, record);
    await page.getByRole("button", { name: "Remove relation" }).click();
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(page.locator('textarea[name="text"]')).toHaveCount(0);
    const cleared = await page.evaluate(id => app.pb.collection("comments").getOne(id), record.id);
    expect(cleared.subject).toBeNull();
    expect(errors).toEqual([]);
});

test("create a polymorphic field from collection settings", async ({ page }) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "New field", exact: true }).click();
    await page.getByRole("button", { name: "Polymorphic relation", exact: true }).click();
    const settings = page.locator(".record-field-settings.field-type-polymorphicRelation").last();
    await settings.locator('input[placeholder="Field name*"]').fill("related");
    await settings.locator(".header-select .selected-container").click();
    await page.locator(".select-option:visible").filter({ hasText: /^articles$/ }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^videos$/ }).click();
    await page.keyboard.press("Escape");
    await settings.getByRole("button", { name: "Field options", exact: true }).click();
    await settings.getByLabel("When parent is deleted", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Clear relation$/ }).click();
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(settings).toHaveCount(0);
    const schema = await page.evaluate(() => app.pb.collections.getOne("comments"));
    const field = schema.fields.find(f => f.name === "related");
    expect(field.type).toBe("polymorphicRelation");
    expect(field.collectionIds).toHaveLength(2);
    expect(field.onDelete).toBe("setNull");
    expect(Object.keys(field.relations)).toHaveLength(2);
    expect(schema.fields.filter(f => f.name.startsWith("pmr_"))).toEqual([]);
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await expect(page.getByRole("button", { name: "Open records picker", exact: true })).toHaveCount(2);
    expect(errors).toEqual([]);
});

test("choosing a parent clears the inline validation error", async ({ page }) => {
    await page.evaluate(async () => {
        const collection = await app.pb.collections.getOne("comments");
        collection.fields.find(f => f.name === "subject").required = true;
        await app.pb.collections.update(collection.id, collection);
        await app.store.loadCollections();
    });
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    await page.locator('textarea[name="text"]').fill("Required relation test");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const error = page.locator('.generated-error[data-input-name="subject"]');
    await expect(error).toBeVisible();
    const field = page.locator('.record-field-input').filter({ has: page.locator('output[name="subject"]') });
    await field.getByRole("button", { name: "Open records picker", exact: true }).click();
    await page.locator(".records-picker-list .list-item").filter({ hasText: "PocketBase plugins" }).click();
    await page.getByRole("button", { name: "Set selection", exact: true }).click();
    await expect(error).toHaveCount(0);
});

test("schema filtering does not modify record JSON data", async ({ page }) => {
    const result = await page.evaluate(async () => {
        const collection = await app.pb.collections.create({
            name: "schema_documents", type: "base", fields: [{ name: "fields", type: "json" }],
        });
        const fields = [
            { name: "subject", type: "polymorphicRelation", relations: { articles: "internal" } },
            { name: "internal", type: "text" },
        ];
        const record = await app.pb.collection(collection.id).create({ fields });
        const loaded = await app.pb.collection(collection.id).getOne(record.id);
        return { expected: fields, created: record.fields, loaded: loaded.fields };
    });
    expect(result.created).toEqual(result.expected);
    expect(result.loaded).toEqual(result.expected);
});
