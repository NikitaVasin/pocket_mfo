document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/mcp/style.css?v=2" }));
app.store.headerLinks = [...app.store.headerLinks, { label: "MCP", href: "#/mcp", icon: "ri-key-2-line" }];
app.routes.superuserOnly("#/mcp", () => t.div({ className: "page mcp-shell" }, mcpPage()));
function mcpPage() {
    const control = (props) => t.div({ className: "field" }, t.input(props));
    app.store.title = "MCP";
    const s = store({ keys: [], tools: [], collections: [], selected: [], selectedCollections: [], preset: "", name: "", expires: "", token: "", copied: false, error: "", busy: false, loaded: false });
    async function load() {
        s.error = "";
        try { const r = await app.pb.send("/api/mcp/admin/keys", { requestKey: null }); s.keys = r.items; s.tools = r.tools; s.collections = r.collections; s.loaded = true; }
        catch (e) { s.error = e?.response?.message || "Не удалось загрузить ключи"; }
    }
    async function run(fn) { if (s.busy) return; s.busy = true; s.error = ""; try { await fn(); } catch (e) { s.error = e?.response?.message || e.message; app.toasts.error(s.error); } finally { s.busy = false; } }
    function preset(mode) {
        s.selected = s.tools.filter(tool => mode === "full" || tool.readOnly).map(tool => tool.name);
        s.selectedCollections = [...s.collections];
        s.preset = mode;
    }
    function choices(items, key, label) { return t.div({ className: "mcp-choices" }, ...items.map(item => t.label({ className: "mcp-check" }, t.input({ type: "checkbox", checked: () => s[key].includes(item.name || item), onchange: e => { const value = item.name || item; s[key] = e.target.checked ? [...s[key], value] : s[key].filter(v => v !== value); s.preset = ""; } }), t.span(null, label(item))))); }
    load();
    return t.div({ className: "mcp-page" },
        t.header(null, t.h1(null, "MCP"), t.p(null, "Выдайте помощнику доступ к нужным инструментам и коллекциям.")),
        t.p(null, "Адрес подключения: ", t.code(null, location.origin + "/api/mcp")),
        () => s.error ? t.div({ role: "alert", className: "alert alert-danger" }, s.error) : null,
        () => !s.loaded ? t.div(null, t.p(null, s.error ? "Данные не загружены." : "Загрузка…"), s.error ? t.button({ type: "button", className: "btn secondary", onclick: load }, "Повторить загрузку") : null) : null,
        t.section(null, t.h2(null, "Новый ключ"),
            t.form({ onsubmit: e => { e.preventDefault(); run(async () => { const r = await app.pb.send("/api/mcp/admin/keys", { method: "POST", body: { name: s.name, tools: s.selected, collections: s.selectedCollections, expiresAt: s.expires ? new Date(s.expires).toISOString() : "" }, requestKey: null }); s.token = r.token; s.copied = false; s.name = ""; await load(); }); } },
                t.div({ className: "mcp-grid" },
                    t.label(null, "Название ключа", control({ required: true, maxLength: 100, value: () => s.name, oninput: e => s.name = e.target.value })),
                    t.label(null, "Действует до", control({ type: "datetime-local", value: () => s.expires, oninput: e => s.expires = e.target.value }))),
                t.h3(null, "Пресет доступа"),
                t.div({ className: "mcp-presets", role: "group", ariaLabel: "Пресеты доступа" },
                    ...[["read", "Только чтение"], ["full", "Полный доступ"]].map(([mode, label]) => t.button({ type: "button", className: () => `btn ${s.preset === mode ? "" : "secondary"}`, ariaPressed: () => String(s.preset === mode), disabled: () => !s.loaded || s.busy, onclick: () => preset(mode) }, label))),
                t.p({ className: "txt-hint" }, "Только чтение — просмотр контента, аудиторий и результатов. Полный доступ — все подключённые инструменты, включая изменение контента и запуск рассылок. Выбор можно изменить ниже."),
                t.h3(null, "Инструменты"), () => choices(s.tools, "selected", tool => tool.name + (tool.readOnly ? " · чтение" : " · изменение")),
                t.h3(null, "Коллекции контента"), () => choices(s.collections, "selectedCollections", name => name),
                t.p({ className: "txt-hint" }, "Коллекции ограничены настройками сервера. Запуск пушей разрешается отдельным инструментом push_launch."),
                t.button({ type: "submit", className: "btn", disabled: () => s.busy || !s.selected.length }, "Создать ключ"))),
        () => s.token ? t.section({ role: "status", className: "mcp-secret" }, t.h2(null, "Сохраните ключ сейчас"), t.p(null, "После закрытия он больше не отображается."), t.div({ className: "field" }, t.textarea({ readOnly: true, ariaLabel: "Созданный MCP ключ", value: s.token, onmount: el => { el.scrollIntoView({ block: "center" }); el.focus({ preventScroll: true }); } })), t.div({ className: "mcp-secret-actions" }, t.button({ type: "button", className: "btn", onclick: () => run(async () => { await navigator.clipboard.writeText(s.token); s.copied = true; }) }, () => s.copied ? "Скопировано" : "Скопировать ключ"), t.button({ type: "button", className: "btn secondary", onclick: () => s.token = "" }, "Ключ сохранён"))) : null,
        t.section(null, t.h2(null, "Выданные ключи"), () => s.keys.length ? t.div(null, ...s.keys.map(key => t.article({ className: "mcp-key" }, t.div(null, t.strong(null, key.name), t.p(null, key.tools.join(", ")), t.small(null, key.revoked ? "Отозван" : key.expiresAt ? `До ${key.expiresAt}` : "Без срока действия")), key.revoked ? null : t.button({ type: "button", className: "btn secondary", disabled: () => s.busy, onclick: () => run(async () => { await app.pb.send(`/api/mcp/admin/keys/${key.id}`, { method: "DELETE", requestKey: null }); await load(); }) }, "Отозвать")))) : t.p(null, "Ключей пока нет.")));
}
