// PocketBase owns the record form and schema editor; this field supplies its controls.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/dynamicLink/editor.css?v=6" }));
const defaults = () => ({ url: "", mode: "browser", saveCooke: true, showLoader: true, changeClient: false, openUrlsInBrowser: false, skipWarningDialog: false });
const dlSelect = (id, value, choices, onchange, disabled = false) => app.components.select({
    id, value, disabled, required: true, options: choices.map(([value, label]) => ({ value, label })),
    placeholder: choices.find(([value]) => value === "")?.[1] || "Выберите значение",
    onchange: options => onchange(options[0]?.value || ""),
});
app.fieldTypes.dynamicLink = {
    icon: "ri-links-line", label: "Dynamic link",
    dummyData: () => ({ ...defaults(), url: "https://example.com" }),
    settings(props) {
        const id = "dl-settings-" + app.utils.randomString();
        return app.components.fieldSettings(props, {
            content: () => t.div({ className: "field" }, t.label({ htmlFor: id }, "Подсказка к полю"),
                t.input({ id, value: () => props.field.help || "", oninput: e => props.field.help = e.target.value })),
            footer: () => t.div({ className: "field" },
                t.input({ id: id + "-required", type: "checkbox", className: "sm", checked: () => !!props.field.required, onchange: e => props.field.required = e.target.checked }),
                t.label({ htmlFor: id + "-required" }, "Required")),
        });
    },
    input(props) {
        const name = props.field.name;
        const uid = "dl-" + app.utils.randomString();
        const draftKey = "@@dynamicLink:" + props.field.id;
        const read = () => props.record[name] || props.record[draftKey] || defaults();
        function change(edit) {
            const value = { ...defaults(), ...JSON.parse(JSON.stringify(read())) };
            edit(value);
            props.record[draftKey] = value;
            props.record[name] = value.url || props.field.required ? value : null;
            root.dispatchEvent(new CustomEvent("change", { bubbles: true }));
        }
        function text(label, key, warning = false, type = "text") {
            const id = uid + (warning ? "-warning-" : "-") + key;
            return t.div({ className: "field" }, t.label({ htmlFor: id }, label),
                t.input({ id, type, value: () => (warning ? read().warningDialog?.[key] : read()[key]) || "", oninput: e => change(value => {
                    if (warning) value.warningDialog[key] = e.target.value;
                    else value[key] = e.target.value;
                }) }));
        }
        const state = store({ policies: [], loading: true, error: "" });
        const categories = () => [...new Map(state.policies.flatMap(p => p.categories || []).map(c => [c.key, c])).values()];
        if (app.store.collections.some(c => c.name === "dynamic_link_settings")) {
            app.pb.collection("dynamic_link_settings").getFullList({ requestKey: null }).then(rows => state.policies = rows)
                .catch(() => state.error = "Не удалось загрузить категории. Откройте поле заново.").finally(() => state.loading = false);
        } else state.loading = false;
        const root = t.div({ className: "record-field-input dl-input" },
            t.div({ className: "dl-label" }, t.i({ className: "ri-links-line", ariaHidden: true }), t.strong(null, name), props.field.required ? t.span({ className: "txt-danger" }, " *") : null),
            t.div({ className: "dl-controls" },
                text("URL", "url", false, "url"),
                t.details({ className: "dl-link-options" }, t.summary(null, "Настроить Dynamic Link"),
                    t.div({ className: "dl-controls" },
                        t.p({ className: "txt-hint" }, "По умолчанию ссылка открывается во внешнем браузере. Поведение задаётся в общих настройках Dynamic Link."),
                        t.div({ className: "field" }, t.label({ htmlFor: uid + "-category" }, "Категория"),
                            () => dlSelect(uid + "-category", () => read().category || "",
                                [["", "Общие настройки"], ...categories().map(c => [c.key, c.label]),
                                    ...(read().category && !categories().some(c => c.key === read().category) ? [[read().category, read().category + " (категория удалена)"]] : [])],
                                category => change(value => value.category = category), () => state.loading || !!state.error)),
                        text("Заголовок WebView", "title"),
                        t.p({ className: "field-help" }, "Используется при открытии ссылки во встроенном WebView."),
                        () => state.error ? t.p({ role: "alert" }, state.error) : null)),
                !props.field.required ? t.button({ type: "button", className: "btn secondary", onclick: () => change(value => { Object.keys(value).forEach(key => delete value[key]); Object.assign(value, defaults()); }) }, "Очистить ссылку") : null),
            () => app.store.errors?.[name] ? t.div({ role: "alert", className: "field-error" }, app.store.errors[name].message || "Проверьте ссылку и параметры открытия") : null,
            () => props.field.help ? t.div({ className: "field-help" }, props.field.help) : null);
        return root;
    },
    view(props) {
        const value = props.record[props.field.name];
        if (!value?.url) return t.span({ className: "missing-value" });
        return t.div({ className: "record-field-view dl-preview", title: value.url }, t.span(null, value.url), t.small({ className: "txt-hint" }, value.category ? "Категория: " + value.category : "Общие настройки"));
    },
};

