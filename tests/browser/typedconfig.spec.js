import { test, expect } from "@playwright/test";

test("typed configurations use cards, nested forms and native reference pickers without JSON", async ({ page }, testInfo) => {
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.goto("/_/");
    await page.waitForFunction(() => window.app?.store?._ready);
    const data = await page.evaluate(async () => {
        await app.pb.collection("_superusers").authWithPassword("browser@example.test", "browser-test-password-123");
        const c = await app.pb.collections.getOne("demo_screen_configs");
        const record = await app.pb.collection(c.id).create({ title: "Тест редактора", items: [] });
        const offer = await app.pb.collection("partner_links").create({ name: "Предложение для редактора", provider: "demo", link: { url: "https://example.test/offer" }, active: true });
        await app.store.loadCollections();
        location.hash = `#/collections?collection=${c.id}`;
        return { cid: c.id, id: record.id, offerName: offer.name, offerID: offer.id };
    });
    await page.getByRole("cell", { name: "Тест редактора", exact: true }).click();
    const editor = page.locator(".tc-editor");
    await expect(editor).toBeVisible();
    await expect(editor.locator("textarea, .cm-editor, .json-editor")).toHaveCount(0);
    await expect(editor.getByText("Здесь пока нет блоков.", { exact: false })).toBeVisible();
    await editor.getByRole("button", { name: "Добавить блок", exact: true }).click();
    const heading = editor.locator(".tc-card").first();
    await heading.getByLabel("Текст заголовка", { exact: false }).pressSequentially("Моя витрина");
    await expect(heading.getByLabel("Текст заголовка", { exact: false })).toHaveValue("Моя витрина");
    await heading.locator(".tc-enable label").click();
    await expect(heading.getByLabel("Использовать: Оформление", { exact: true })).toBeChecked();
    await heading.locator(".tc-toggle:not(.tc-enable) label").click();
    await expect(heading.getByLabel("Компактный вид", { exact: false })).toBeChecked();
    await expect(heading.getByRole("group", { name: "Оформление", exact: true })).toBeVisible();
    await editor.getByLabel("Тип нового блока", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Карточка оффера$/ }).click();
    await editor.getByRole("button", { name: "Добавить блок", exact: true }).click();
    const offerCard = editor.locator(".tc-card").last();
    await offerCard.getByRole("button", { name: "Выбрать запись", exact: true }).click();
    await page.locator(".records-picker-modal").getByText(data.offerName, { exact: true }).click();
    await page.locator(".records-picker-modal").getByRole("button", { name: "Set selection", exact: true }).click();
    await expect(page.locator(".records-picker-modal")).toHaveCount(0);
    await expect(offerCard).toContainText(data.offerName);
    await offerCard.locator(".tc-enable label").click();
    await expect(offerCard.getByLabel("Использовать: Метка карточки", { exact: true })).toBeChecked();
    await offerCard.getByLabel("Метка карточки", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Популярное$/ }).click();
    await editor.getByLabel("Тип нового блока", { exact: true }).click();
    await page.locator(".select-option:visible").filter({ hasText: /^Вопросы и ответы$/ }).click();
    await editor.getByRole("button", { name: "Добавить блок", exact: true }).click();
    const faq = editor.locator(".tc-card").last();
    await faq.getByRole("button", { name: "Добавить элемент", exact: true }).click();
    await faq.getByRole("textbox", { name: /^Вопрос/ }).fill("Как это работает?");
    await faq.getByLabel("Ответ", { exact: false }).fill("Сравните условия");
    await faq.getByRole("button", { name: "Выше", exact: true }).click();
    await expect(editor.locator(".tc-card > summary strong")).toHaveText(["Заголовок", "Вопросы и ответы", "Карточка оффера"]);
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        for (const width of [1280, 390]) {
            await page.setViewportSize({ width, height: 1000 });
            await editor.scrollIntoViewIfNeeded();
            expect(await editor.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
            // A checkbox must never turn its containing group into a horizontal
            // checkbox field or hide nested text/select inputs (PocketBase :has).
            const layoutProblems = await editor.evaluate(el => {
                const problems = [];
                const rect = node => node.getBoundingClientRect();
                for (const property of el.querySelectorAll(".tc-property")) {
                    const header = property.querySelector(":scope > .tc-property-header");
                    const content = property.querySelector(":scope > .tc-property-content");
                    if (header && content && rect(content).top < rect(header).bottom + 6)
                        problems.push(`${property.dataset.property}: header overlaps content`);
                }
                for (const input of el.querySelectorAll('.tc-input > input, .tc-input .select')) {
                    if (getComputedStyle(input).opacity === "0" || rect(input).width < 100)
                        problems.push("Hidden or collapsed input");
                }
                for (const label of el.querySelectorAll(".tc-input label")) {
                    if (getComputedStyle(label, "::before").content !== "none") problems.push("Stray checkbox on input label");
                }
                for (const toggle of el.querySelectorAll(".tc-toggle")) {
                    const label = toggle.querySelector("label");
                    const bounds = rect(label), parent = rect(toggle);
                    if (bounds.left < parent.left || bounds.right > parent.right + 1 || bounds.height < 24)
                        problems.push("Toggle label escapes its row");
                }
                return problems;
            });
            expect(layoutProblems).toEqual([]);
            for (const [index, card] of (await editor.locator(".tc-card").all()).entries()) {
                await card.screenshot({ path: `test-results/typedconfig-${testInfo.project.name}-${theme}-${width}-${index}.png`, animations: "disabled" });
            }
        }
    }
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await expect(page.locator(".record-upsert-modal")).toHaveCount(0);
    const saved = await page.evaluate(async data => app.pb.collection(data.cid).getOne(data.id), data);
    expect(saved.items.map(i => i.type)).toEqual(["heading", "faq", "offerCard"]);
    expect(saved.items[0].data.style.compact).toBe(true);
    expect(saved.items[1].data.entries[0].question).toBe("Как это работает?");
    expect(saved.items[2].data.offer.id).toBe(data.offerID);
    expect(saved.items[2].data.badge).toBe("Популярное");
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.getByRole("cell", { name: "Тест редактора", exact: true }).click();
    await expect(editor.locator(".tc-card")).toHaveCount(3);
    for (const theme of ["light", "dark"]) {
        await page.evaluate(theme => app.store.userColorScheme = theme, theme);
        await page.screenshot({ path: `test-results/typedconfig-overview-${testInfo.project.name}-${theme}.png`, fullPage: true, animations: "disabled" });
    }
    await page.locator(".record-upsert-modal").getByRole("button", { name: "Close", exact: true }).click();
    const afterDelete = await page.evaluate(async data => {
        await app.pb.collection("partner_links").delete(data.offerID);
        return app.pb.collection(data.cid).getOne(data.id);
    }, data);
    expect(afterDelete.items.map(i => i.type)).toEqual(["heading", "faq"]);
    expect(errors).toEqual([]);
});
