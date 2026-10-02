// PocketBase v0.40.4 UI extension; authorization is enforced on the server.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/partnerlinks/editor.css?v=12" }));
const settingsPath = "#/partner-links";
app.store.headerLinks = [...app.store.headerLinks, { label: "Партнёрские ссылки", href: settingsPath, icon: "ri-links-line" }];
app.routes.superuserOnly(settingsPath, () => partnerPage());
app.routes.superuserOnly("#/settings/partner-links", () => { location.replace(settingsPath + "?tab=settings"); return t.div(); });
// Redirect earlier query bookmarks without modifying the native settings shell.
document.addEventListener("mount:pageApplicationSettings", () => {
    if (app.utils.getHashQueryParams().plugin === "partnerlinks") location.replace(settingsPath);
});
// Use PocketBase's native read-only list and preview for server-owned orders.
// This also applies when the application does not install Schema Lock.
const plServerRecords = collection => collection?.name === "conversations";
watch(() => plServerRecords(app.store.activeCollection), locked => document.body.classList.toggle("pl-conversations-active", !!locked));
const plRecordUpsert = app.modals.openRecordUpsert;
app.modals.openRecordUpsert = function(collection, record, options) {
    if (!plServerRecords(collection)) return plRecordUpsert(collection, record, options);
    const id = typeof record === "string" ? record : record?.id;
    if (id) return app.modals.openRecordPreview({ id, collectionId: collection.id });
    app.toasts.error("Конверсии создаются и изменяются только сервером.");
};
const plRecordsList = app.components.recordsList;
app.components.recordsList = function(props = {}) {
    return plRecordsList({ ...props, collection: () => {
        const collection = typeof props.collection === "function" ? props.collection() : props.collection;
        return plServerRecords(collection) ? { ...collection, type: "view" } : collection;
    } });
};
const plCollectionUpsert = app.modals.openCollectionUpsert;
app.modals.openCollectionUpsert = function(collection, ...args) {
    if (plServerRecords(collection)) { app.toasts.error("Схема конверсий управляется сервером. Variants не используются."); return; }
    return plCollectionUpsert(collection, ...args);
};

// Keep the stored provider ID and server validation; only replace its editor.
const plTextInput = app.fieldTypes.text.input;
app.fieldTypes.text.input = function(props) {
    if (props.collection?.name !== "partner_links" || props.field.name !== "provider") return plTextInput(props);
    const uid = "pl-provider-" + app.utils.randomString();
    const state = store({ providers: [], loading: true, error: "" });
    async function load() {
        state.loading = true; state.error = "";
        try {
            const config = await app.pb.send("/api/partnerlinks/admin/config", { requestKey: null });
            state.providers = config.providers || [];
        } catch (error) {
            state.error = error?.response?.message || "Не удалось загрузить провайдеров.";
        } finally { state.loading = false; }
    }
    load();
    return t.div({ className: "record-field-input field-type-select pl-provider-input" },
        t.div({ className: "field" },
            t.label({ htmlFor: uid }, t.i({ className: app.fieldTypes.select.icon, ariaHidden: true }), t.span({ className: "txt" }, props.field.name)),
            () => app.components.select({
                id: uid, name: props.field.name, required: props.field.required,
                disabled: state.loading || !!state.error || !state.providers.length,
                placeholder: state.loading ? "Загрузка провайдеров…" : "Выберите провайдера",
                searchThreshold: 1,
                options: state.providers.map(provider => ({ value: provider.id, label: `${provider.name} (${provider.id})` })),
                value: () => props.record[props.field.name],
                onchange: options => { props.record[props.field.name] = options[0]?.value || ""; },
            })),
        () => state.error ? t.div({ className: "field-help", role: "alert" }, state.error, " ",
            t.button({ type: "button", className: "btn sm secondary", onclick: load }, "Повторить")) : null,
        () => !state.loading && !state.error && !state.providers.length
            ? t.div({ className: "field-help" }, "Сначала добавьте и сохраните провайдера в разделе «Партнёрские ссылки».") : null);
};

