document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/push/style.css?v=9" }));
app.store.headerLinks = [...app.store.headerLinks, { label: "Пуши", href: "#/push", icon: "ri-notification-3-line" }];
app.routes.superuserOnly("#/push", () => t.div({ className: "page push-shell" }, pushPage()));
const pushDeviceCollection = collection => collection?.name === "push_devices";
watch(() => pushDeviceCollection(app.store.activeCollection), active => document.body.classList.toggle("push-devices-active", !!active));
const pushRecordUpsert = app.modals.openRecordUpsert;
app.modals.openRecordUpsert = function(collection, record, options) {
    if (!pushDeviceCollection(collection)) return pushRecordUpsert(collection, record, options);
    const id = typeof record === "string" ? record : record?.id;
    if (id) return app.modals.openRecordPreview({ id, collectionId: collection.id });
    app.toasts.error("Устройства регистрируются и обновляются приложением.");
};
const pushRecordsList = app.components.recordsList;
app.components.recordsList = function(props = {}) {
    return pushRecordsList({ ...props, collection: () => {
        const collection = typeof props.collection === "function" ? props.collection() : props.collection;
        return pushDeviceCollection(collection) ? { ...collection, type: "view" } : collection;
    } });
};
function pushPage() {
    const control = (props) => t.div({ className: "field" }, t.input(props));
    app.store.title = "Пуши";
    const blankCampaign = () => ({ version: 0, name: "", allUsers: false, audienceIds: [], excludeAudienceIds: [], lastDeviceOnly: false, cooldownHours: 24, message: { title: "", text: "", action: "app", target: "", image: "" } });
    const s = store({ tab: "Кампании", modalOpen: false, loaded: false, busy: false, error: "", notice: "", data: null, campaign: blankCampaign(), audience: null, dirty: false, preview: null, schedule: "", testDevices: [], launchKey: "", report: null, reportMode: "diagnostics", overview: null, overviewLoading: false, overviewError: "", overviewFrom: new Date(Date.now() - 29 * 86400000).toISOString().slice(0, 10), overviewTo: new Date().toISOString().slice(0, 10), overviewMetric: "revenue", hiddenCampaigns: [], analytics: null, analyticsScope: "run", analyticsLoading: false, analyticsError: "", links: [], linksLoaded: false, linksLoading: false, linksError: "", coverage: null, coverageLoading: false, coverageError: "" });
    let nextID = 0;
    let activeModal = null;
    const copy = value => JSON.parse(JSON.stringify(value));
    const api = (action, body = {}) => app.pb.send(`/api/push/admin/${action}`, { method: "POST", body, requestKey: null });
    async function load() { s.data = await app.pb.send("/api/push/admin/state", { requestKey: null }); s.loaded = true; if (s.report) { s.report = await api("report", { runId: s.report.id }); if (s.reportMode === "analytics") void loadAnalytics(); } }
    async function run(fn) { if (s.busy) return; s.busy = true; s.error = ""; s.notice = ""; try { await fn(); } catch (e) { s.error = e?.response?.message || e.message || "Не удалось выполнить запрос"; app.toasts.error(s.error); } finally { s.busy = false; if (s.notice) app.toasts.success(s.notice); } }
    let coverageTimer, coverageRevision = 0;
    const coverageWatcher = watch(() => s.tab === "Аудитории" && s.modalOpen && s.audience ? JSON.stringify(s.audience) : "", value => {
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
        const update = value => { object[key] = opts.number ? Number(value) : value; if (object === s.campaign.message && key === "action") { object.target = ""; if (object.action === "partner" && !s.linksLoaded) loadLinks(); } if (opts.onchange) opts.onchange(object[key]); changed(); };
        const common = { id, disabled: opts.disabled || false, value: () => object[key] ?? "", oninput: e => update(e.target.value) };
        const input = opts.choices ? app.components.select({ id, disabled: common.disabled, value: common.value, required: true, options: opts.choices.map(([value, label]) => ({ value, label })), onchange: options => update(options[0]?.value || "") })
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
        const id = `push-condition-${++nextID}`;
        return t.div({ className: "push-condition" },
            t.label({ htmlFor: id, className: "push-control-label" }, "Тип условия"),
            t.div({ className: "push-condition-head field" }, app.components.select({ id, required: true, value: () => node.kind, onchange: options => switchCondition(node, options[0]?.value || "field", inConversion), options: [["field", "Поле"], ["all", "Все условия (И)"], ["any", "Любое условие (ИЛИ)"], ["not", "Исключить (НЕ)"], ["conversion", "Есть заявка"], ["variant", "Назначение Variants"]].filter(([v]) => !inConversion || !["conversion", "variant"].includes(v)).map(([value, label]) => ({ value, label })) }),
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
    function audienceConditions(node) {
        if (!node) return 0;
        if (node.children) return node.children.reduce((total, child) => total + audienceConditions(child), 0);
        return 1;
    }
    function audiences() {
        return t.section({ className: "push-audiences" },
            t.div({ className: "push-actions" }, t.h2(null, "Аудитории"), button("Новая аудитория", () => editAudience(), true)),
            !s.data.audiences.length ? t.p({ className: "push-empty" }, "Аудиторий пока нет. Создайте первую, чтобы выбрать пользователей и условия получения уведомлений.") : null,
            t.div({ className: "push-audience-list" }, ...s.data.audiences.map(a => {
                const usedBy = s.data.campaigns.filter(c => c.audienceIds?.includes(a.id) || c.excludeAudienceIds?.includes(a.id)).length;
                return t.article({ className: "push-audience-card" },
                    t.div({ className: "push-card-heading" }, t.h3(null, a.name), t.small({ className: "txt-hint" }, `Коллекция: ${a.authCollection}`)),
                    t.p(null, a.condition ? `Условий отбора: ${audienceConditions(a.condition)}` : "Все пользователи с разрешёнными уведомлениями"),
                    a.userIds?.length ? t.p({ className: "txt-hint" }, `Выбрано пользователей по ID: ${a.userIds.length}`) : null,
                    a.excludeUserIds?.length ? t.p({ className: "txt-hint" }, `Исключено пользователей по ID: ${a.excludeUserIds.length}`) : null,
                    t.small({ className: "txt-hint" }, `Используется в кампаниях: ${usedBy}. Актуальный охват — в редакторе.`),
                    t.div({ className: "push-actions" }, button("Редактировать", () => editAudience(a))));
            })));
    }
    function editAudience(audience) {
        s.audience = audience ? copy(audience) : { version: 0, name: "", authCollection: s.data.authCollections[0] || "users", condition: { kind: "all", children: [leaf()] }, userIds: [], excludeUserIds: [] };
        s.dirty = false; s.preview = null; s.error = ""; s.notice = "";
        openPanel("audience", () => s.audience?.id ? "Редактирование аудитории" : "Новая аудитория", () => audienceEditor(),
            button("Сохранить аудиторию", async () => {
                s.audience = await api("audience_save", s.audience); s.dirty = false;
                await load(); s.notice = "Аудитория сохранена.";
            }, true), deleteButton("audience"));
    }
    function audienceEditor() {
        if (!s.audience) return null;
        return t.section(null, field("Название аудитории", s.audience, "name"), field("Пользователи", s.audience, "authCollection", { choices: s.data.authCollections.map(v => [v, v]) }),
            coverage(), t.h3(null, "Условия"), () => s.audience?.condition ? condition(s.audience.condition) : t.p(null, "Все пользователи с разрешёнными уведомлениями."),
            t.div({ className: "push-actions" }, button("Все пользователи", async () => { s.audience.condition = null; changed(); }), button("Добавить условия", async () => { s.audience.condition = { kind: "all", children: [leaf()] }; changed(); })),
            listIDs("Только эти user ID (через запятую)", s.audience, "userIds"), listIDs("Исключить user ID", s.audience, "excludeUserIds"),
            t.div({ className: "push-actions" }, button("Обновить подсчёт", () => countAudience())));
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
    function openPanel(kind, title, content, footer, secondaryFooter = null) {
        if (activeModal) return;
        const opener = document.activeElement;
        const titleID = `push-dialog-${++nextID}`;
        const modal = t.div({ className: `modal lg push-modal push-${kind}-modal`, role: "dialog", ariaModal: "true", "html-aria-labelledby": titleID,
            onbeforeclose: (_, forced) => {
                if (forced) return true;
                if (s.busy) return false;
                if (!["campaign", "audience"].includes(kind) || !s.dirty) return true;
                return new Promise(resolve => app.modals.confirm(`Закрыть без сохранения изменений ${kind === "audience" ? "аудитории" : "кампании"}?`, () => resolve(true), () => resolve(false), { yesButton: "Не сохранять", noButton: "Продолжить редактирование" }));
            },
            onafterclose: el => {
                activeModal = null; s.modalOpen = false;
                s.error = ""; s.notice = ""; s.report = null; s.audience = null; analyticsRevision++; s.analytics = null; s.analyticsLoading = false;
                el.remove();
                if (opener?.isConnected) opener.focus({ preventScroll: true });
            },
            onkeydown: e => {
                if (app.modals.getTop() !== modal) return;
                if (e.key === "Escape" && !document.querySelector(":popover-open")) { e.preventDefault(); e.stopPropagation(); app.modals.close(modal); }
                if (e.key !== "Tab") return;
                const targets = [...modal.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], summary, [tabindex="0"]')].filter(el => el.getClientRects().length);
                const first = targets[0], last = targets.at(-1);
                if (e.shiftKey && (document.activeElement === first || document.activeElement === modal)) { e.preventDefault(); last?.focus(); }
                else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
            },
        }, t.header({ className: "modal-header" }, t.h5({ id: titleID, className: "modal-title" }, title),
            t.button({ type: "button", className: "btn sm circle transparent modal-close-btn m-l-auto", ariaLabel: "Закрыть диалог", disabled: () => s.busy, onclick: () => app.modals.close(modal) }, t.i({ className: "ri-close-line", ariaHidden: true }))),
            t.div({ className: "modal-content push-modal-content" },
                () => s.error ? t.div({ role: "alert", className: "alert danger" }, s.error) : null,
                () => s.notice ? t.div({ role: "status", className: "push-notice" }, s.notice) : null,
                content),
            t.footer({ className: "modal-footer" }, t.button({ type: "button", className: "btn secondary", disabled: () => s.busy, onclick: () => app.modals.close(modal) }, "Закрыть"), secondaryFooter, footer));
        activeModal = modal;
        s.modalOpen = true;
        root.append(modal);
        app.modals.open(modal);
    }
    function deleteButton(kind) {
        const label = kind === "campaign" ? "Удалить кампанию" : "Удалить аудиторию";
        return () => s[kind]?.id ? t.button({ type: "button", className: "btn danger", disabled: () => s.busy,
            onclick: () => {
                const record = s[kind];
                const saved = s.data[kind === "campaign" ? "campaigns" : "audiences"].find(item => item.id === record.id);
                const message = kind === "campaign" ? "История отправок и аналитика сохранятся. Вернуть кампанию будет нельзя." : "Вернуть аудиторию будет нельзя. Используемые в кампаниях аудитории удалить нельзя.";
                app.modals.confirm(`${label} «${saved?.name || record.name}»? ${message}`, () => run(async () => {
                    await api(`${kind}_delete`, { id: record.id, version: record.version });
                    s.dirty = false;
                    // Reflect the successful deletion even if the subsequent refresh fails.
                    const key = kind === "campaign" ? "campaigns" : "audiences";
                    s.data[key] = s.data[key].filter(item => item.id !== record.id);
                    app.modals.close(activeModal, true);
                    await load();
                    s.notice = kind === "campaign" ? "Кампания удалена. История отправок сохранена." : "Аудитория удалена.";
                }), null, { yesButton: label, noButton: "Отмена" });
            } }, label) : null;
    }
    function editCampaign(campaign) {
        s.campaign = campaign ? copy(campaign) : blankCampaign();
        s.dirty = false; s.preview = null; s.launchKey = ""; s.schedule = ""; s.testDevices = []; s.error = ""; s.notice = "";
        if (s.campaign.message.action === "partner" && !s.linksLoaded) loadLinks();
        openPanel("campaign", () => s.campaign.id ? "Редактирование кампании" : "Новая кампания", () => campaignEditor(),
            button("Сохранить кампанию", async () => {
                if (s.campaign.message.action === "app") s.campaign.message.target = "";
                s.campaign = await api("campaign_save", s.campaign); s.dirty = false;
                await load(); s.notice = "Кампания сохранена. Отправка ещё не запущена.";
            }, true), deleteButton("campaign"));
    }
    let analyticsRevision = 0;
    async function loadAnalytics() {
        if (!s.report) return;
        const revision = ++analyticsRevision;
        s.analytics = null; s.analyticsError = ""; s.analyticsLoading = true;
        try {
            const result = await api("analytics", { runId: s.report.id, scope: s.analyticsScope });
            if (revision === analyticsRevision) s.analytics = result;
        } catch (error) {
            if (revision === analyticsRevision) s.analyticsError = error?.response?.message || "Не удалось загрузить аналитику AppMetrica. Повторите запрос.";
        } finally { if (revision === analyticsRevision) s.analyticsLoading = false; }
    }
    async function openReport(id, scope = "run", mode = "diagnostics") {
        s.report = await api("report", { runId: id });
        s.analyticsScope = scope; s.reportMode = mode;
        openPanel("report", () => mode === "analytics" ? "Результаты AppMetrica" : "Результаты запуска", () => mode === "analytics" ? t.section(null, t.h2(null, s.report?.name || ""), analytics()) : report(), button("Обновить результаты", load));
        if (mode === "analytics") void loadAnalytics();
    }
    function campaigns() {
        return t.section({ className: "push-campaigns" },
            t.div({ className: "push-actions" }, t.h2(null, "Кампании"), button("Новая кампания", () => editCampaign(), true)),
            !s.data.campaigns.length ? t.p({ className: "push-empty" }, "Кампаний пока нет. Создайте первую, чтобы подготовить сообщение и выбрать получателей.") : null,
            t.div({ className: "push-campaign-list" }, ...s.data.campaigns.map(c => {
                const latest = s.data.runs.find(r => r.campaignId === c.id);
                return t.article({ className: "push-campaign-card" },
                    t.div({ className: "push-card-heading" }, t.h3(null, c.name), latest ? statusBadge(latest.status) : statusBadge("draft")),
                    t.strong(null, c.message.title), t.p({ className: "push-card-text" }, c.message.text),
                    t.small({ className: "txt-hint" }, c.allUsers ? "Все пользователи с разрешёнными уведомлениями" : `Аудитории: ${c.audienceIds.map(id => s.data.audiences.find(a => a.id === id)?.name || id).join(", ")}`),
                    t.div({ className: "push-actions" }, button("Редактировать", () => editCampaign(c)), latest ? button("Результаты", () => openReport(latest.id)) : null, latest ? button("Результаты AppMetrica", () => openReport(latest.id, "campaign", "analytics")) : null));
            })));
    }
    function campaignEditor() {
        return t.section(null, field("Название кампании", s.campaign, "name"),
                t.div({ className: "push-composer" }, t.div(null, field("Заголовок", s.campaign.message, "title"), field("Текст уведомления", s.campaign.message, "text", { multiline: true }), field("Изображение HTTPS", s.campaign.message, "image")), t.div({ className: "push-notification", ariaLabel: "Предпросмотр уведомления" }, t.small(null, "УВЕДОМЛЕНИЕ"), t.strong(null, () => s.campaign.message.title || "Заголовок"), t.p(null, () => s.campaign.message.text || "Текст сообщения"))),
                field("При нажатии", s.campaign.message, "action", { choices: [["app", "Открыть приложение"], ["route", "Открыть экран"], ["partner", "Открыть партнёрское предложение"]] }),
                () => s.campaign.message.action === "partner" ? partnerSelect() : s.campaign.message.action === "route" ? field("Маршрут", s.campaign.message, "target", { hint: "Например, /offers. Экран откроется внутри приложения." }) : null,
                recipients(), s.data.audiences.length ? multi("Исключить аудитории", s.data.audiences, s.campaign, "excludeAudienceIds") : null,
                check("Только последнее активное устройство пользователя", s.campaign, "lastDeviceOnly"), field("Минимальный интервал между рассылками, часов", s.campaign, "cooldownHours", { number: true, min: 0, max: 8760 }),
                t.div({ className: "push-actions" }, button("Проверить аудиторию", async () => { s.preview = await api("preview", { campaignId: s.campaign.id }); }, false, () => !s.campaign.id || s.dirty)), preview(),
                t.div({ className: "push-launch" }, t.h3(null, "Запуск сохранённой кампании"), t.p({ className: "push-readiness", ariaLive: "polite" }, launchHint),
                    t.label({ className: "push-field" }, "Время отправки (пусто — сейчас)", control({ type: "datetime-local", value: () => s.schedule, onchange: e => { s.schedule = e.target.value; s.launchKey = ""; } })),
                    button("Запустить рассылку", async () => { s.launchKey ||= crypto.randomUUID(); const r = await api("launch", { campaignId: s.campaign.id, version: s.campaign.version, idempotencyKey: s.launchKey, scheduledAt: s.schedule ? new Date(s.schedule).toISOString() : "" }); s.launchKey = ""; s.notice = `Запуск создан: ${r.id}. Статус: ${statuses[r.status]?.[0] || r.status}.`; await load(); }, true, () => !s.campaign.id || s.dirty || !s.preview?.devices || !s.data.config.hasOAuthToken),
                    t.details({ className: "push-test" }, t.summary(null, "Тестовая отправка"), t.p(null, "Только выбранные устройства, без отправки всей аудитории. Сначала сохраните кампанию."), multi("Тестовые устройства (последние 100)", s.data.devices.filter(d => d.enabled && ["authorized", "provisional"].includes(d.notificationPermission)), s, "testDevices"), button("Обновить устройства", load), button("Отправить тест", async () => { await api("test", { campaignId: s.campaign.id, version: s.campaign.version, idempotencyKey: crypto.randomUUID(), testDeviceIds: s.testDevices }); s.notice = "Тестовая отправка добавлена в очередь."; await load(); }, false, () => !s.campaign.id || s.dirty || !s.testDevices.length || !s.data.config.hasOAuthToken))));
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
    const statuses = {
        draft: ["Черновик", "neutral"], scheduled: ["Запланирована", "info"], queued: ["В очереди", "info"], sending: ["Отправляется", "info"],
        submitted: ["Принята AppMetrica", "info"], sent: ["Отправлена", "success"], failed: ["Ошибка", "danger"],
        unknown: ["Статус неизвестен", "warning"], cancelled: ["Отменена", "neutral"], empty: ["Нет получателей", "neutral"],
    };
    function statusBadge(status) {
        const [label, tone] = statuses[status] || [status || "Нет статуса", "neutral"];
        return t.span({ className: `push-status push-status-${tone}`, title: status }, label);
    }
    function failureText(item) {
        return item.error || (item.status === "failed" ? "Причина не сохранена. Запросите детали в AppMetrica." : "");
    }
    function analytics() {
        const count = value => value.toLocaleString("ru-RU", { maximumFractionDigits: 1 });
        const money = value => value.toLocaleString("ru-RU", { style: "currency", currency: "RUB" });
        const metric = (label, value, hint) => t.div({ className: "push-metric" }, t.small(null, label), t.strong(null, value), hint ? t.small(null, hint) : null);
        const sectionNote = section => t.div(null,
            section.sampled ? t.p({ className: "push-analytics-warning" }, "AppMetrica применила выборку: значения оценочные.") : null,
            section.dataLagSeconds ? t.small(null, `Задержка данных: ${count(section.dataLagSeconds)} с`) : null);
        const unavailable = section => t.p({ className: "push-analytics-warning", role: "status" }, section.error || "Данные недоступны.");
        return t.section({ className: "push-analytics", ariaLabel: "Аналитика AppMetrica" },
            t.div({ className: "push-card-heading" }, t.h3(null, "AppMetrica"),
                t.div({ className: "push-actions", role: "group", ariaLabel: "Период результатов" }, ...[["run", "Этот запуск"], ["campaign", "Вся кампания"]].map(([scope, label]) => t.button({ type: "button", className: "btn secondary", ariaPressed: () => s.analyticsScope === scope, onclick: () => { s.analyticsScope = scope; if (s.reportMode === "analytics") void loadAnalytics(); } }, label)))),
            () => {
                if (s.analyticsLoading) return t.p({ role: "status", className: "push-analytics-loading" }, "Загружаем метрики из AppMetrica…");
                if (s.analyticsError) return t.div(null, t.p({ role: "alert" }, s.analyticsError), button("Повторить загрузку аналитики", loadAnalytics));
                const a = s.analytics;
                if (!a) return null;
                const revenue = a.revenue, events = a.events, push = a.push;
                const ratio = events.status === "ready" && events.values.lead > 0 ? events.values.approved / events.values.lead : null;
                return t.div(null,
                    t.p({ className: "txt-hint" }, `${a.scope === "campaign" ? "Все реальные запуски кампании" : a.test ? "Тестовый запуск" : "Выбранный запуск"} · ${a.dateFrom} — ${a.dateTo} · UTC`),
                    t.div({ className: "push-revenue" }, t.small(null, "Revenue по одобрениям"),
                        t.strong({ className: "push-revenue-value" }, revenue.status === "ready" ? money(revenue.values.approved) : "—"),
                        revenue.status === "ready" ? t.div(null,
                            t.p(null, `В hold: ${money(revenue.values.hold)} · отдельно от одобрений`),
                            t.small(null, `${count(revenue.values.approvedEvents)} revenue-событий одобрения. Пересчёт валют в RUB — AppMetrica.`),
                            revenue.values.approvedEvents + revenue.values.holdEvents === 0 ? t.p(null, "Revenue-событий не зарегистрировано. Проверьте передачу revenue у партнёра и дождитесь обработки данных.") : null,
                            sectionNote(revenue)) : unavailable(revenue)),
                    t.h3(null, "Доставка и открытия"),
                    push.status === "ready" ? t.div(null, t.div({ className: "push-metrics" },
                        ...[["sent", "Отправлено"], ["received", "Доставлено"], ["shown", "Показано · Android"], ["opened", "Открыли пуш"]].map(([key, label]) => metric(label, count(push.values[key]), "устройств"))),
                        push.values.sent > 0 ? t.p(null, `Открытия / отправки: ${count(100 * push.values.opened / push.values.sent)}%`) : null, sectionNote(push)) : unavailable(push),
                    t.h3(null, "Этапы конверсии"),
                    events.status === "ready" ? t.div(null,
                        t.dl({ className: "push-stages" }, ...[["click", "Перешли к офферу"], ["lead", "Оставили заявку"], ["approved", "Получили одобрение"], ["hold", "Попали в hold"], ["rejected", "Получили отказ"]].flatMap(([key, label]) => [t.dt(null, label), t.dd(null, count(events.values[key]))])),
                        t.p(null, `Одобрения / заявки: ${ratio === null || ratio > 1 ? "—" : count(ratio * 100) + "%"}`),
                        ratio > 1 ? t.p(null, "Одобрений больше, чем событий заявки: проверьте передачу этапа lead.") : null,
                        t.small(null, "Уникальные выданные ссылки, для которых зарегистрирован этап. Один человек может получить несколько ссылок. Этапы не отражают последний статус заявки."), sectionNote(events)) : unavailable(events),
                    t.details({ className: "push-analytics-method" }, t.summary(null, "Как считаются результаты"),
                        t.p(null, "Все метрики получены из AppMetrica. События офферов и revenue отобраны по сохранённому ID кампании или запуска. В кампанию не входят тестовые запуски."),
                        t.p(null, "Источник фиксируется при выдаче ссылки: последнее проверенное открытие пуша за 7 дней. Поздние постбеки относятся к исходной кампании."),
                        t.p(null, "Этапы считаются независимо. Одобрение без события lead не создаёт заявку в отчёте. Переход к офферу означает редирект на ссылку, а не подтверждённую загрузку страницы партнёра."),
                        t.p(null, "Revenue — сумма событий AppMetrica, не подтверждение выплаты. Повторные постбеки могут увеличивать сумму; последующий отказ автоматически не вычитает ранее переданный доход. Hold показан отдельно. Расходы кампании здесь не учитываются."),
                        t.p(null, "Доставка зависит от событий SDK. Для расширенной статистики iOS требуется Notification Service Extension. Отсутствие события не доказывает отсутствие действия.")),
                    t.small({ className: "push-analytics-updated" }, `Получено: ${new Date(a.fetchedAt).toLocaleString("ru-RU")}. Следующий запрос к AppMetrica — после ${new Date(a.nextRefreshAt).toLocaleTimeString("ru-RU")}.`));
            });
    }
    function report() {
        const r = s.report;
        if (!r) return null;
        return t.section({ className: "push-report", ariaLabel: "Результаты запуска" },
            t.div({ className: "push-actions" }, t.h2(null, r.name), statusBadge(r.status)),
            t.h3(null, "Диагностика выбранного запуска"),
            t.p(null, `Запуск: ${r.created}. Получателей в снимке: ${r.recipients}.`),
            failureText(r) ? t.div({ className: "push-diagnostic", role: "alert" }, t.strong(null, "Причина / последняя ошибка"), t.p(null, failureText(r))) : null,
            r.status === "unknown" ? t.p(null, "Ответ на отправку не подтверждён. Сервер проверяет статус; автоматическая повторная отправка отключена, чтобы не создавать дубликаты.") : null,
            t.p(null, `Группа в AppMetrica: ${r.appmetricaGroupId === "0" ? "ещё не создана" : r.appmetricaGroupId}`),
            (r.jobs || []).some(j => j.status === "failed" && !j.error) ? t.div(null,
                button("Запросить причину в AppMetrica", async () => { s.report = await api("refresh_report", { runId: r.id }); await load(); }),
                t.small(null, "Только проверка статуса, без повторной отправки. До 20 пакетов за запрос.")) : null,
            t.h3(null, "Пакеты отправки"),
            t.div({ className: "push-job-counts" }, ...Object.entries(r.jobCounts || {}).map(([status, count]) => t.span(null, statusBadge(status), ` ${count}`))),
            !(r.jobs || []).length ? t.p({ className: "push-empty" }, "Пакеты ещё не созданы или в аудитории нет устройств.") : null,
            ...(r.jobs || []).map((j, index) => t.article({ className: "push-job" },
                t.div({ className: "push-actions" }, t.strong(null, `Пакет ${index + 1} · ${j.recipients} устройств`), statusBadge(j.status)),
                failureText(j) ? t.p({ className: "push-job-error" }, failureText(j)) : null,
                t.dl({ className: "push-job-meta" },
                    t.dt(null, "ID отправки AppMetrica"), t.dd(null, j.transferId || "не получен"),
                    t.dt(null, "Client transfer ID"), t.dd(null, j.clientTransferId),
                    t.dt(null, "Обновлён"), t.dd(null, j.updated || "—"),
                    ...(j.nextAttempt ? [t.dt(null, j.status === "queued" ? "Следующая попытка" : "Следующая проверка статуса"), t.dd(null, j.nextAttempt)] : []),
                    t.dt(null, "Отложенных проверок / попыток"), t.dd(null, String(j.deferrals || 0))))),
            t.small(null, "Статус отправки и пакеты — данные сервера. Статистика доступна по кнопке «Результаты AppMetrica». «Отправлена» не означает «Доставлена»."));
    }
    function history() {
        return t.section(null, t.div({ className: "push-actions" }, t.h2(null, "История запусков"), button("Обновить", load)),
            s.data.runs.length ? t.div(null, ...s.data.runs.map(r => t.article({ className: "push-run" },
                t.div(null, t.strong(null, r.name), t.div({ className: "push-run-status" }, statusBadge(r.status), t.span(null, `${r.recipients} устройств`)),
                    t.small(null, r.created), failureText(r) ? t.p({ className: "push-job-error" }, failureText(r)) : null),
                t.div({ className: "push-actions" }, button("Результаты", () => openReport(r.id)), button("Результаты AppMetrica", () => openReport(r.id, "run", "analytics")),
                    r.status === "scheduled" ? button("Отменить", async () => { await api("cancel", { runId: r.id }); await load(); }) : null)))) : t.p(null, "Рассылок пока нет."));
    }
    let overviewRevision = 0;
    const overviewMetrics = { revenue: ["Revenue по одобрениям, ₽", "revenue"], holdRevenue: ["Revenue в hold, ₽", "revenue"], opened: ["Открытия пуша", "push"], lead: ["Заявки", "eventDays"], approved: ["Одобрения", "eventDays"] };
    const graphColors = ["#3478db", "#c86d10", "#20866f", "#a653be", "#d34d65", "#448691", "#807722", "#997054", "#6078b5", "#bd6089"];
    const utcToday = () => new Date().toISOString().slice(0, 10);
    const dayOffset = (date, days) => new Date(Date.parse(date + "T00:00:00Z") + days * 86400000).toISOString().slice(0, 10);
    async function loadOverview() {
        const revision = ++overviewRevision;
        s.overviewLoading = true; s.overviewError = ""; s.overview = null;
        try {
            const result = await api("analytics_overview", { dateFrom: s.overviewFrom, dateTo: s.overviewTo });
            if (revision === overviewRevision) { s.overview = result; s.hiddenCampaigns = s.hiddenCampaigns.filter(id => result.campaigns.some(c => c.id === id)); }
        } catch (error) {
            if (revision === overviewRevision) s.overviewError = error?.response?.message || "Не удалось получить обзор AppMetrica.";
        } finally { if (revision === overviewRevision) s.overviewLoading = false; }
    }
    function shiftOverview(days) {
        const from = s.overview?.dateFrom || s.overviewFrom, to = s.overview?.dateTo || s.overviewTo;
        const nextTo = dayOffset(to, days);
        if (nextTo > utcToday()) days = Math.round((Date.parse(utcToday()) - Date.parse(to)) / 86400000);
        if (!days) return;
        s.overviewFrom = dayOffset(from, days); s.overviewTo = dayOffset(to, days);
        void loadOverview();
    }
    function overview() {
        return t.section({ className: "push-overview", ariaLabel: "Обзор эффективности кампаний" },
            t.h2(null, "Эффективность кампаний"),
            t.p({ className: "txt-hint" }, "Последние 10 кампаний с реальными отправками на конец периода. Все показатели — из AppMetrica, по датам событий в UTC."),
            t.form({ className: "push-overview-period", onsubmit: e => { e.preventDefault(); void loadOverview(); } },
                t.label({ className: "push-field" }, "С даты", control({ type: "date", required: true, max: utcToday(), value: () => s.overviewFrom, onchange: e => { s.overviewFrom = e.target.value; } })),
                t.label({ className: "push-field" }, "По дату", control({ type: "date", required: true, max: utcToday(), value: () => s.overviewTo, onchange: e => { s.overviewTo = e.target.value; } })),
                t.button({ type: "submit", className: "btn", disabled: () => s.overviewLoading }, "Показать"),
                ...[7, 30, 90].map(days => t.button({ type: "button", className: "btn secondary", onclick: () => { s.overviewTo = utcToday(); s.overviewFrom = dayOffset(s.overviewTo, 1 - days); void loadOverview(); } }, `${days} дней`))),
            () => {
                if (s.overviewLoading) return t.p({ role: "status", className: "push-analytics-loading" }, "Загружаем сравнение кампаний…");
                if (s.overviewError) return t.div({ role: "alert" }, t.p(null, s.overviewError), button("Повторить загрузку обзора", loadOverview));
                const data = s.overview;
                if (!data) return null;
                const span = 1 + Math.round((Date.parse(data.dateTo) - Date.parse(data.dateFrom)) / 86400000);
                const metric = s.overviewMetric, section = data.sections[overviewMetrics[metric][1]];
                const fmt = value => value == null ? "—" : value.toLocaleString("ru-RU", { maximumFractionDigits: 2 });
                const rate = c => c.totals.sent > 0 && c.totals.opened != null ? fmt(100 * c.totals.opened / c.totals.sent) + "%" : "—";
                const metricID = `push-metric-${++nextID}`;
                return t.div(null,
                    t.div({ className: "push-overview-toolbar" },
                        t.div({ className: "push-actions" }, button("← Раньше", () => shiftOverview(-span)), t.strong(null, `${data.dateFrom} — ${data.dateTo}`), button("Позже →", () => shiftOverview(span), false, () => data.dateTo >= utcToday())),
                        t.div({ className: "push-field" }, t.label({ htmlFor: metricID }, "Показатель графика"), t.div({ className: "field" }, app.components.select({ id: metricID, required: true, value: () => s.overviewMetric, onchange: options => s.overviewMetric = options[0]?.value || "revenue", options: Object.entries(overviewMetrics).map(([value, [label]]) => ({ value, label })) })))),
                    !data.campaigns.length ? t.p({ className: "push-empty" }, "На конец этого периода нет кампаний с реальными отправками. Выберите другие даты.") : t.div(null,
                        section?.status === "ready" ? overviewGraph(data, metric) : t.p({ className: "push-analytics-warning", role: "status" }, section?.error || "График недоступен."),
                        t.div({ className: "push-chart-legend", role: "group", ariaLabel: "Кампании на графике" }, ...data.campaigns.map((c, index) => t.button({ type: "button", className: "btn secondary", ariaPressed: !s.hiddenCampaigns.includes(c.id), onclick: () => { s.hiddenCampaigns = s.hiddenCampaigns.includes(c.id) ? s.hiddenCampaigns.filter(id => id !== c.id) : [...s.hiddenCampaigns, c.id]; } }, t.span({ className: "push-chart-key", style: `--campaign-color:${graphColors[index]}` }, String(index + 1)), c.name))),
                        t.small(null, "Перетащите график по горизонтали или используйте стрелки для сдвига дат. Нажмите на кампанию в легенде, чтобы скрыть или показать линию."),
                        t.h3(null, "Сравнение за период"),
                        t.div({ className: "push-table-scroll", role: "region", ariaLabel: "Сравнение кампаний", tabIndex: 0 },
                            t.table({ className: "push-overview-table" }, t.thead(null, t.tr(null, ...["Кампания", "Отправки", "Открытия", "Открытия / отправки", "Заявки", "Одобрения", "Revenue, ₽", "Hold, ₽", ""].map(label => t.th({ scope: "col" }, label)))),
                                t.tbody(null, ...data.campaigns.map((c, index) => t.tr(null, t.th({ scope: "row" }, `${index + 1}. ${c.name}`), ...[c.totals.sent, c.totals.opened].map(v => t.td(null, fmt(v))), t.td(null, rate(c)), ...[c.totals.lead, c.totals.approved, c.totals.revenue, c.totals.holdRevenue].map(v => t.td(null, fmt(v))), t.td(null, button("Результаты AppMetrica", () => openReport(c.runId, "campaign", "analytics")))))))),
                        ...Object.entries(data.sections).filter(([, v]) => v.status !== "ready").map(([key, v]) => t.p({ className: "push-analytics-warning", role: "status" }, `${{ push: "Доставка", events: "Этапы за период", eventDays: "Этапы по дням", revenue: "Revenue" }[key]}: ${v.error}`)),
                        Object.values(data.sections).some(v => v.sampled) ? t.p({ className: "push-analytics-warning" }, "AppMetrica применила выборку: часть значений оценочная.") : null,
                        t.p({ className: "txt-hint" }, "Отправки и открытия — события, включая повторные запуски. Заявки и одобрения — уникальные ссылки за период; их дневные значения нельзя складывать для подсчёта уникальных заявок за весь период. Revenue по одобрениям и hold разделены; повторные постбеки могут увеличивать сумму."),
                        t.details(null, t.summary(null, "Данные графика по дням"), t.div({ className: "push-table-scroll" }, t.table({ className: "push-overview-table" },
                            t.thead(null, t.tr(null, t.th({ scope: "col" }, "Дата"), ...data.campaigns.map(c => t.th({ scope: "col" }, c.name)))),
                            t.tbody(null, ...data.campaigns[0].days.map((day, i) => t.tr(null, t.th({ scope: "row" }, day.date), ...data.campaigns.map(c => t.td(null, fmt(c.days[i].values[metric])))))))))),
                    t.small({ className: "push-analytics-updated" }, `Получено: ${new Date(data.fetchedAt).toLocaleString("ru-RU")}. Следующее обновление — после ${new Date(data.nextRefreshAt).toLocaleTimeString("ru-RU")}. Задержка AppMetrica: до ${Math.max(0, ...Object.values(data.sections).map(v => v.dataLagSeconds || 0))} с.`));
            });
    }
    function overviewGraph(data, metric) {
        const visible = data.campaigns.map((c, index) => ({ c, index })).filter(({ c }) => !s.hiddenCampaigns.includes(c.id));
        if (!visible.length) return t.p({ className: "push-empty" }, "Выберите хотя бы одну кампанию в легенде.");
        const svgNode = (name, attrs, text) => {
            const el = document.createElementNS("http://www.w3.org/2000/svg", name);
            for (const [key, value] of Object.entries(attrs || {})) el.setAttribute(key, value);
            if (text != null) el.textContent = text;
            return el;
        };
        const width = 960, height = 320, left = 65, right = 20, top = 24, bottom = 40;
        const length = data.campaigns[0].days.length;
        const values = visible.flatMap(({ c }) => c.days.map(d => d.values[metric]));
        const min = Math.min(0, ...values), maximum = Math.max(1, ...values);
        const max = ["revenue", "holdRevenue"].includes(metric) ? maximum : Math.max(4, Math.ceil(maximum / 4) * 4);
        const x = i => left + (width - left - right) * (length === 1 ? 0.5 : i / (length - 1));
        const y = value => top + (height - top - bottom) * (1 - (value - min) / (max - min));
        const svg = svgNode("svg", { viewBox: `0 0 ${width} ${height}`, role: "img", "aria-label": `${overviewMetrics[metric][0]} по дням. Период ${data.dateFrom} — ${data.dateTo}`, tabindex: "0", class: "push-trend" });
        svg.append(svgNode("title", {}, `${overviewMetrics[metric][0]} · перетаскивание и стрелки меняют период`));
        for (let n = 0; n <= 4; n++) {
            const v = min + (max - min) * n / 4;
            svg.append(svgNode("line", { x1: left, y1: y(v), x2: width - right, y2: y(v), class: "push-chart-grid" }), svgNode("text", { x: left - 8, y: y(v) + 4, "text-anchor": "end", class: "push-chart-label" }, v.toLocaleString("ru-RU", { maximumFractionDigits: 1 })));
        }
        for (const i of [...new Set([0, Math.floor((length - 1) / 2), length - 1])]) svg.append(svgNode("text", { x: x(i), y: height - 12, "text-anchor": i === 0 ? "start" : i === length - 1 ? "end" : "middle", class: "push-chart-label" }, data.campaigns[0].days[i].date));
        for (const { c, index } of visible) {
            const path = c.days.map((day, i) => `${i ? "L" : "M"}${x(i)},${y(day.values[metric])}`).join(" ");
            svg.append(svgNode("path", { d: path, fill: "none", stroke: graphColors[index], "stroke-width": 2.5, "stroke-dasharray": index >= 5 ? "7 4" : "none", "vector-effect": "non-scaling-stroke" }));
            c.days.forEach((day, i) => {
                const point = svgNode("circle", { cx: x(i), cy: y(day.values[metric]), r: 4, fill: graphColors[index] });
                point.append(svgNode("title", {}, `${index + 1}. ${c.name} · ${day.date}: ${day.values[metric].toLocaleString("ru-RU", { maximumFractionDigits: 2 })}`)); svg.append(point);
            });
        }
        let start;
        svg.addEventListener("pointerdown", event => { if (event.button !== 0) return; start = event.clientX; svg.setPointerCapture(event.pointerId); });
        svg.addEventListener("pointerup", event => { if (start == null) return; const delta = event.clientX - start; start = null; if (Math.abs(delta) > 20) shiftOverview(-Math.round(delta / svg.getBoundingClientRect().width * length)); });
        svg.addEventListener("pointercancel", () => { start = null; });
        svg.addEventListener("keydown", event => { if (["ArrowLeft", "ArrowRight"].includes(event.key)) { event.preventDefault(); shiftOverview(event.key === "ArrowLeft" ? -1 : 1); } });
        return t.div({ className: "push-chart-frame" }, svg);
    }
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
    run(load);
    const root = t.div({ className: "push-page", onunmount: () => { if (activeModal) app.modals.close(activeModal, true); clearTimeout(coverageTimer); coverageRevision++; overviewRevision++; analyticsRevision++; coverageWatcher.unwatch(); } }, t.div({ inert: () => s.modalOpen }, t.header(null, t.h1(null, "Пуши"), t.p(null, "Подготовьте сообщение, выберите аудиторию и запустите рассылку.")),
        t.nav({ className: "push-tabs", ariaLabel: "Разделы пушей" }, ...["Кампании", "Аудитории", "История", "Обзор", "Настройки"].map(tab => t.button({ type: "button", className: () => `btn ${s.tab === tab ? "" : "secondary"}`, ariaPressed: () => String(s.tab === tab), disabled: () => s.busy, onclick: () => { s.tab = tab; s.error = ""; s.notice = ""; s.preview = null; if (tab === "Обзор" && !s.overview && !s.overviewLoading) void loadOverview(); } }, tab))),
        () => !s.modalOpen && s.error ? t.div({ role: "alert", className: "alert alert-danger" }, s.error) : null,
        () => !s.modalOpen && s.notice ? t.div({ role: "status", className: "push-notice" }, s.notice) : null,
        () => !s.loaded ? t.div(null, t.p(null, s.error ? "Не удалось загрузить данные." : "Загрузка…"), s.error ? button("Повторить загрузку", load) : null) : s.tab === "Кампании" ? campaigns() : s.tab === "Аудитории" ? audiences() : s.tab === "История" ? history() : s.tab === "Обзор" ? overview() : settings()));
    return root;
}