watch(() => app.store.activeCollection?.name === "dynamic_link_settings", active => document.body.classList.toggle("dl-settings-active", !!active));

// Reuse the native Singleton form and Variants toolbar, accessible from the app bar.
const dlServiceCollection = collection => ["dynamic_link_settings", "partner_links"].includes(collection?.name);
app.store.headerLinks = [...app.store.headerLinks.map(link => link.href === "#/collections" ? {
    ...link,
    isActive: el => link.isActive?.(el) || app.utils.isActivePath("#/collections"),
    get href() {
        // Native Collections restores pbLastActiveCollection, which older URLs
        // may have set to a service collection. Give the header a safe target
        // even when the user is already on that legacy Collections page.
        const collections = app.store.collections;
        const active = app.store.activeCollection;
        const saved = localStorage.getItem("pbLastActiveCollection");
        const target = active && !dlServiceCollection(active) ? active :
            collections.find(c => !dlServiceCollection(c) && [c.id, c.name].includes(saved)) ||
            collections.find(c => !c.system && !dlServiceCollection(c));
        return target ? "#/collections?collection=" + encodeURIComponent(target.name) : "#/collections";
    },
} : link), { label: "Dynamic Link", href: "#/dynamic-links", icon: "ri-external-link-line" }];
app.routes.superuserOnly("#/dynamic-links", () => {
    app.store.title = "Dynamic Link";
    const state = store({ filter: app.utils.getHashQueryParams().filter || "" });
    // Collection loading starts asynchronously during authentication. Keep the
    // page reactive so a direct URL/reload can render once the schema arrives.
    return t.div({ className: "page", onmount: () => {
        // Wait until the outgoing Collections page has disposed its watchers;
        // otherwise it stores this service collection as the last content page.
        app.store.activeCollection = "dynamic_link_settings";
    } }, () => {
        const collection = app.store.collections.find(c => c.name === "dynamic_link_settings");
        if (!collection) {
            return t.div({ className: "page-content wrapper" }, t.h2(null, "Dynamic Link"),
                app.store.isLoadingCollections ? t.p({ role: "status" }, "Загрузка настроек…") :
                app.store.collections.length ? t.p(null, "Общие настройки ещё не подключены. Вызовите dynamiclink.Configure в миграции приложения.") :
                t.div({ role: "alert" }, "Не удалось загрузить коллекции. ",
                    t.button({ type: "button", className: "btn secondary", onclick: () => app.store.loadCollections("dynamic_link_settings") }, "Повторить")));
        }
        return t.div({ className: "page-content dl-settings-page" },
            t.header({ className: "page-header" },
                t.nav({ className: "breadcrumbs" }, t.div({ className: "breadcrumb-item" }, "Dynamic Link")),
                t.div({ pbEvent: "pageHeaderSecondaryBtns", className: "page-header-secondary-btns" },
                    t.button({ type: "button", className: "btn secondary btn-collection-settings", ariaLabel: "Collection settings", onclick: () => app.modals.openCollectionUpsert(collection) }, "Collection settings"))),
            t.p({ className: "txt-hint" }, "Общие настройки открытия и категории ссылок. По умолчанию используется внешний браузер."),
            app.components.recordsSearchbar({ collection, value: () => state.filter, onsubmit: value => state.filter = value }),
            app.components.recordsList({ collection, filter: () => state.filter, onchange: value => state.filter = value }));
    });
});