function partnerPage() {
    app.store.title = "Партнёрские ссылки";
    const tab = app.utils.getHashQueryParams().tab || "links";
    return t.div({ className: "page" }, t.div({ pbEvent: "pagePartnerLinks", className: "page-content" },
        t.header({ className: "page-header" }, t.nav({ className: "breadcrumbs" }, t.div({ className: "breadcrumb-item" }, "Партнёрские ссылки"))),
        t.div({ className: "wrapper" },
            t.nav({ className: "pl-tabs", ariaLabel: "Разделы партнёрских ссылок" },
                [["links", "Ссылки"], ["conversions", "Конверсии"], ["settings", "Настройки"]].map(([value, label]) =>
                    t.a({ href: settingsPath + (value === "links" ? "" : "?tab=" + value), className: `btn ${tab === value ? "" : "secondary"}`, ariaCurrent: tab === value ? "page" : undefined }, label))),
            tab === "settings" ? partnerSettingsContent() : tab === "conversions" ? partnerConversionCards() : partnerLinkCards())));
}

function partnerSettingsContent() {
    const s = store({ config: null, error: "", notice: "", busy: false });
    const run = async fn => { if (s.busy) return; s.busy = true; s.error = ""; try { await fn(); } catch (e) { s.error = e?.response?.message || e.message; } finally { s.busy = false; } };
    const load = async () => { s.config = await app.pb.send("/api/partnerlinks/admin/config", { requestKey: null }); };
    const locked = key => s.config.locks?.all || s.config.locks?.fields?.includes(key);
    const editableSecret = provider => !s.config.locks?.all && provider.preset === "rafinad_new" && !s.config.locks?.providerSecrets?.includes(provider.id);
    const field = (label, object, key, type = "text") => t.div({ className: "pl-field" }, t.label({ htmlFor: `pl-${object.id || key}` }, label), t.div({ className: "field" }, t.input({ id: `pl-${object.id || key}`, type, autocomplete: "off", value: () => object[key] || "", oninput: e => { object[key] = e.target.value; } })));
    run(load);
    return t.div({ className: "pl-settings" },
        () => s.error ? t.div({ role: "alert", className: "alert alert-danger" }, s.error) : null,
        () => s.notice ? t.p({ role: "status" }, s.notice) : null,
        () => !s.config ? t.div(null, "Загрузка…", s.error ? t.button({ className: "btn", onclick: () => run(load) }, "Повторить загрузку") : null) : t.form({ onsubmit: e => { e.preventDefault(); run(async () => {
            const { locks, presets, readiness, sharedAppMetrica, ...body } = JSON.parse(JSON.stringify(s.config));
            s.config = await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body, requestKey: null }); s.notice = "Настройки сохранены.";
        }); } },
            t.h2(null, "Готовность подключения"),
            s.config.readiness?.ready ? t.div({ className: "alert", role: "status" }, "✓ Все обязательные параметры заданы.") : t.div({ className: "alert", role: "status" }, (s.config.readiness?.missing || []).map(item => t.p(null, `⚠ ${item.message}`))),
            s.config.sharedAppMetrica ? t.p(null, t.a({ href: "#/appmetrica" }, "Настройки и проверка AppMetrica →")) : t.p({ className: "txt-hint" }, "Подключите общий модуль AppMetrica в коде проекта."),
            !locked("baseUrl") ? field("Публичный URL сервера", s.config, "baseUrl", "url") : null,
            s.config.providers.filter(editableSecret).map(provider => t.div(null, t.h3(null, provider.name), t.p({ className: "txt-hint" }, provider.hasSecret ? "✓ Секрет задан. Пустое поле сохраняет текущий." : "⚠ Укажите секрет постбека."), field("Секрет постбека", provider, "secret", "password"))),
            !locked("baseUrl") || s.config.providers.some(editableSecret) ? t.button({ className: "btn", type: "submit", disabled: () => s.busy }, "Сохранить настройки") : null,
            t.section({ className: "pl-postback-guides", ariaLabel: "Инструкции по настройке постбеков" },
                t.h2(null, "Настройка постбеков"),
                t.p({ className: "txt-hint" }, "Раскройте провайдера и перенесите параметры в его кабинет."),
                s.config.providers.map(provider => partnerPostbackGuide(s.config, provider)))));
}

