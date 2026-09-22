import { test, expect } from "@playwright/test";

test("dynamic link can be added to another collection and cleared", async ({ page }) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.create({ name: "link_banners", type: "base", fields: [] });
        await app.store.loadCollections();
        location.hash = "#/collections?collection=" + c.id;
    });
    await page.getByRole("button", { name: "Collection settings", exact: true }).click();
    await page.getByRole("button", { name: "New field", exact: true }).click();
    await page.getByRole("button", { name: "Dynamic link", exact: true }).click();
    const settings = page.locator('.record-field-settings.field-type-dynamicLink');
    await settings.locator('input[placeholder="Field name*"]').fill("destination");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(settings).toHaveCount(0);
    const schema = await page.evaluate(() => app.pb.collections.getOne("link_banners"));
    expect(schema.fields.find(f => f.name === "destination").type).toBe("dynamicLink");
    await page.getByRole("button", { name: "New record", exact: true }).first().click();
    const field = page.locator('.dl-input');
    await field.getByLabel("URL", { exact: true }).pressSequentially("https://example.com/banner");
    await expect(field.getByLabel("URL", { exact: true })).toHaveValue("https://example.com/banner");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(field).toHaveCount(0);
    const record = await page.evaluate(async () => (await app.pb.collection("link_banners").getList(1, 1)).items[0]);
    expect(record.destination).toMatchObject({ url: "https://example.com/banner", mode: "appView", saveCooke: true, showLoader: true });
    await page.evaluate(record => app.modals.openRecordUpsert(app.store.collections.find(c => c.name === "link_banners"), record), record);
    await field.getByRole("button", { name: "Очистить ссылку", exact: true }).click();
    await expect(field.getByLabel("URL", { exact: true })).toHaveValue("");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(field).toHaveCount(0);
    const cleared = await page.evaluate(id => app.pb.collection("link_banners").getOne(id), record.id);
    expect(cleared.destination).toBeNull();
    expect(errors).toEqual([]);
});
