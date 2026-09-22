// PocketBase owns the record form and schema editor; this field supplies its controls.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/dynamicLink/editor.css?v=1" }));
const defaults = () => ({ url: "", mode: "appView", saveCooke: true, showLoader: true, changeClient: false, openUrlsInBrowser: false, skipWarningDialog: false });
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
        function flag(label, key, hint) {
            const id = uid + "-" + key;
            return t.div({ className: "dl-option" }, t.div({ className: "field" },
                t.input({ id, type: "checkbox", className: "switch", checked: () => !!read()[key], onchange: e => change(value => value[key] = e.target.checked) }),
                t.label({ htmlFor: id }, label)), t.div({ className: "field-help" }, hint));
        }
        const root = t.div({ className: "record-field-input dl-input" },
            t.div({ className: "dl-label" }, t.i({ className: "ri-links-line", ariaHidden: true }), t.strong(null, name), props.field.required ? t.span({ className: "txt-danger" }, " *") : null),
            t.div({ className: "dl-controls" },
                text("URL", "url", false, "url"),
                t.div({ className: "field" }, t.label({ htmlFor: uid + "-mode" }, "Режим открытия"),
                    t.select({ id: uid + "-mode", value: () => read().mode, onchange: e => change(value => value.mode = e.target.value) },
                        t.option({ value: "appView" }, "WebView приложения (appView)"), t.option({ value: "view" }, "Встроенный браузер ОС (view)"), t.option({ value: "browser" }, "Внешний браузер (browser)"))),
                t.div({ className: "dl-grid" },
                    flag("Сохранять cookies", "saveCooke", "Сохраняет cookies между открытиями WebView."),
                    flag("Показывать загрузку", "showLoader", "Индикатор загрузки страницы в WebView."),
                    flag("Изменять клиент", "changeClient", "Использовать альтернативный User-Agent адаптера."),
                    flag("Внутренние ссылки во внешнем браузере", "openUrlsInBrowser", "Последующие ссылки со страницы открываются вне WebView."),
                    flag("Пропускать предупреждение", "skipWarningDialog", "Открывать ссылку без диалога подтверждения.")),
                t.details(null, t.summary(null, "Заголовок, аналитическое имя и предупреждение"),
                    t.div({ className: "dl-controls" }, text("Заголовок WebView", "title"), text("Имя для аналитики", "trackName"),
                        t.div({ className: "field" }, t.input({ id: uid + "-warning", type: "checkbox", className: "switch", checked: () => !!read().warningDialog, onchange: e => change(value => {
                            if (e.target.checked) value.warningDialog = { title: "", content: "" };
                            else delete value.warningDialog;
                        }) }), t.label({ htmlFor: uid + "-warning" }, "Свой текст предупреждения")),
                        t.div({ className: "dl-controls", hidden: () => !read().warningDialog }, text("Заголовок предупреждения", "title", true), text("Текст предупреждения", "content", true)))),
                !props.field.required ? t.button({ type: "button", className: "btn secondary", onclick: () => change(value => { Object.keys(value).forEach(key => delete value[key]); Object.assign(value, defaults()); }) }, "Очистить ссылку") : null),
            () => app.store.errors?.[name] ? t.div({ role: "alert", className: "field-error" }, app.store.errors[name].message || "Проверьте ссылку и параметры открытия") : null,
            () => props.field.help ? t.div({ className: "field-help" }, props.field.help) : null);
        return root;
    },
    view(props) {
        const value = props.record[props.field.name];
        if (!value?.url) return t.span({ className: "missing-value" });
        return t.div({ className: "record-field-view dl-preview", title: value.url }, t.span(null, value.url), t.small({ className: "txt-hint" }, value.mode || "appView"));
    },
};