function partnerPostbackGuide(config, provider) {
    const rafinad = provider.preset === "rafinad_new";
    const endpoint = (config.baseUrl || "https://YOUR_SERVER").replace(/\/+$/, "") + "/api/partnerlinks/postbacks/" + encodeURIComponent(provider.id);
    const local = !config.baseUrl || /^https?:\/\/(localhost\.?|127\.0\.0\.1|\[::1\])(?=[:/]|$)/i.test(config.baseUrl);
    const copy = async () => { try { await navigator.clipboard.writeText(endpoint); app.toasts.success("Адрес постбека скопирован."); } catch { app.toasts.error("Не удалось скопировать. Выделите адрес и скопируйте вручную."); } };
    const copySecret = async () => { try { await navigator.clipboard.writeText(provider.secret); app.toasts.success("Секрет скопирован."); } catch { app.toasts.error("Не удалось скопировать. Выделите секрет и скопируйте вручную."); } };
    const labels = { token: "Токен перехода", status: "Статус", leadId: "ID заявки", amount: "Комиссия", currency: "Валюта", timestamp: "Unix timestamp", eventId: "ID события" };
    const macros = { token: "{p_click_id}", status: "{status}", leadId: "{order_id}", amount: "{publisher_commission}", currency: "{currency}" };
    const statuses = { lead: "Заявка", hold: "На удержании", rejected: "Отклонена", approved: "Одобрена" };
    const table = (headers, rows) => t.table({ className: "pl-guide-table" }, t.thead(null, t.tr(null, headers.map(h => t.th({ scope: "col" }, h)))), t.tbody(null, rows.map(row => t.tr(null, row.map(value => t.td(null, value))))));
    return t.details({ className: "pl-panel pl-postback-guide" },
        t.summary(null, `${provider.name} — инструкция постбека`),
        t.div({ className: "pl-section" },
            t.p({ className: "txt-hint" }, `Провайдер: ${provider.id}. ${provider.hasSecret ? "✓ Секрет задан." : "⚠ Секрет не задан: настройте его перед подключением."}`),
            rafinad ? t.p(null, "Откройте new.rafinad.io → Инструменты → Постбеки → Создать.") : t.p(null, "Создайте постбек в кабинете провайдера. Параметры ниже соответствуют конфигурации вашего проекта."),
            t.div({ className: "pl-endpoint" }, t.strong(null, "Адрес постбека"), t.code(null, endpoint), t.button({ className: "btn secondary", type: "button", onclick: copy }, "Скопировать адрес")),
            local ? t.p({ className: "pl-example-note" }, "Сейчас указан локальный или примерный адрес. Для получения постбеков задайте публичный HTTPS URL сервера в конфигурации проекта.") : null,
            t.p(null, provider.secretLocation === "body" ? "Метод: POST. Параметры и секрет передаются в теле JSON или form-urlencoded." : "Метод: GET. Параметры передаются в строке запроса."),
            rafinad ? t.ol(null,
                t.li(null, "Укажите название и адрес постбека. Выберите оффер и источник или включите глобальный постбек. Не дублируйте отправку на один адрес."),
                t.li(null, "Включите все нужные статусы и укажите их значения из таблицы ниже."),
                t.li(null, "В разделе «Параметры» добавьте имена и макросы из таблицы. Секрет передайте отдельно, как описано ниже.")) : null,
            t.h3(null, "Параметры"),
            table(["Имя параметра", rafinad ? "Макрос Rafinad" : "Значение"], Object.entries(provider.fields || {}).filter(([, name]) => name).map(([key, name]) => [t.code(null, name), rafinad && macros[key] ? t.code(null, macros[key]) : labels[key] || key])),
            t.h3(null, "Статусы"),
            rafinad ? t.p(null, "Статусы кабинета: «В ожидании» → Заявка, «В холде» → На удержании, «Отклонено» → Отклонена, «Одобрено» → Одобрена. Числовые значения задайте по таблице.") : null,
            table(["Значение провайдера", "Статус конверсии"], Object.entries(provider.statuses || {}).map(([value, status]) => [t.code(null, value), statuses[status] || status])),
            t.h3(null, "Секрет постбека"),
            t.p(null, provider.secretLocation === "header" ? "Передайте секрет в HTTP-заголовке " : provider.secretLocation === "body" ? "Передайте секрет в поле тела запроса " : rafinad ? "В разделе «Константы» добавьте параметр " : "Добавьте постоянный параметр строки запроса ", t.code(null, provider.secretName), ". Значение — секрет этого провайдера из конфигурации сервера."),
            provider.secret ? t.div({ className: "pl-endpoint pl-postback-secret" }, t.code(null, provider.secret), t.button({ className: "btn secondary", type: "button", onclick: copySecret }, "Скопировать секрет")) : null,
            t.p({ className: "txt-hint" }, "Скопируйте этот секрет в кабинет провайдера. Если секрет не задан в коде, сервер создаёт и сохраняет его автоматически; при перезапуске он остаётся прежним."),
            t.h3(null, "Проверка"),
            t.p(null, rafinad ? "Сохраните постбек. В партнёрской ссылке выберите этого провайдера и укажите исходную ссылку потока. Сервер сам добавит p_click_id при переходе." : "Сохраните постбек и откройте партнёрскую ссылку из приложения."),
            t.p(null, "Для проверки используйте токен настоящего перехода. Произвольный тестовый токен не создаёт конверсию. Результат смотрите во вкладке «Конверсии»; для отправки события должна быть готова AppMetrica."),
            t.p({ className: "txt-hint" }, provider.sendRevenue ? `Передача дохода включена для статуса ${statuses[provider.revenueStatus] || provider.revenueStatus}. Сумма — комиссия, а не сумма займа.` : "Передача дохода в AppMetrica выключена. Статусы конверсий передаются как события.")));
}