const dlJSONInput = app.fieldTypes.json.input;
app.fieldTypes.json.input = function(props) {
    if (props.collection?.name !== "dynamic_link_settings" || !["openingOptions", "categories"].includes(props.field.name)) return dlJSONInput(props);
    const key = props.field.name;
    const uid = "dl-policy-" + app.utils.randomString();
    const categories = key === "categories";
    const read = () => props.record[key] || (categories ? [] : {});
    const state = store({ selected: 0 });
    const selected = () => Math.min(state.selected, Math.max(0, read().length - 1));
    function change(edit) {
        if (!props.record[key]) props.record[key] = categories ? [] : {};
        edit(props.record[key]);
        root.dispatchEvent(new CustomEvent("change", { bubbles: true }));
    }
    let sequence = 0;
    function control(label, readValue, update, choices) {
        const id = uid + "-" + (++sequence);
        return t.div({ className: "field" }, t.label({ htmlFor: id }, label), choices
            ? dlSelect(id, readValue, choices, update)
            : t.input({ id, value: readValue, oninput: e => update(e.target.value) }));
    }
    function options(get, edit, inherited) {
        const field = (label, name, choices) => control(label, () => get()[name] ?? "", value => edit(o => { if (value === "") delete o[name]; else o[name] = value; }), choices);
        const flags = [["saveCooke", "Сохранять cookies", true], ["showLoader", "Показывать загрузку", true], ["changeClient", "Альтернативный User-Agent", false], ["openUrlsInBrowser", "Внутренние ссылки во внешнем браузере", false]];
        return t.div({ className: "dl-controls" },
            inherited ? field("Режим открытия категории", "mode", [["", "Общий режим"], ["browser", "Внешний браузер"], ["view", "Встроенный браузер ОС"], ["appView", "WebView приложения"]]) : null,
            t.div({ className: "dl-grid" }, flags.map(([name, label, fallback]) => {
                const id = uid + "-" + (++sequence);
                const shared = () => props.record.openingOptions?.[name] ?? fallback;
                return t.div({ className: "dl-option" },
                    t.div({ className: "field dl-switch" },
                        t.input({ id, type: "checkbox", className: "switch", checked: () => get()[name] ?? (inherited ? shared() : fallback),
                            onchange: e => edit(o => o[name] = e.target.checked) }),
                        t.label({ htmlFor: id }, label)),
                    inherited ? t.div({ className: "field-help" },
                        () => get()[name] === undefined ? "Используется общая настройка" : "Переопределено для категории",
                        t.button({ type: "button", className: "btn sm secondary", hidden: () => get()[name] === undefined,
                            ariaLabel: "Вернуть общую настройку: " + label, onclick: () => edit(o => delete o[name]) }, "Сбросить")) : null);
            })),
            inherited ? field("Предупреждение категории", "warningPolicy", [["", "Общее предупреждение"], ["disabled", "Не показывать"], ["replace", "Свой текст"]]) : null,
            inherited ? t.div({ className: "dl-controls", hidden: () => get().warningPolicy !== "replace" }, field("Заголовок предупреждения", "warningTitle"), field("Текст предупреждения", "warningContent")) : null);
    }
    function categoryEditor(index) {
        const get = () => read()[index];
        const edit = fn => change(rows => fn(rows[index]));
        return t.div({ className: "dl-category", role: "tabpanel", id: uid + "-panel-" + index, ariaLabelledby: uid + "-tab-" + index },
            t.div({ className: "dl-controls" },
                control("Название категории", () => get()?.label || "", value => edit(c => c.label = value)),
                control("Код категории", () => get()?.key || "", value => edit(c => c.key = value)),
                t.p({ className: "txt-hint" }, "Латинские буквы, цифры, _ и -. Код связывает ссылки с категорией; при его смене выберите категорию в ссылках заново."),
                options(() => get()?.options || {}, fn => edit(c => { c.options ||= {}; fn(c.options); }), true),
                t.button({ type: "button", className: "btn secondary", onclick: () => { if (confirm("Удалить категорию? Её ссылки будут использовать общие настройки.")) change(rows => rows.splice(index, 1)); } }, "Удалить категорию")));
    }
    function tabKeydown(event, index) {
        const count = read().length;
        const next = { ArrowRight: (index + 1) % count, ArrowLeft: (index + count - 1) % count, Home: 0, End: count - 1 }[event.key];
        if (next === undefined) return;
        event.preventDefault();
        state.selected = next;
        root.querySelectorAll('[role="tab"]')[next]?.focus();
    }
    const root = t.div({ className: "record-field-input dl-policy-input dl-controls" },
        t.strong(null, categories ? "Категории ссылок" : "Параметры открытия"),
        categories ? t.p({ className: "txt-hint" }, "Категория переопределяет общие настройки для выбранного набора пользователей. Без категории действуют общие настройки.") : null,
        categories ? t.div({ className: "dl-category-navigation" },
            t.div({ className: "dl-category-tabs", role: "tablist", ariaLabel: "Категории ссылок", hidden: () => !read().length },
                () => read().map((category, index) => t.button({ type: "button", role: "tab", className: "dl-category-tab",
                    id: uid + "-tab-" + index, ariaControls: uid + "-panel-" + index,
                    ariaSelected: () => selected() === index, tabIndex: () => selected() === index ? 0 : -1,
                    title: () => read()[index]?.label || "Новая категория",
                    onclick: () => state.selected = index, onkeydown: e => tabKeydown(e, index) },
                    () => read()[index]?.label || "Новая категория"))),
            t.button({ type: "button", className: "btn secondary", onclick: () => {
                change(rows => {
                    rows.push({ key: "category_" + app.utils.randomString(6), label: "", options: {} });
                    state.selected = rows.length - 1;
                });
                requestAnimationFrame(() => root.querySelector('[role="tab"][aria-selected="true"]')?.scrollIntoView({ block: "nearest", inline: "nearest" }));
            } }, "Добавить категорию")) : null,
        categories ? t.div({ className: "dl-controls" }, () => read().length ? categoryEditor(selected())
            : t.p({ className: "txt-hint" }, "Категорий пока нет. Добавьте категорию, чтобы задать отдельные параметры открытия.")) : options(read, change, false));
    return root;
};

