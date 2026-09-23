import { test, expect } from "@playwright/test";

test("currency rates collection is available in both themes", async ({ page }) => {
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
        await expect(page.locator('.collections-sidebar').getByText("currency_rates", { exact: true })).toBeVisible();
        await expect(page.locator('[data-pb="pageCollections"]')).toBeVisible();
        await page.screenshot({ path: `test-results/currencyrates-${theme}-${test.info().project.name}.png`, fullPage: true, animations: "disabled" });
    }
    expect(errors).toEqual([]);
});