function partnerConversionCards() {
    const state = store({ items: [], loading: true, error: "", query: "", status: "", page: 1, totalPages: 1, total: 0 });
    const statuses = { pending: "Ожидает конверсии", lead: "Заявка", approved: "Одобрена", hold: "На удержании", rejected: "Отклонена" };
    const uid = "pl-conversions-" + app.utils.randomString();
    let generation = 0, timer;
    async function load() {
        const current = ++generation;
        state.loading = true; state.error = "";
        const filters = [];
        if (state.query.trim()) filters.push(app.pb.filter("(id ~ {:q} || leadId ~ {:q} || clickId ~ {:q} || provider ~ {:q} || userId ~ {:q} || link.name ~ {:q})", { q: state.query.trim() }));
        if (state.status) filters.push(app.pb.filter("status = {:status}", { status: state.status }));
        try {
            const result = await app.pb.collection("conversations").getList(state.page, 24, {
                sort: "-created,-id", expand: "link", requestKey: null, filter: filters.join(" && "),
                fields: "id,collectionId,provider,leadId,clickId,userId,status,amount,currency,created,link,expand.link.name",
            });
            if (current !== generation) return;
            state.items = result.items; state.totalPages = result.totalPages; state.total = result.totalItems;
        } catch (error) { if (current === generation) state.error = error?.response?.message || "Не удалось загрузить конверсии."; }
        finally { if (current === generation) state.loading = false; }
    }
    function search() {
        // Invalidate in-flight results immediately, including the debounce window.
        generation++; state.loading = true; state.page = 1;
        clearTimeout(timer); timer = setTimeout(load, 200);
    }
    const date = value => { const parsed = new Date(value); return Number.isNaN(parsed.getTime()) ? "Дата не указана" : parsed.toLocaleString("ru-RU"); };
    return t.section({ className: "pl-links pl-conversions", ariaLabel: "Конверсии", onmount: load, onunmount: () => { generation++; clearTimeout(timer); } },
        t.div({ className: "pl-links-heading" }, t.h2(null, "Конверсии"), t.button({ type: "button", className: "btn secondary", disabled: () => state.loading, onclick: load }, "Обновить")),
        t.p({ className: "txt-hint" }, "Состояние заявок и переходов по партнёрским ссылкам. Нажмите на карточку, чтобы посмотреть подробности."),
        t.div({ className: "pl-grid" },
            t.div({ className: "field" }, t.label({ htmlFor: uid }, "Поиск конверсий"), t.input({ id: uid, type: "search", placeholder: "Ссылка, провайдер, ID заявки, клика или пользователя", value: () => state.query, oninput: e => { state.query = e.target.value; search(); } })),
            t.div({ className: "field" }, t.label({ htmlFor: uid + "-status" }, "Статус конверсии"), app.components.select({
                id: uid + "-status", options: [{ value: "", label: "Все статусы" }, ...Object.entries(statuses).map(([value, label]) => ({ value, label }))],
                value: () => state.status, onchange: options => { state.status = options[0]?.value || ""; search(); },
            }))),
        () => state.error ? t.div({ role: "alert" }, state.error, t.button({ type: "button", className: "btn secondary", onclick: load }, "Повторить")) : null,
        t.p({ role: "status", hidden: () => !state.loading }, "Загрузка конверсий…"),
        t.p({ className: "txt-hint", hidden: () => state.loading || !!state.error || state.items.length > 0 }, () => state.query.trim() || state.status ? "По выбранным условиям конверсий не найдено." : "Конверсий пока нет. Они появятся после выдачи партнёрских ссылок пользователям."),
        t.div({ className: "pl-link-cards", "html-aria-busy": () => state.loading, hidden: () => state.loading || !!state.error }, () => state.items.map(record =>
            t.button({ type: "button", className: "pl-link-card pl-conversion-card", ariaLabel: "Конверсия: " + record.id, onclick: () => app.modals.openRecordPreview({ id: record.id, collectionId: record.collectionId }) },
                t.div({ className: "pl-links-heading" }, t.strong(null, record.expand?.link?.name || "Партнёрская ссылка"), t.span({ className: record.status === "approved" ? "txt-success" : "txt-hint" }, statuses[record.status] || record.status)),
                t.span(null, record.amount ? `Сумма: ${record.amount} ${record.currency || ""}`.trim() : "Сумма не указана"),
                t.span({ className: "txt-hint" }, "Провайдер: " + record.provider),
                t.span(null, record.leadId ? "Заявка: " + record.leadId : "Клик: " + record.clickId),
                t.small({ className: "txt-hint" }, "Пользователь: " + record.userId),
                t.small({ className: "txt-hint" }, date(record.created)),
                t.small({ className: "txt-hint" }, "ID: " + record.id)))),
        t.div({ className: "pl-links-heading pl-link-pagination" },
            t.span({ className: "txt-hint" }, () => `Всего: ${state.total}`),
            t.div({ className: "pl-links-heading" },
                t.button({ type: "button", className: "btn secondary", disabled: () => state.loading || !!state.error || state.page <= 1, onclick: () => { state.page--; load(); } }, "Назад"),
                t.span(null, () => `${state.page} / ${Math.max(1, state.totalPages)}`),
                t.button({ type: "button", className: "btn secondary", disabled: () => state.loading || !!state.error || state.page >= state.totalPages, onclick: () => { state.page++; load(); } }, "Далее"))));
}