// Keep the native record lifecycle, with human-readable global policy controls.
for (const type of ["select", "text"]) {
    const nativeInput = app.fieldTypes[type].input;
    app.fieldTypes[type].input = function(props) {
        const name = props.field.name;
        const labels = { mode: "Режим открытия", warningPolicy: "Предупреждение", warningTitle: "Заголовок предупреждения", warningContent: "Текст предупреждения" };
        if (props.collection?.name !== "dynamic_link_settings" || !labels[name]) return nativeInput(props);
        const id = "dl-global-" + app.utils.randomString();
        const isWarningText = ["warningTitle", "warningContent"].includes(name);
        const choices = name === "mode" ? [["browser", "Внешний браузер"], ["view", "Встроенный браузер ОС"], ["appView", "WebView приложения"]]
            : [["disabled", "Не показывать"], ["replace", "Свой текст"]];
        const update = value => { props.record[name] = value; };
        return t.div({ className: "record-field-input dl-controls", hidden: () => isWarningText && props.record.warningPolicy !== "replace" },
            t.div({ className: "field" }, t.label({ htmlFor: id }, labels[name]),
                type === "select" ? dlSelect(id, () => name === "mode" ? props.record[name] || "browser" : props.record[name] === "replace" ? "replace" : "disabled", choices, update)
                    : t.input({ id, value: () => props.record[name] || "", oninput: e => update(e.target.value) })));
    };
}
