document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/push/style.css?v=2" }));
app.store.headerLinks = [...app.store.headerLinks, { label: "Пуши", href: "#/push", icon: "ri-notification-3-line" }];
app.routes.superuserOnly("#/push", () => t.div({ className: "page push-shell" }, pushPage()));
function pushPage() {
    const control = (props) => t.div({ className: "field" }, t.input(props));
    app.store.title = "Пуши";
    const blankCampaign = () => ({ version: 0, name: "", allUsers: false, audienceIds: [], excludeAudienceIds: [], lastDeviceOnly: false, cooldownHours: 24, message: { title: "", text: "", action: "app", target: "", image: "" } });
    const s = store({ tab: "Кампании", loaded: false, busy: false, error: "", notice: "", data: null, campaign: blankCampaign(), audience: null, dirty: false, preview: null, schedule: "", testDevices: [], launchKey: "", report: null, links: [], linksLoaded: false, linksLoading: false, linksError: "", coverage: null, coverageLoading: false, coverageError: "" });
    let nextID = 0;
    const copy = value => JSON.parse(JSON.stringify(value));
    const api = (action, body = {}) => app.pb.send(`/api/push/admin/${action}`, { method: "POST", body, requestKey: null });
    async function load() { s.data = await app.pb.send("/api/push/admin/state", { requestKey: null }); s.loaded = true; }
    async function run(fn) { if (s.busy) return; s.busy = true; s.error = ""; s.notice = ""; try { await fn(); } catch (e) { s.error = e?.response?.message || e.message || "Не удалось выполнить запрос"; app.toasts.error(s.error); } finally { s.busy = false; if (s.notice) app.toasts.success(s.notice); } }
    let coverageTimer, coverageRevision = 0;
    const coverageWatcher = watch(() => s.tab === "Аудитории" && s.audience ? JSON.stringify(s.audience) : "", value => {
        clearTimeout(coverageTimer); coverageRevision++;
        s.coverage = null; s.coverageError = ""; s.coverageLoading = !!value;
        if (value) coverageTimer = setTimeout(() => countAudience(JSON.parse(value)), 400);
    });
    async function countAudience(audience = copy(s.audience)) {
        clearTimeout(coverageTimer);
        const revision = ++coverageRevision;
        s.coverageLoading = true; s.coverageError = "";
        try {
            const result = await api("audience_preview", audience);
            if (revision === coverageRevision) s.coverage = result;
        } catch (error) {
            if (revision === coverageRevision) { s.coverage = null; s.coverageError = error?.response?.message || "Не удалось подсчитать аудиторию. Проверьте условия и повторите."; }
        } finally { if (revision === coverageRevision) s.coverageLoading = false; }
    }
    function coverage() {
        return t.div({ className: "push-coverage", role: "status", ariaLive: "polite", ariaAtomic: "true" }, () => {
            if (s.coverageLoading) return t.p(null, "Подсчитываем охват…");
            if (s.coverageError) return t.p(null, s.coverageError);
            if (!s.coverage) return t.p(null, "Выберите условия для подсчёта охвата.");
            const total = s.coverage.totalUsers || 0;
            const percent = total ? 100 * s.coverage.users / total : 0;
            return t.div(null,
                t.strong(null, `${s.coverage.users.toLocaleString("ru-RU")} из ${total.toLocaleString("ru-RU")} пользователей · ${percent.toLocaleString("ru-RU", { maximumFractionDigits: 1 })}%`),
                t.progress({ max: Math.max(1, total), value: s.coverage.users, ariaLabel: "Охват аудитории" }),
                t.p(null, `Доступных устройств: ${s.coverage.devices}`),
                t.small(null, "Доля от всех пользователей выбранной коллекции. В охват входят только пользователи с доступными устройствами."));
        });
    }
    function changed() { s.dirty = true; s.preview = null; s.launchKey = ""; }
    function field(label, object, key, opts = {}) {
        const id = `push-field-${++nextID}`;
        const common = { id, disabled: opts.disabled || false, value: () => object[key] ?? "", oninput: e => { object[key] = opts.number ? Number(e.target.value) : e.target.value; if (object === s.campaign.message && key === "action") { object.target = ""; if (object.action === "partner" && !s.linksLoaded) loadLinks(); } if (opts.onchange) opts.onchange(object[key]); changed(); } };
        const input = opts.choices ? t.select({ ...common, onchange: common.oninput }, ...opts.choices.map(([value, name]) => t.option({ value }, name)))
            : opts.multiline ? t.textarea({ ...common, rows: 3 }) : t.input({ ...common, type: opts.type || (opts.number ? "number" : "text"), min: opts.min, max: opts.max });
        return t.div({ className: "push-field" }, t.label({ htmlFor: id }, label), t.div({ className: "field" }, input), opts.hint ? t.small({ className: "txt-hint" }, opts.hint) : null);
    }
    const button = (label, fn, primary = false, disabled = () => false) => t.button({ type: "button", className: `btn ${primary ? "" : "secondary"}`, disabled: () => s.busy || disabled(), onclick: () => run(fn) }, label);
    function check(label, object, key) { return t.label({ className: "push-check" }, t.input({ type: "checkbox", checked: () => !!object[key], onchange: e => { object[key] = e.target.checked; changed(); } }), label); }
    function multi(label, items, object, key) { return t.fieldset(null, t.legend(null, label), !items.length ? t.p({ className: "push-empty" }, key === "testDevices" ? "Нет устройств с разрешёнными уведомлениями. Откройте приложение и разрешите уведомления, затем обновите список." : "Аудиторий пока нет. Создайте аудиторию на вкладке «Аудитории».") : null, ...items.map(item => t.label({ className: "push-check" }, t.input({ type: "checkbox", checked: () => (object[key] || []).includes(item.id), onchange: e => { object[key] = e.target.checked ? [...(object[key] || []), item.id] : object[key].filter(id => id !== item.id); if (object !== s) changed(); } }), item.name || `${item.platform} · ${item.userId} · ${item.id}`))); }
    const leaf = (inConversion = false) => inConversion
        ? { kind: "field", source: "conversion", field: "status", op: "eq", value: "approved" }
        : { kind: "field", source: "device", field: "platform", op: "eq", value: "android" };
    function switchCondition(node, kind, inConversion) {
        const next = kind === "field" ? leaf(inConversion)
            : ["all", "any", "not", "conversion"].includes(kind) ? { kind, children: [leaf(inConversion || kind === "conversion")] }
            : { kind, collection: "", variant: "", experiment: "", group: "" };
        for (const key of Object.keys(node)) delete node[key];
        Object.assign(node, next);
        changed();
    }
    function condition(node, parent, index = 0, depth = 0, inConversion = false) {
        const kind = node.kind;
        return t.div({ className: "push-condition" },
            t.div({ className: "push-condition-head field" }, t.select({ ariaLabel: "Тип условия", value: () => node.kind, onchange: e => switchCondition(node, e.target.value, inConversion) }, ...[["field", "Поле"], ["all", "Все условия (И)"], ["any", "Любое условие (ИЛИ)"], ["not", "Исключить (НЕ)"], ["conversion", "Есть заявка"], ["variant", "Назначение Variants"]].filter(([v]) => !inConversion || !["conversion", "variant"].includes(v)).map(([v, label]) => t.option({ value: v }, label))),
                parent ? t.button({ type: "button", className: "btn sm secondary", ariaLabel: "Удалить условие", onclick: () => { parent.children.splice(index, 1); changed(); } }, "×") : null),
            ["all", "any", "not", "conversion"].includes(kind) ? t.div(null,
                () => t.div(null, ...(node.children || []).map((child, i) => condition(child, node, i, depth + 1, inConversion || kind === "conversion"))),
                ["all", "any"].includes(kind) && depth < 6 ? t.button({ type: "button", className: "btn sm secondary", onclick: () => { node.children.push(leaf(inConversion)); changed(); } }, "Добавить условие") : null)
                : kind === "variant" ? t.div({ className: "push-grid" }, field("Коллекция Variants", node, "collection"), field("Вариант", node, "variant"), field("Эксперимент", node, "experiment"), field("Группа", node, "group"))
                : t.div({ className: "push-grid" },
                    field("Источник", node, "source", { choices: [["user", "Пользователь"], ["device", "Устройство"], ...(inConversion ? [["conversion", "Заявка"]] : [])], onchange: source => {
                        node.field = source === "device" ? "platform" : source === "conversion" ? "status" : "id";
                        node.op = "eq"; node.value = source === "device" ? "android" : source === "conversion" ? "approved" : "";
                    } }),
                    (() => { const collectionName = node.source === "device" ? "push_devices" : node.source === "conversion" ? "conversations" : s.audience.authCollection; const fields = s.data.fields[collectionName]?.fields || []; return field("Поле", node, "field", { choices: [["", "Выберите поле"], ...fields.filter(f => !["json", "password", "relation", "polymorphicRelation"].includes(f.type)).map(f => [f.name, f.name])] }); })(),
                    field("Сравнение", node, "op", { choices: [["eq", "Равно"], ["ne", "Не равно"], ["gt", "Больше"], ["gte", "Не меньше"], ["lt", "Меньше"], ["lte", "Не больше"], ["empty", "Пусто"], ["withinHours", "За последние N часов"], ["olderHours", "Раньше N часов назад"]] }),
                    (() => { const collectionName = node.source === "device" ? "push_devices" : node.source === "conversion" ? "conversations" : s.audience.authCollection; const type = s.data.fields[collectionName]?.fields.find(f => f.name === node.field)?.type; if (node.op === "empty") return null; if (type === "bool") return t.label({ className: "push-check" }, t.input({ type: "checkbox", checked: () => node.value === true, onchange: e => { node.value = e.target.checked; changed(); } }), "Да"); return field("Значение", node, "value", { number: type === "number" || ["withinHours", "olderHours"].includes(node.op) }); })()));
    }
    function audiences() {
        return t.div({ className: "push-workspace" },
            t.aside(null, t.h2(null, "Аудитории"), button("Новая аудитория", async () => { s.audience = { version: 0, name: "", authCollection: s.data.authCollections[0] || "users", condition: { kind: "all", children: [leaf()] }, userIds: [], excludeUserIds: [] }; s.preview = null; }),
                ...s.data.audiences.map(a => button(a.name, async () => { s.audience = copy(a); s.preview = null; }))),
            () => s.audience ? t.section(null, t.h2(null, s.audience.id ? "Редактирование аудитории" : "Новая аудитория"), field("Название аудитории", s.audience, "name"), field("Пользователи", s.audience, "authCollection", { choices: s.data.authCollections.map(v => [v, v]) }),
                coverage(), t.h3(null, "Условия"), () => s.audience.condition ? condition(s.audience.condition) : t.p(null, "Все пользователи с доступными устройствами."),
                t.div({ className: "push-actions" }, button("Все пользователи", async () => { s.audience.condition = null; }), button("Добавить условия", async () => { s.audience.condition = { kind: "all", children: [leaf()] }; })),
                listIDs("Только эти user ID (через запятую)", s.audience, "userIds"), listIDs("Исключить user ID", s.audience, "excludeUserIds"),
                t.div({ className: "push-actions" }, button("Сохранить аудиторию", async () => { s.audience = await api("audience_save", s.audience); await load(); s.notice = "Аудитория сохранена."; }, true), button("Обновить подсчёт", () => countAudience()))) : t.section(null, t.h2(null, "Кому отправить сообщение"), t.p(null, "Выберите аудиторию или создайте новую. Условия вычисляются по актуальным данным перед отправкой.")));
    }
    function listIDs(label, object, key) { return t.label({ className: "push-field" }, label, control({ value: () => (object[key] || []).join(", "), onchange: e => { object[key] = e.target.value.split(/[\s,]+/).filter(Boolean); changed(); } })); }
    function preview() { return () => s.preview ? t.p({ role: "status", className: "push-preview" }, `${s.preview.users} пользователей · ${s.preview.devices} устройств`) : null; }
    function recipients() {
        return t.div(null,
            t.fieldset(null, t.legend(null, "Кому отправить"),
                ...[[false, "Выбранные аудитории"], [true, "Все пользователи"]].map(([all, label]) =>
                    t.label({ className: "push-check" }, t.input({ type: "radio", name: "push-recipients", value: String(all), checked: () => !!s.campaign.allUsers === all,
                        onchange: () => { s.campaign.allUsers = all; if (all) s.campaign.audienceIds = []; changed(); } }), label)),
                s.campaign.allUsers ? t.p({ className: "txt-hint" }, "Все пользователи с доступными устройствами во всех подключённых коллекциях. Исключения, выбор последнего устройства и интервал между рассылками учитываются.") : null),
            s.campaign.allUsers ? null : multi("Получатели", s.data.audiences, s.campaign, "audienceIds"));
    }
    function campaigns() {
        return t.div({ className: "push-workspace" }, t.aside(null, t.h2(null, "Кампании"), button("Новая кампания", async () => { s.campaign = blankCampaign(); s.dirty = false; s.preview = null; s.launchKey = ""; s.schedule = ""; s.testDevices = []; }), ...s.data.campaigns.map(c => button(c.name, async () => { s.campaign = copy(c); s.dirty = false; s.preview = null; s.launchKey = ""; s.schedule = ""; s.testDevices = []; if (s.campaign.message.action === "partner" && !s.linksLoaded) loadLinks(); }))),
            () => t.section(null, t.h2(null, s.campaign.id ? "Редактирование кампании" : "Новая кампания"), field("Название кампании", s.campaign, "name"),
                t.div({ className: "push-composer" }, t.div(null, field("Заголовок", s.campaign.message, "title"), field("Текст уведомления", s.campaign.message, "text", { multiline: true }), field("Изображение HTTPS", s.campaign.message, "image")), t.div({ className: "push-notification", ariaLabel: "Предпросмотр уведомления" }, t.small(null, "УВЕДОМЛЕНИЕ"), t.strong(null, () => s.campaign.message.title || "Заголовок"), t.p(null, () => s.campaign.message.text || "Текст сообщения"))),
                field("При нажатии", s.campaign.message, "action", { choices: [["app", "Открыть приложение"], ["route", "Открыть экран"], ["partner", "Открыть партнёрское предложение"]] }),
                () => s.campaign.message.action === "partner" ? partnerSelect() : s.campaign.message.action === "route" ? field("Маршрут", s.campaign.message, "target", { hint: "Например, /offers. Экран откроется внутри приложения." }) : null,
                recipients(), s.data.audiences.length ? multi("Исключить аудитории", s.data.audiences, s.campaign, "excludeAudienceIds") : null,
                check("Только последнее активное устройство пользователя", s.campaign, "lastDeviceOnly"), field("Минимальный интервал между рассылками, часов", s.campaign, "cooldownHours", { number: true, min: 0, max: 8760 }),
                t.div({ className: "push-actions" }, button("Сохранить кампанию", async () => { if (s.campaign.message.action === "app") s.campaign.message.target = ""; s.campaign = await api("campaign_save", s.campaign); s.dirty = false; await load(); s.notice = "Кампания сохранена. Отправка ещё не запущена."; }, true), button("Проверить аудиторию", async () => { s.preview = await api("preview", { campaignId: s.campaign.id }); }, false, () => !s.campaign.id || s.dirty)), preview(),
                t.div({ className: "push-launch" }, t.h3(null, "Запуск сохранённой кампании"), t.p({ className: "push-readiness", ariaLive: "polite" }, launchHint),
                    t.label({ className: "push-field" }, "Время отправки (пусто — сейчас)", control({ type: "datetime-local", value: () => s.schedule, onchange: e => { s.schedule = e.target.value; s.launchKey = ""; } })),
                    button("Запустить рассылку", async () => { s.launchKey ||= crypto.randomUUID(); const r = await api("launch", { campaignId: s.campaign.id, version: s.campaign.version, idempotencyKey: s.launchKey, scheduledAt: s.schedule ? new Date(s.schedule).toISOString() : "" }); s.launchKey = ""; s.notice = `Запуск создан: ${r.id}. Статус: ${r.status}.`; await load(); }, true, () => !s.campaign.id || s.dirty || !s.preview?.devices || !s.data.config.hasOAuthToken),
                    t.details({ className: "push-test" }, t.summary(null, "Тестовая отправка"), t.p(null, "Только выбранные устройства, без отправки всей аудитории. Сначала сохраните кампанию."), multi("Тестовые устройства (последние 100)", s.data.devices.filter(d => d.enabled), s, "testDevices"), button("Обновить устройства", load), button("Отправить тест", async () => { await api("test", { campaignId: s.campaign.id, version: s.campaign.version, idempotencyKey: crypto.randomUUID(), testDeviceIds: s.testDevices }); s.notice = "Тестовая отправка добавлена в очередь."; await load(); }, false, () => !s.campaign.id || s.dirty || !s.testDevices.length || !s.data.config.hasOAuthToken)))));
    }
    function launchHint() {
        if (!s.campaign.id) return "Сначала сохраните кампанию. Сохранение не запускает отправку.";
        if (s.dirty) return "Сохраните изменения перед отправкой.";
        if (!s.data.config.hasOAuthToken) return "Для отправки заполните AppMetrica на вкладке «Настройки». Аудиторию можно проверить заранее.";
        if (!s.preview) return "Нажмите «Проверить аудиторию», чтобы увидеть число получателей.";
        if (!s.preview.devices) return "В аудитории нет доступных устройств. Измените условия или дождитесь регистрации устройств.";
        return `Готово к запуску: ${s.preview.users} пользователей · ${s.preview.devices} устройств. Каждый запуск сохраняется в истории.`;
    }
    async function loadLinks() {
        if (s.linksLoading) return;
        s.linksLoading = true; s.linksError = "";
        try {
            s.links = await app.pb.collection("partner_links").getFullList({ sort: "name,id", fields: "id,name,provider,active", requestKey: null });
            s.linksLoaded = true;
        } catch (error) {
            s.linksError = error?.status === 404 ? "Плагин Partner Links не подключён." : "Не удалось загрузить партнёрские ссылки.";
        } finally { s.linksLoading = false; }
    }
    function partnerSelect() {
        const id = `push-partner-${++nextID}`;
        if (!s.linksLoaded && !s.linksLoading && !s.linksError) loadLinks();
        return t.div({ className: "push-field push-partner" }, t.label({ htmlFor: id }, "Партнёрская ссылка"),
            () => {
                const options = s.links.filter(link => link.active || link.id === s.campaign.message.target).map(link => ({ value: link.id, label: `${link.name || "Без названия"} · ${link.provider} · ${link.id}${link.active ? "" : " · отключена"}` }));
                if (s.campaign.message.target && !options.some(opt => opt.value === s.campaign.message.target)) options.unshift({ value: s.campaign.message.target, label: `Ссылка недоступна · ${s.campaign.message.target}` });
                return t.div({ className: "field" }, app.components.select({ id, required: true, searchThreshold: 1, options, value: () => s.campaign.message.target, disabled: s.linksLoading || !!s.linksError || !options.length, placeholder: s.linksLoading ? "Загрузка ссылок…" : "Выберите предложение", noItemsFoundText: "Ссылки не найдены", onchange: options => { s.campaign.message.target = options[0]?.value || ""; changed(); } }));
            },
            () => s.linksError ? t.div({ role: "alert", className: "push-inline-help" }, s.linksError, t.button({ type: "button", className: "btn sm secondary", onclick: loadLinks }, "Повторить загрузку ссылок")) : s.linksLoaded && !s.links.some(link => link.active) ? t.p({ className: "push-empty" }, "Нет активных партнёрских ссылок. Создайте или включите предложение в коллекции partner_links.") : t.small({ className: "txt-hint" }, "Поиск по названию, провайдеру или ID. В кампании сохраняется ID выбранной ссылки."));
    }
    function history() { return t.section(null, t.div({ className: "push-actions" }, t.h2(null, "История запусков"), button("Обновить", load)), s.data.runs.length ? t.div(null, ...s.data.runs.map(r => t.article({ className: "push-run" }, t.div(null, t.strong(null, r.name), t.p(null, `${r.status} · ${r.recipients} устройств`), t.small(null, r.created), r.error ? t.p({ role: "alert" }, r.error) : null), t.div({ className: "push-actions" }, button("Результаты", async () => { s.report = await api("report", { runId: r.id }); }), r.status === "scheduled" ? button("Отменить", async () => { await api("cancel", { runId: r.id }); await load(); }) : null)))) : t.p(null, "Рассылок пока нет."),
        () => s.report ? t.div({ className: "push-report" }, t.h3(null, s.report.name), t.p(null, `Статус: ${s.report.status}. Открытия, зарегистрированные приложением: ${s.report.opens}.`), t.p(null, `Группа в AppMetrica: ${s.report.appmetricaGroupId}`), ...(s.report.conversions || []).map(c => t.p(null, `${c.status}: ${c.count} · ${c.amount} ${c.currency}`)), t.small(null, "Отправлено не означает доставлено. Доставка и открытия SDK доступны в отчёте AppMetrica. Суммы сгруппированы по статусу и валюте.")) : null); }
    function settings() {
        const locks = s.data.configLocks || { all: false, fields: [] };
        const locked = key => locks.all || locks.fields.includes(key);
        const allLocked = ["applicationId", "sendRate", "oauthToken"].every(locked);
        const tokenHint = locked("oauthToken")
            ? (s.data.config.hasOAuthToken ? "Токен задан сервером. Значение скрыто." : "Токен не задан. Настройте его на сервере и перезапустите сервер.")
            : (s.data.config.hasOAuthToken ? "Токен сохранён. Пустое поле сохраняет текущий." : "Серверный токен AppMetrica с доступом к приложению.");
        return t.section(null, t.h2(null, "AppMetrica Push"),
            locks.all || locks.fields.length ? t.p({ className: "txt-hint" }, allLocked ? "Настройки закреплены в Go-коде. Изменение доступно только на сервере." : "Часть настроек закреплена в Go-коде и недоступна для изменения.") : null,
            t.div({ className: "push-grid" },
                field("Application ID", s.data.config, "applicationId", { number: true, disabled: locked("applicationId") }),
                field("Скорость отправки в секунду", s.data.config, "sendRate", { number: true, min: 100, max: 5000, disabled: locked("sendRate") })),
            s.data.oauthClientId ? t.div(null,
                field("OAuth ClientID", s.data, "oauthClientId", { disabled: true, hint: "Идентификатор OAuth-приложения Яндекс ID. Закреплён в Go-коде." }),
                t.p(null, t.a({ href: `https://oauth.yandex.ru/authorize?response_type=token&client_id=${encodeURIComponent(s.data.oauthClientId)}`, target: "_blank", rel: "noopener noreferrer" }, "Получить OAuth-токен ↗")),
                t.p({ className: "txt-hint" }, "Войдите в Яндекс под аккаунтом с доступом к приложению AppMetrica. Полученный токен задайте на сервере в APPMETRICA_PUSH_OAUTH_TOKEN. Client secret для этого способа не требуется.")) : null,
            field("OAuth token", s.data.config, "oauthToken", { type: "password", disabled: locked("oauthToken"), hint: tokenHint }),
            allLocked ? null : button("Сохранить настройки", async () => {
                await app.pb.send("/api/push/admin/config", { method: "PUT", body: s.data.config, requestKey: null });
                await load(); s.notice = "Настройки сохранены.";
            }, true));
    }
    function devices() { return t.section(null, t.div({ className: "push-actions" }, t.h2(null, "Устройства"), button("Обновить", load)), t.p(null, "Последние 100 устройств. Регистрация и разрешение обновляются приложением."), !s.data.devices.length ? t.p({ className: "push-empty" }, "Устройств пока нет. Они появятся после входа в приложение с подключённой регистрацией пушей.") : null, ...s.data.devices.map(d => t.article({ className: "push-run" }, t.div(null, t.strong(null, `${d.platform} · ${d.userId}`), t.p(null, `${d.language} · ${d.appVersion} · ${d.enabled ? "Включено" : "Отключено"}`), t.small(null, d.id + " · " + d.lastSeen))))); }
    run(load);
    return t.div({ className: "push-page", onunmount: () => { clearTimeout(coverageTimer); coverageRevision++; coverageWatcher.unwatch(); } }, t.header(null, t.h1(null, "Пуши"), t.p(null, "Подготовьте сообщение, выберите аудиторию и запустите рассылку.")),
        t.nav({ className: "push-tabs", ariaLabel: "Разделы пушей" }, ...["Кампании", "Аудитории", "История", "Устройства", "Настройки"].map(tab => t.button({ type: "button", className: () => `btn ${s.tab === tab ? "" : "secondary"}`, ariaPressed: () => String(s.tab === tab), disabled: () => s.busy, onclick: () => { s.tab = tab; s.error = ""; s.notice = ""; s.preview = null; } }, tab))),
        () => s.error ? t.div({ role: "alert", className: "alert alert-danger" }, s.error) : null,
        () => s.notice ? t.div({ role: "status", className: "push-notice" }, s.notice) : null,
        () => !s.loaded ? t.div(null, t.p(null, s.error ? "Не удалось загрузить данные." : "Загрузка…"), s.error ? button("Повторить загрузку", load) : null) : s.tab === "Кампании" ? campaigns() : s.tab === "Аудитории" ? audiences() : s.tab === "История" ? history() : s.tab === "Устройства" ? devices() : settings());
}
