app.store.headerLinks = [...app.store.headerLinks, { label: "AppMetrica", href: "#/appmetrica", icon: "ri-bar-chart-line" }];
app.routes.superuserOnly("#/appmetrica", () => {
    app.store.title = "AppMetrica";
    const s = store({ data: null, busy: false, error: "", notice: "", check: null, dirty: false, token: "", applicationId: 0, applications: [] });
    const labels = { events: "События и доходы", push: "Push API", reports: "Чтение аналитики" };
    const locked = key => s.data?.locks.all || s.data?.locks.fields.includes(key);
    const run = async fn => { if (s.busy) return; s.busy = true; s.error = ""; s.notice = ""; try { await fn(); } catch (e) { s.error = e?.response?.message || e.message; } finally { s.busy = false; } };
    const apply = data => { s.data = data; s.check = null; s.dirty = false; s.token = ""; s.applicationId = data.config.applicationId; s.applications = []; };
    const load = async () => apply(await app.pb.send("/api/appmetrica/admin/config", { requestKey: null }));
    const changed = () => { s.check = null; s.dirty = true; s.applications = []; };
    const connect = async () => {
        const result = await app.pb.send("/api/appmetrica/admin/connect", { method: "POST", body: { version: s.data.config.version, oauthToken: s.token, applicationId: s.applicationId }, requestKey: null });
        if (result.applications) { s.applications = result.applications; return; }
        apply(result.settings); s.notice = "Подключение сохранено. Ключи получены из AppMetrica.";
    };
    const row = (label, value, present) => t.div({ className: "alert", style: "margin-bottom:10px;overflow-wrap:anywhere" }, t.strong(null, label), t.p(null, present ? `✓ ${value || "Задано · значение скрыто"}` : "⚠ Не получено"));
    run(load);
    return t.div({ className: "page" }, t.div({ className: "content", style: "max-width:880px;padding:24px 24px 80px;width:100%" },
        t.h1(null, "AppMetrica"), t.p({ className: "txt-hint" }, "Общее подключение для партнёрских ссылок, пушей и аналитики."),
        () => s.error ? t.div({ role: "alert", className: "alert alert-danger" }, s.error) : null,
        () => s.notice ? t.p({ role: "status" }, s.notice) : null,
        () => !s.data ? t.div(null, "Загрузка…", s.error ? t.button({ className: "btn", onclick: () => run(load) }, "Повторить") : null) : t.div(null,
            t.form({ onsubmit: e => { e.preventDefault(); run(connect); } },
                t.h2(null, "Подключение"), t.p({ className: "txt-hint" }, "Укажите OAuth-токен и приложение. Остальные ключи получим автоматически."),
                t.div({ style: "margin-bottom:18px" }, t.label({ htmlFor: "am-applicationId" }, "Application ID"),
                    t.p({ className: "txt-hint" }, s.data.config.applicationId ? `✓ Задано${locked("applicationId") ? " в коде проекта" : ""}` : "Если приложение одно, можно оставить пустым."),
                    t.div({ className: "field" }, t.input({ id: "am-applicationId", type: "number", min: 1, readOnly: locked("applicationId"), disabled: () => s.busy, value: () => s.applicationId || "", oninput: e => { s.applicationId = Number(e.target.value); changed(); } }))),
                t.div({ style: "margin-bottom:18px" }, t.label({ htmlFor: "am-oauthToken" }, "OAuth-токен"),
                    t.p({ className: "txt-hint" }, s.data.hasOAuthToken ? `✓ Задан${locked("oauthToken") ? " в коде проекта" : ""} · значение скрыто` : "⚠ Не задан. Нужен доступ к настройкам и ключам приложения."),
                    locked("oauthToken") ? null : t.div({ className: "field" }, t.input({ id: "am-oauthToken", type: "password", autocomplete: "off", disabled: () => s.busy, value: () => s.token, placeholder: s.data.hasOAuthToken ? "Пустое поле сохраняет текущий токен" : "OAuth-токен", oninput: e => { s.token = e.target.value; changed(); } }))),
                s.data.config.oauthClientId ? t.p(null, t.a({ href: `https://oauth.yandex.ru/authorize?response_type=token&client_id=${encodeURIComponent(s.data.config.oauthClientId)}`, target: "_blank", rel: "noopener noreferrer" }, "Получить OAuth-токен ↗")) : null,
                () => s.applications.length ? t.div({ style: "margin-bottom:18px" }, t.label({ htmlFor: "am-application" }, "Выберите приложение"), app.components.select({ id: "am-application", value: () => String(s.applicationId || ""), options: s.applications.map(a => ({ value: String(a.id), label: `${a.name} (${a.id})` })), onchange: options => { s.applicationId = Number(options[0]?.value || 0); s.dirty = true; } })) : null,
                s.data.locks.all ? null : t.button({ type: "submit", className: "btn", disabled: () => s.busy || (!s.token && !s.data.hasOAuthToken) || (s.applications.length > 0 && !s.applicationId) }, () => s.busy ? "Подождите…" : "Подключить AppMetrica")),
            t.section({ style: "margin-top:24px", ariaLabel: "Полученные параметры" }, t.h2(null, "Параметры приложения"),
                row("SDK API key", s.data.config.sdkApiKey, !!s.data.config.sdkApiKey), row("Post API key", "", s.data.hasPostApiKey)),
            t.section({ style: "margin-top:24px", ariaLabel: "Готовность AppMetrica" }, t.h2(null, "Готовность"), Object.entries(labels).map(([key, label]) => t.div({ className: "alert", style: "margin-bottom:12px", role: "status" },
                t.strong(null, label), t.p(null, () => { const item = (s.check || s.data.readiness)[key]; return `${item.verified ? "✓ Проверено. " : item.configured ? "✓ Заполнено. " : "⚠ "}${item.message}`; }))),
                t.button({ className: "btn secondary", disabled: () => s.busy || s.dirty, onclick: () => run(async () => { s.check = await app.pb.send("/api/appmetrica/admin/check", { method: "POST", body: {}, requestKey: null }); }) }, "Проверить доступ"),
                t.p({ className: "txt-hint" }, () => s.dirty ? "Сначала сохраните подключение." : "Проверка только читает данные. События и пуши не отправляются.")))));
});
