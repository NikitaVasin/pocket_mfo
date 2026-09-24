// PocketBase v0.40.4 UI extension; authorization is enforced on the server.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/partnerlinks/editor.css?v=10" }));
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
    const settings = app.utils.getHashQueryParams().tab === "settings";
    return t.div({ className: "page" }, t.div({ pbEvent: "pagePartnerLinks", className: "page-content" },
        t.header({ className: "page-header" }, t.nav({ className: "breadcrumbs" }, t.div({ className: "breadcrumb-item" }, "Партнёрские ссылки"))),
        t.div({ className: "wrapper" },
            t.nav({ className: "pl-tabs", ariaLabel: "Разделы партнёрских ссылок" },
                t.a({ href: settingsPath, className: `btn ${settings ? "secondary" : ""}`, ariaCurrent: settings ? undefined : "page" }, "Ссылки"),
                t.a({ href: settingsPath + "?tab=settings", className: `btn ${settings ? "" : "secondary"}`, ariaCurrent: settings ? "page" : undefined }, "Настройки")),
            settings ? partnerSettingsContent() : partnerLinkCards())));
}

function partnerSettingsContent() {
    const state = store({ config: null, error: "", notice: "", loading: true, saving: false });
    let sequence = 0;
    const expandedProviders = new Map();
    const statusChoices = [["lead", "Заявка создана (лид)"], ["approved", "Подтверждение"], ["hold", "Холд / ожидание"], ["rejected", "Отказ"]];
    const eventLabels = { click: "Клик по офферу", lead: "Заявка создана (лид)", approved: "Подтверждение", hold: "Холд / ожидание", rejected: "Отказ" };
    const button = (label, action, secondary = true) => t.button({ type: "button", className: `btn ${secondary ? "secondary" : ""}`, disabled: () => state.saving || !!state.config?.locks?.all, onclick: action }, label);
    const message = error => error?.response?.message || error?.message || "Не удалось выполнить запрос";
    function field(label, object, key, { type = "text", hint = "", required = false, choices = null, placeholder = "" } = {}) {
        const locked = !!state.config?.locks?.all ||
            (object === state.config && state.config.locks?.fields?.includes(key)) ||
            (object === state.config?.eventNames && state.config.locks?.eventNames?.includes(key));
        if (locked) {
            const original = hint;
            hint = () => `Задано в Go-коде. ${typeof original === "function" ? original() : original}`;
        }
        const id = `pl-input-${++sequence}`;
        const update = value => { object[key] = type === "number" ? Number(value) : value; };
        const control = choices
            ? app.components.select({ id, required: true, disabled: locked, value: () => object[key], options: choices.map(([value, label]) => ({ value, label })), onchange: options => update(options[0]?.value || "") })
            : t.input({ id, type, required, disabled: locked, ariaDescribedby: hint ? `${id}-help` : undefined, placeholder, min: type === "number" ? 0 : undefined, autocomplete: type === "password" ? "new-password" : "off", value: () => object[key] ?? "", oninput: event => update(event.target.value) });
        return t.div({ className: "pl-field" }, t.label({ className: "pl-label", htmlFor: id }, label), t.div({ className: "field pl-control" }, control),
            hint ? t.div({ id: `${id}-help`, className: "field-help pl-wrap" }, hint) : null);
    }
    function prepare(config) {
        for (const provider of config.providers) {
            provider.revenueStatus ||= "approved";
            provider._statusRows = Object.entries(provider.statuses).map(([from, to]) => ({ from, to }));
            provider._extraRows = Object.entries(provider.extraFields || {}).map(([from, to]) => ({ from, to }));
        }
        return config;
    }
    function rowMap(rows, label) {
        const keys = new Set();
        for (const row of rows) {
            if (!row.from.trim() || !row.to.trim()) throw new Error(`${label}: заполните обе колонки или удалите пустую строку.`);
            if (keys.has(row.from)) throw new Error(`${label}: значение «${row.from}» повторяется. Для него должна быть одна строка.`);
            keys.add(row.from);
        }
        return Object.fromEntries(rows.map(row => [row.from, row.to]));
    }
    function serialize() {
        const copy = JSON.parse(JSON.stringify(state.config));
        delete copy.locks; delete copy.presets;
        copy.providers = copy.providers.map(({ _statusRows, _extraRows, ...provider }) => ({ ...provider,
            statuses: rowMap(_statusRows, "Соответствие статусов"), extraFields: rowMap(_extraRows, "Дополнительные параметры") }));
        return copy;
    }
    async function load() {
        state.loading = true; state.error = "";
        try {
            const config = await app.pb.send("/api/partnerlinks/admin/config");
            state.config = prepare(config);
        }
        catch (error) { state.error = message(error); }
        finally { state.loading = false; }
    }
    async function saveSettings(event) {
        event.preventDefault(); if (state.saving || state.config.locks?.all) return;
        state.saving = true; state.error = ""; state.notice = "";
        try {
            state.config = prepare(await app.pb.send("/api/partnerlinks/admin/config", { method: "PUT", body: serialize() }));
            state.notice = "Настройки сохранены.";
        } catch (error) { state.error = message(error); }
        finally { state.saving = false; }
    }
    function newProvider() {
        expandedProviders.set(state.config.providers.length, true);
        state.config.providers.push({ id: "", name: "", revenueStatus: "approved", urlTemplate: "{url}?subid={clickData}", maxTokenLength: 0, secret: "", secretLocation: "query", secretName: "secret",
            fields: { token: "subid", status: "status", leadId: "lead_id", eventId: "", timestamp: "", amount: "", currency: "" },
            statuses: {}, extraFields: {}, _statusRows: statusChoices.map(([key]) => ({ from: key, to: key })), _extraRows: [] });
    }
    function mappings(provider) {
        return t.section({ className: "pl-section pl-status-mapping" },
            t.h4(null, "Соответствие статусов"),
            t.p({ className: "txt-hint" }, "Здесь вы переводите значения партнёра в события приложения. Слева — точное значение из поля статуса постбека, справа — что оно означает."),
            t.p({ className: "pl-example-note" }, "Например, партнёр присылает status=1 при одобрении заявки. Добавьте строку «1 → Подтверждение». Тогда сервер отправит событие подтверждения в AppMetrica."),
            t.div({ className: "pl-mapping-list" }, () => provider._statusRows.map((row, index) => t.div({ className: "pl-mapping-row" },
                field("Статус партнёра", row, "from", { required: true, placeholder: "Например: 1 или approved" }),
                field("Значение статуса", row, "to", { choices: statusChoices, hint: () => `Событие AppMetrica: ${state.config.eventNames[row.to] || row.to}` }),
                t.button({ type: "button", className: "btn secondary pl-remove", title: "Удалить соответствие", ariaLabel: `Удалить соответствие ${index + 1}`, disabled: () => state.saving, onclick: () => provider._statusRows.splice(index, 1) }, t.i({ className: "ri-delete-bin-line", ariaHidden: true }))))),
            button("Добавить соответствие", () => provider._statusRows.push({ from: "", to: "lead" })),
            t.p({ className: "txt-hint" }, "Можно сопоставить несколько значений одному событию. Неизвестный статус отклоняется. Подтверждение, холд и отказ не создают дополнительный лид: для лида нужен отдельный постбек."));
    }
    function extras(provider) {
        return t.section({ className: "pl-section pl-extra-mapping" },
            t.h4(null, "Дополнительные параметры"),
            t.p({ className: "txt-hint" }, "Необязательно. Укажите, какие ещё поля партнёра передавать в параметры конверсии. Например: имя offerId, поле data.offer_id. Секрет сюда добавлять нельзя."),
            t.div({ className: "pl-mapping-list" }, () => provider._extraRows.map((row, index) => t.div({ className: "pl-mapping-row" },
                field("Имя параметра в конверсии", row, "from", { required: true, placeholder: "offerId" }),
                field("Поле партнёра", row, "to", { required: true, placeholder: "data.offer_id" }),
                t.button({ type: "button", className: "btn secondary pl-remove", title: "Удалить параметр", ariaLabel: `Удалить параметр ${index + 1}`, disabled: () => state.saving, onclick: () => provider._extraRows.splice(index, 1) }, t.i({ className: "ri-delete-bin-line", ariaHidden: true }))))),
            button("Добавить параметр", () => provider._extraRows.push({ from: "", to: "" })));
    }
    function applyPreset(provider, index, presetID) {
        const preset = state.config.presets?.find(p => p.id === presetID);
        if (!preset) { provider.preset = ""; return; }
        const next = JSON.parse(JSON.stringify(preset.provider));
        next.id = provider.id;
        next.name = provider.name || preset.name;
        next.secret = provider.secret || Array.from(crypto.getRandomValues(new Uint8Array(24)), byte => byte.toString(16).padStart(2, "0")).join("");
        prepare({ providers: [next] });
        state.config.providers[index] = next;
    }
    function presetSelector(provider, index) {
        const id = `pl-input-${++sequence}`;
        return t.div({ className: "pl-field" }, t.label({ className: "pl-label", htmlFor: id }, "Пресет партнёра"),
            t.div({ className: "field pl-control" }, app.components.select({ id, required: true, value: () => provider.preset || "", placeholder: "Ручная настройка",
                options: [{ value: "", label: "Ручная настройка" }, ...(state.config.presets || []).map(preset => ({ value: preset.id, label: preset.name }))],
                onchange: options => applyPreset(provider, index, options[0]?.value || "") })),
            t.p({ className: "field-help" }, "Выбор пресета заменяет шаблон, поля и статусы. ID, название и введённый секрет сохраняются."));
    }
    function presetGuide(provider) {
        if (provider.preset !== "rafinad_new") return null;
        const standard = state.config.presets?.find(p => p.id === provider.preset)?.provider;
        const isStandard = standard && ["urlTemplate", "secretLocation", "secretName"].every(key => provider[key] === standard[key]) &&
            Object.keys(standard.fields).every(key => provider.fields[key] === standard.fields[key]) &&
            provider._statusRows.length === 4 && provider._statusRows.every(row => standard.statuses[row.from] === row.to) && !provider._extraRows.length;
        if (!isStandard) return t.section({ className: "pl-section pl-preset-guide pl-example-note" },
            t.h4(null, "Настройка Rafinad New"), t.p(null, "Поля пресета изменены. Для стандартной инструкции выберите «Ручная настройка», затем снова «Rafinad New». Либо настройте постбек в кабинете под изменённые поля."));
        const endpoint = `${state.config.baseUrl || location.origin}/api/partnerlinks/postbacks/${provider.id || "PROVIDER_ID"}`;
        return t.section({ className: "pl-section pl-preset-guide pl-example-note" },
            t.h4(null, "Настройка Rafinad New"),
            t.ol(null,
                t.li(null, "Заполните ID провайдера и публичный URL сервера, затем сохраните настройки. Секрет уже создан; его можно заменить перед сохранением."),
                t.li(null, "Откройте new.rafinad.io → Инструменты → Постбеки → Создать. Укажите понятное название, выберите метод GET. В «Ссылка постбека» вставьте:", t.div({ className: "pl-endpoint" }, t.code(null, endpoint))),
                t.li(null, "В «Фильтрация» выберите нужные оффер и источник. Для всех офферов включите «Глобальный постбек». Если он должен отправляться также при наличии отдельного постбека оффера, включите «Всегда отправлять глобальный постбек». Не настраивайте два постбека на один и тот же адрес — это даст повторные события."),
                t.li(null, "Отметьте все четыре статуса конверсии. В «Маппинг статусов отправки» задайте: В ожидании = 1, В холде = 2, Отклонено = 3, Одобрено = 4. Здесь 1 создаёт лид, 2 — холд, 3 — отказ, 4 — подтверждение."),
                t.li(null, "В «Параметры» добавьте строки: слева имя параметра, справа соответствующий макрос Rafinad из списка.",
                    t.table({ className: "pl-preset-table" }, t.thead(null, t.tr(null, t.th(null, "Имя параметра"), t.th(null, "Макрос Rafinad"))),
                        t.tbody(null, ["p_click_id", "status", "order_id", "publisher_commission", "currency"].map(key => t.tr(null, t.td(null, t.code(null, key)), t.td(null, t.code(null, `{${key}}`))))))),
                t.li(null, "В «Константы» добавьте имя secret и его значение:", t.div({ className: "pl-endpoint" }, t.code(null, provider.secret || "Сначала задайте секрет постбека"))),
                t.li(null, "Сохраните постбек в Rafinad. Во вкладке «Ссылки» выберите этого провайдера и вставьте исходную ссылку потока Rafinad в поле link. Параметр p_click_id с токеном сервер добавит сам при переходе."),
                t.li(null, "Проверьте переход из приложения и постбек по полученному p_click_id: ответ 200 означает успешную отправку события в AppMetrica. Произвольный тестовый токен не подойдёт — сначала нужна выданная приложению ссылка.")),
            t.p(null, "publisher_commission — ваша комиссия, order_total — сумма заказа и здесь не используется. Даты Rafinad имеют строковый формат, поэтому время берётся при получении постбека. Revenue по умолчанию выключен; при необходимости включите его ниже и выберите статус начисления."));
    }
    function providerEditor(provider, index) {
        return t.details({ className: "pl-panel pl-provider", open: expandedProviders.get(index) || false, ontoggle: event => expandedProviders.set(index, event.target.open) },
            t.summary(null, () => provider.name || "Новый провайдер"),
            t.fieldset({ className: "pl-provider-body", disabled: !!state.config.locks?.all || state.config.locks?.providers?.includes(provider.id) },
                state.config.locks?.providers?.includes(provider.id) ? t.p({ className: "pl-managed-note" }, "Провайдер задан в Go-коде. Доступен только просмотр.") : null,
                presetSelector(provider, index),
                () => presetGuide(provider),
                t.section({ className: "pl-section" }, t.h4(null, "1. Ссылка партнёра"),
                    t.div({ className: "pl-grid" },
                        field("Название провайдера", provider, "name", { required: true, hint: "Удобное название партнёрской сети для вас." }),
                        field("ID провайдера", provider, "id", { required: true, hint: "Буквы, цифры, дефис или подчёркивание. В поле provider коллекции partner_links выберите этого провайдера из списка. После подключения не меняйте." }),
                        field("Шаблон ссылки", provider, "urlTemplate", { required: true, hint: "{url} — исходная ссылка из коллекции. {clickData} — случайный токен конверсии (43 символа). Если партнёр принимает их в subid, используйте {url}?subid={clickData}. Остальные query-параметры сохраняются." }),
                        field("Лимит длины clickData", provider, "maxTokenLength", { type: "number", hint: "Уточните максимальную длину subid у партнёра. 0 — без дополнительного лимита партнёра; серверный предел 32768 байт." }))),
                t.section({ className: "pl-section" }, t.h4(null, "2. Доступ к постбеку"),
                    t.p({ className: "txt-hint" }, "Постбек — запрос сервера партнёра после создания заявки или изменения её статуса. Передайте партнёру адрес ниже и отдельный секрет. Секрет не должен попадать в ссылку приложения."),
                    t.div({ className: "pl-endpoint" }, t.span(null, "Адрес постбека"), t.code(null, () => `${state.config.baseUrl || location.origin}/api/partnerlinks/postbacks/${provider.id || "PROVIDER_ID"}`)),
                    t.div({ className: "pl-grid" },
                        field("Секрет постбека", provider, "secret", { hint: "Секрет виден суперпользователям. Скопируйте его в кабинет партнёра. Пустое поле при сохранении оставляет текущий секрет. Минимум 16 символов." }),
                        field("Где передавать секрет", provider, "secretLocation", { choices: [["query", "Параметр URL (GET / POST)"], ["header", "HTTP-заголовок (GET / POST)"], ["body", "Тело запроса (POST JSON / form)"]], hint: "Выберите способ, который поддерживает партнёр. Сервер проверяет секрет только в выбранном месте." }),
                        field("Имя параметра / заголовка секрета", provider, "secretName", { required: true, hint: "URL или form: secret. Заголовок: X-Partner-Secret. JSON: secret или вложенный путь auth.secret. Здесь указывается имя поля, а не значение секрета." }))),
                t.section({ className: "pl-section pl-postback-fields" }, t.h4(null, "3. Поля постбека"),
                    t.p({ className: "txt-hint" }, "Укажите имена полей из документации партнёра, а не их значения. Для GET это параметры URL, для POST — поля form или JSON. Вложенное поле JSON задаётся через точку: data.status. Необязательное поле можно оставить пустым."),
                    t.div({ className: "pl-grid" },
                        field("clickData", provider.fields, "token", { required: true, hint: "Поле, в котором партнёр возвращает токен из ссылки без изменений. Например subid или data.subid." }),
                        field("Статус", provider.fields, "status", { required: true, hint: "Имя поля со статусом, например status. Что означают его значения, задайте ниже в «Соответствии статусов»." }),
                        field("ID заявки", provider.fields, "leadId", { hint: "Например lead_id. Идентификатор закрепляется за выданной ссылкой: заменить его другой заявкой нельзя. Для Revenue обязателен." }),
                        field("ID события", provider.fields, "eventId", { hint: "Если партнёр передаёт отдельный ID уведомления. Сам по себе не исключает дубли." }),
                        field("Время события (Unix, секунды)", provider.fields, "timestamp", { hint: "Например timestamp. Пусто — время получения постбека. Принимаются события за последние 14 дней." }),
                        field("Сумма", provider.fields, "amount", { hint: "Например payout. Это ваш доход от партнёра. Передача в Revenue настраивается ниже." }),
                        field("Валюта", provider.fields, "currency", { hint: "Имя поля, содержащего RUB, USD и т. п., например currency." }))),
                mappings(provider),
                t.section({ className: "pl-section pl-revenue" }, t.h4(null, "Доходы в AppMetrica"),
                    t.label(null, t.input({ type: "checkbox", checked: () => !!provider.sendRevenue, onchange: event => provider.sendRevenue = event.target.checked }), " Передавать доход в Revenue"),
                    field("Когда начислять Revenue", provider, "revenueStatus", { choices: [["approved", "Подтверждение (апрув)"], ["hold", "Холд / ожидание"]], hint: "Выберите один статус из «Соответствия статусов». Если доход учитывается на холде, последующее подтверждение не отправляет Revenue. По умолчанию — подтверждение." }),
                    t.p({ className: "txt-hint" }, "При включённой передаче Revenue отправляется только для выбранного статуса. Настройте ID заявки, сумму и валюту. Сумма — ваш доход от партнёра, не сумма займа. Валюта — RUB, USD и т. п. Доходы появятся в отчётах монетизации."),
                    t.p({ className: "txt-hint" }, () => `Для воронки используйте ${state.config.eventNames.click} → ${state.config.eventNames.lead} → ${state.config.eventNames[provider.revenueStatus]}.`),
                    t.p({ className: "txt-hint" }, "Подтверждённый Revenue повторно не начисляется, в том числе после смены выбранного статуса. Отказ не начисляет доход и не отменяет уже переданный Revenue. При неизвестном результате отправки нужна сверка с AppMetrica.")), extras(provider),
                t.details({ className: "pl-examples" }, t.summary(null, "Примеры постбеков: URL, заголовок и body"),
                    t.p(null, "Примеры ниже используют поля subid, status и lead_id. YOUR_SECRET — секрет, известный только вам и партнёру; CLICK_DATA — токен, полученный партнёром из ссылки."),
                    t.p(null, "Секрет в URL: выберите «Параметр URL», имя secret."),
                    t.pre(null, t.code(null, "GET /api/partnerlinks/postbacks/PROVIDER_ID?secret=YOUR_SECRET&subid=CLICK_DATA&status=1&lead_id=123")),
                    t.p(null, "Секрет в заголовке: выберите «HTTP-заголовок», имя X-Partner-Secret. Остальные поля передайте в query для GET или в body для POST."),
                    t.pre(null, t.code(null, 'POST /api/partnerlinks/postbacks/PROVIDER_ID\nX-Partner-Secret: YOUR_SECRET\nContent-Type: application/json\n\n{"subid":"CLICK_DATA","status":"1","lead_id":"123"}')),
                    t.p(null, "Секрет в JSON: выберите «Тело запроса», имя auth.secret. В примере статус 1 нужно сопоставить с подтверждением."),
                    t.pre(null, t.code(null, 'POST /api/partnerlinks/postbacks/PROVIDER_ID\nContent-Type: application/json\n\n{\n  "auth": {"secret": "YOUR_SECRET"},\n  "subid": "CLICK_DATA",\n  "status": "1",\n  "lead_id": "123"\n}')),
                    t.p(null, "Для form выберите «Тело запроса», имя secret и Content-Type: application/x-www-form-urlencoded."),
                    t.pre(null, t.code(null, "secret=YOUR_SECRET&subid=CLICK_DATA&status=1&lead_id=123"))),
                button("Убрать провайдера", () => { state.config.providers.splice(index, 1); expandedProviders.clear(); })));
    }
    function guide() {
        return t.details({ className: "pl-panel pl-guide" }, t.summary(null, "Как настроить интеграцию — по шагам"),
            t.ol(null,
                t.li(null, "В AppMetrica откройте настройки нужного приложения. Скопируйте числовой Application ID и Post API key в поля ниже. Это ключ для серверной загрузки событий, а не SDK API key."),
                t.li(null, "Укажите публичный HTTPS-адрес этого сервера, доступный приложению и партнёру. При выдаче ссылки сервер сохраняет данные клика в conversations. Партнёр получает только случайный токен clickData и возвращает его в постбеке."),
                t.li(null, "Добавьте провайдера. По его документации настройте параметр передачи clickData, имена полей постбека и значения статусов. Передайте партнёру адрес постбека и секрет."),
                t.li(null, "Сохраните настройки. Нажмите «Добавить ссылку» над карточками: name — название, provider — выберите сохранённого провайдера из списка, active — включено. В поле link (Dynamic link) укажите исходный URL и при необходимости категорию. По умолчанию открывается внешний браузер; поведение категории задаётся в Dynamic Link."),
                t.li(null, "AppMetrica profileId всегда равен ID пользователя PocketBase. Передайте user.id в SDK до активации; отдельное поле пользователя не требуется. Приложение запрашивает ссылку только по ID записи partner_links; сервер использует ID авторизованного пользователя. При переходе отправляется клик. Для заявки и дальнейших статусов партнёр присылает отдельные постбеки.")),
            t.p({ className: "txt-hint" }, "200 в ответе постбека означает подтверждённый приём загрузки AppMetrica, включая уже обработанный повтор. Подтверждённые события и Revenue повторно не отправляются. При 502 повтор поможет после явного отказа; при таймауте или обрыве связи сначала нужна сверка результата."));
    }
    load();
    return t.div({ className: "pl-settings" },
                () => state.error ? t.div({ role: "alert", className: "alert alert-danger pl-wrap" }, state.error) : null,
                () => state.notice ? t.div({ role: "status", className: "alert" }, state.notice) : null,
                guide(),
                () => state.loading ? t.p({ role: "status" }, "Загрузка…") : null,
                () => !state.config && !state.loading ? button("Повторить загрузку", load) : null,
                () => state.config ? t.form({ onsubmit: saveSettings },
                    state.config.locks?.all ? t.div({ className: "alert pl-managed-note", role: "status" }, "Настройки доступны только для просмотра. Изменения разрешены только из Go-кода.") : null,
                    t.h2(null, "AppMetrica"), t.p({ className: "txt-hint" }, "Здесь настраиваются события и провайдеры. Партнёрские ссылки находятся во вкладке «Ссылки»."),
                    t.div({ className: "pl-grid" },
                        field("Публичный URL сервера", state.config, "baseUrl", { type: "url", hint: "Например https://api.example.com — без /api и /_/. Из него формируются адреса переходов и постбеков." }),
                        field("Application ID", state.config, "applicationId", { type: "number", hint: "Числовой идентификатор приложения в AppMetrica, например 1234567." }),
                        field("Post API key", state.config, "postApiKey", { type: "password", hint: state.config.hasPostApiKey ? "Ключ сохранён. Пустое поле сохраняет текущий." : "Ключ загрузки событий из настроек AppMetrica. Не путайте с ключом SDK или секретом партнёра." }),
                        field("Срок открытия ссылки, секунд", state.config, "openTtlSeconds", { type: "number", required: true, hint: "86400 = 1 сутки после выдачи. Позже приложение должно запросить новую ссылку." }),
                        field("Без заявки: хранить дней", state.config, "pendingRetentionDays", { type: "number", required: true, hint: "По умолчанию 14 дней от выдачи ссылки. Это записи pending, по которым ещё не было ни одного статуса от партнёра. 0 — хранить бессрочно." }),
                        field("С заявкой: хранить дней", state.config, "conversionRetentionDays", { type: "number", required: true, hint: "По умолчанию 0 — бессрочно. Другой срок считается от последнего изменения статуса. Lead, холд, подтверждение и отказ относятся к этой группе." })),
                    t.p({ className: "txt-hint pl-retention-help" }, "Одна выданная ссылка — одна конверсия. Очистка запускается каждый час; просроченные записи уже не принимают постбеки. После удаления ссылка и постбеки по её токену недоступны. Изменение сроков хранения действует и на существующие записи."),
                    t.details({ className: "pl-panel" }, t.summary(null, "Дополнительно: собственные имена событий"),
                        t.p({ className: "txt-hint" }, "По умолчанию уже заданы стандартные события плагина. Меняйте имена только для совместимости с вашей аналитикой. В AppMetrica они учитываются как пользовательские события. Значения, присылаемые партнёром, настраиваются отдельно в соответствии статусов провайдера."),
                        t.div({ className: "pl-grid" }, Object.keys(state.config.eventNames).map(key => field(eventLabels[key] || key, state.config.eventNames, key, { required: true })))),
                    t.h2(null, "Провайдеры"), () => state.config.providers.map(providerEditor),
                    t.div({ className: "pl-actions" }, button("Добавить провайдера", newProvider), t.button({ type: "submit", className: "btn", disabled: () => state.saving || !!state.config.locks?.all }, () => state.saving ? "Сохранение…" : "Сохранить настройки"))) : null);
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