function partnerLinkCards() {
    const state = store({ items: [], loading: true, error: "", query: "", page: 1, totalPages: 1, total: 0 });
    let generation = 0, timer;
    const collection = () => app.store.collections.find(c => c.name === "partner_links");
    async function load() {
        const current = ++generation;
        state.loading = true; state.error = "";
        try {
            const result = await app.pb.collection("partner_links").getList(state.page, 24, {
                sort: "name,id", expand: "content_set", requestKey: null,
                filter: state.query.trim() ? app.pb.filter("name ~ {:q} || provider ~ {:q}", { q: state.query.trim() }) : "",
            });
            if (current !== generation) return;
            state.items = result.items; state.totalPages = result.totalPages; state.total = result.totalItems;
        } catch (error) { if (current === generation) state.error = error?.response?.message || "Не удалось загрузить ссылки"; }
        finally { if (current === generation) state.loading = false; }
    }
    const edit = record => app.modals.openRecordUpsert(collection(), record, { onsave: load, ondelete: () => { state.page = 1; load(); } });
    const uid = "pl-search-" + app.utils.randomString();
    return t.section({ className: "pl-links", ariaLabel: "Ссылки", onmount: load, onunmount: () => { generation++; clearTimeout(timer); } },
        t.div({ className: "pl-links-heading" }, t.h2(null, "Ссылки"), t.button({ type: "button", className: "btn", disabled: () => !collection(), onclick: () => edit(null) }, "Добавить ссылку")),
        t.div({ className: "field pl-links-search" }, t.label({ htmlFor: uid }, "Поиск ссылок"), t.input({ id: uid, type: "search", placeholder: "Название или провайдер", value: () => state.query, oninput: e => { state.query = e.target.value; state.page = 1; clearTimeout(timer); timer = setTimeout(load, 200); } })),
        () => state.error ? t.div({ role: "alert" }, state.error, t.button({ type: "button", className: "btn secondary", onclick: load }, "Повторить")) : null,
        t.p({ role: "status", hidden: () => !state.loading }, "Загрузка ссылок…"),
        t.p({ className: "txt-hint", hidden: () => state.loading || !!state.error || state.items.length > 0 }, "Ссылок пока нет или ничего не найдено."),
        t.div({ className: "pl-link-cards", "html-aria-busy": () => state.loading }, () => state.items.map(record =>
            t.button({ type: "button", className: "pl-link-card", onclick: () => edit(record), ariaLabel: "Редактировать: " + record.name },
                t.div({ className: "pl-links-heading" }, t.strong(null, record.name), t.span({ className: record.active ? "txt-success" : "txt-hint" }, record.active ? "Активна" : "Выключена")),
                t.span({ className: "txt-hint" }, record.provider + (record.link?.category ? " · " + record.link.category : " · Общие настройки")),
                t.span({ className: "pl-link-url" }, record.link?.url || "URL не задан"),
                record.expand?.content_set ? t.small({ className: "txt-hint" }, [record.expand.content_set.variant, record.expand.content_set.experiment, record.expand.content_set.group].filter(Boolean).join(" / ")) : null))),
        t.div({ className: "pl-links-heading pl-link-pagination" },
            t.span({ className: "txt-hint" }, () => `Всего: ${state.total}`),
            t.div({ className: "pl-links-heading" },
                t.button({ type: "button", className: "btn secondary", disabled: () => state.loading || state.page <= 1, onclick: () => { state.page--; load(); } }, "Назад"),
                t.span(null, () => `${state.page} / ${Math.max(1, state.totalPages)}`),
                t.button({ type: "button", className: "btn secondary", disabled: () => state.loading || state.page >= state.totalPages, onclick: () => { state.page++; load(); } }, "Далее"))));
}
