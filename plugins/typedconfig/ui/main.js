// Native PocketBase forms. Content authors never need to see or edit JSON.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/typedConfig/editor.css?v=4" }));
const previousAfterSend = app.pb.afterSend;
app.pb.afterSend = async (response, data) => {
    if (previousAfterSend) data = await previousAfterSend(response, data);
    if (/^\/api\/collections(?:\/[^/]+)?\/?$/.test(new URL(response.url).pathname)) {
        for (const collection of Array.isArray(data) ? data : data?.items || [data]) {
            if (!Array.isArray(collection?.fields)) continue;
            const managed = new Set(collection.fields.filter(f => f.type === "typedConfig").flatMap(f => Object.values(f.relations || {})));
            collection.fields = collection.fields.filter(f => !managed.has(f.name));
        }
    }
    return data;
};
const clone = value => JSON.parse(JSON.stringify(value));
const button = (label, action, disabled = false) => t.button({ type: "button", className: "btn sm secondary", onclick: action, disabled }, label);
function resolve(schema, node) { for (let i = 0; node?.ref && i < 12; i++) node = schema.definitions?.[node.ref]; return node || {}; }
function initial(schema, node) {
    node = resolve(schema, node);
    switch (node.kind) {
        case "boolean": return false;
        case "number": case "integer": return node.min ?? 0;
        case "enum": return node.options?.[0] || "";
        case "object": return objectDefaults(schema, node.fields);
        case "array": return [];
        case "reference": return { id: "" };
        default: return "";
    }
}
function objectDefaults(schema, fields) { return Object.fromEntries((fields || []).filter(f => f.required).map(f => [f.name, initial(schema, f.node)])); }
function summary(item, schema) {
    const block = schema.types?.find(b => b.key === item.type);
    const title = Object.values(item.data || {}).find(v => typeof v === "string" && v.trim());
    return title || block?.description || "Настройте содержимое блока";
}
function sourceView(node, value) {
    if (!value?.id) return t.span({ className: "txt-hint" }, "Запись не выбрана");
    const collection = app.store.collections.find(c => c.id === node.source.collection);
    const labelField = collection?.fields.find(f => f.id === node.source.labelField || f.name === node.source.labelField);
    if (!labelField) {
        const adapted = store({ source: value.id });
        return app.fieldTypes.relation.view({ record: adapted, field: { name: "source", type: "relation", collectionId: node.source.collection, maxSelect: 1 } });
    }
    const state = store({ title: "Загрузка записи…", failed: false });
    async function load() {
        state.failed = false;
        try {
            const record = await app.pb.collection(node.source.collection).getOne(value.id, { fields: `id,${labelField.name}`, requestKey: null });
            state.title = String(record[labelField.name] || "Без названия");
        } catch { state.title = "Не удалось загрузить запись"; state.failed = true; }
    }
    return t.div({ className: "tc-source-name", onmount: load },
        t.span(null, () => state.title),
        () => state.failed ? button("Повторить загрузку", load) : null);
}
app.fieldTypes.typedConfig = {
    icon: "ri-layout-grid-line", label: "Типизированные блоки", dummyData: () => [],
    settings(props) {
        return app.components.fieldSettings(props, { content: () => t.div(null,
            t.p({ className: "txt-hint" }, "Типы блоков и связи задаются схемой приложения. Содержимое редактируется карточками в форме записи."),
            t.div({ className: "tc-type-list" }, ...(props.field.schema?.types || []).map(b => t.span({ className: "label" }, b.label))),
            !props.field.schema?.types?.length ? t.p({ role: "alert" }, "Сначала добавьте схему типов через Go-миграцию.") : null),
        });
    },
    input(props) {
        const schema = props.field.schema || { types: [] };
        const name = props.field.name;
        const uid = "tc-" + app.utils.randomString();
        const local = store({ type: schema.types[0]?.key || "", query: "", dragging: "" });
        const expanded = new Set();
        const items = () => Array.isArray(props.record[name]) ? props.record[name] : [];
        const changed = () => root.dispatchEvent(new CustomEvent("change", { bubbles: true }));
        function replace(next) { props.record[name] = next; changed(); }
        function get(itemID, path) { let v = items().find(i => i.id === itemID)?.data; for (const key of path) v = v?.[key]; return v; }
        function set(itemID, path, value) {
            const item = items().find(i => i.id === itemID); if (!item) return;
            let owner = item.data;
            for (const key of path.slice(0, -1)) owner = owner[key];
            if (value === undefined) delete owner[path.at(-1)]; else owner[path.at(-1)] = value;
            changed();
        }
        function move(id, delta) {
            const next = [...items()], from = next.findIndex(i => i.id === id), to = from + delta;
            if (from < 0 || to < 0 || to >= next.length) return;
            next.splice(to, 0, next.splice(from, 1)[0]); replace(next);
        }
        function control(prop, itemID, path) {
            const node = resolve(schema, prop.node), id = uid + "-" + itemID + "-" + path.join("-");
            const read = () => get(itemID, path), write = v => set(itemID, path, v);
            const label = prop.label || "Элемент";
            function content() {
                switch (node.kind) {
                    case "object": return t.div({ className: "tc-nested" }, ...(node.fields || []).map(p => control(p, itemID, [...path, p.name])));
                    case "array": return t.div({ className: "tc-nested" },
                        () => (read() || []).map((_, index) => t.div({ className: "tc-array-item" },
                            control({ label: `Элемент ${index + 1}`, node: node.items, required: true }, itemID, [...path, index]),
                            button("Убрать элемент", () => write(read().filter((_, i) => i !== index))))),
                        button("Добавить элемент", () => write([...(read() || []), initial(schema, node.items)]), () => (read() || []).length >= 100));
                    case "reference": return t.div({ className: "tc-source" },
                        () => sourceView(node, read()),
                        button("Выбрать запись", () => app.modals.openRecordsPicker({
                            collection: node.source.collection, maxSelect: 1, selectedIds: read()?.id ? [read().id] : [],
                            onselect: records => write(records[0] ? { id: records[0].id } : null),
                        })),
                        t.p({ className: "field-help" }, "Данные обновляются из связанной записи. При её удалении этот блок будет убран."),
                        node.fields?.length ? t.div({ className: "tc-type-list" }, ...node.fields.map(p => t.span({ className: "label" }, p.label))) : null);
                    case "boolean": return t.div({ className: "field tc-toggle" },
                        t.input({ id, type: "checkbox", className: "switch", checked: () => !!read(), onchange: e => write(e.target.checked) }),
                        t.label({ htmlFor: id }, label, prop.required ? " *" : ""));
                    case "enum": return app.components.select({ id, required: prop.required, value: () => read() || "", options: () => node.options.map(value => ({ value, label: value })), onchange: opts => write(opts[0]?.value || "") });
                    default: return t.input({ id, type: ["number", "integer"].includes(node.kind) ? "number" : "text", required: prop.required, maxLength: node.maxLength || undefined,
                        min: node.min, max: node.max, step: node.kind === "integer" ? "1" : "any", value: () => read() ?? "",
                        oninput: e => write(["number", "integer"].includes(node.kind) ? (e.target.value === "" ? "" : e.target.valueAsNumber) : e.target.value) });
                }
            }
            const scalar = !["object", "array", "reference", "boolean"].includes(node.kind);
            const title = () => scalar
                ? t.label({ htmlFor: id }, label)
                : t.strong({ id: id + "-label", className: "tc-property-title" }, label, prop.required ? " *" : "");
            // Only atomic controls may use .field: PocketBase's checkbox styles
            // match all descendant inputs and labels, including nested groups.
            return t.div({ className: "tc-property", "html-data-property": path.join(".") },
                !prop.required || !scalar && node.kind !== "boolean" ? t.div({ className: "tc-property-header" }, title(),
                    !prop.required ? t.div({ className: "field tc-toggle tc-enable" },
                        t.input({ id: id + "-enabled", type: "checkbox", className: "switch", ariaLabel: `Использовать: ${label}`, checked: () => read() != null,
                            onchange: e => write(e.target.checked ? initial(schema, node) : undefined) }),
                        t.label({ htmlFor: id + "-enabled" }, () => read() != null ? "Включено" : "Не задано")) : null) : null,
                () => prop.required || read() != null ? t.div({ className: "tc-property-content", role: scalar || node.kind === "boolean" ? undefined : "group",
                    "html-aria-labelledby": scalar || node.kind === "boolean" ? undefined : id + "-label" },
                    scalar ? t.div({ className: "field tc-input" }, prop.required ? title() : null, content()) : content()) : null,
                prop.help ? t.div({ className: "field-help" }, prop.help) : null);

        }
        function card(item) {
            const block = schema.types.find(b => b.key === item.type);
            if (!block) return t.div({ role: "alert" }, "Неизвестный тип блока. Обновите страницу.");
            return t.details({ className: "tc-card", "html-data-item": item.id, open: expanded.has(item.id), ontoggle: e => e.target.open ? expanded.add(item.id) : expanded.delete(item.id) },
                t.summary({ draggable: true, ondragstart: () => local.dragging = item.id,
                    ondragover: e => e.preventDefault(), ondrop: e => { e.preventDefault(); const from = items().findIndex(i => i.id === local.dragging), to = items().findIndex(i => i.id === item.id); if (from >= 0) move(local.dragging, to - from); local.dragging = ""; } },
                    t.span({ className: "tc-number" }, () => String(items().findIndex(i => i.id === item.id) + 1).padStart(2, "0")),
                    t.div({ className: "tc-card-heading" }, t.strong(null, block.label), t.span({ className: "txt-hint" }, () => summary(items().find(i => i.id === item.id) || item, schema))),
                    t.i({ className: "ri-arrow-down-s-line", ariaHidden: true })),
                t.div({ className: "tc-card-body" }, block.description ? t.p({ className: "txt-hint" }, block.description) : null,
                    ...block.fields.map(p => control(p, item.id, [p.name])),
                    t.div({ className: "tc-actions" },
                        button("Выше", () => move(item.id, -1), () => items()[0]?.id === item.id),
                        button("Ниже", () => move(item.id, 1), () => items().at(-1)?.id === item.id),
                        button("Дублировать", () => { const copy = clone(items().find(i => i.id === item.id)); copy.id = "i" + app.utils.randomString(14); expanded.add(copy.id); replace([...items(), copy]); }, () => items().length >= 100),
                        button("Удалить блок", () => { if (window.confirm(`Удалить блок «${block.label}»? Связанные записи сохранятся.`)) replace(items().filter(i => i.id !== item.id)); }))));
        }
        const root = t.div({ className: "record-field-input tc-editor" },
            t.div({ className: "tc-heading" }, t.strong(null, props.field.help || "Содержимое экрана"), t.span({ className: "txt-hint" }, () => `${items().length} / 100 блоков`)),
            t.p({ className: "field-help" }, "Добавьте блоки и настройте их содержимое. Порядок карточек задаёт порядок на экране."),
            t.label({ htmlFor: uid + "-type" }, "Тип нового блока"),
            t.div({ className: "tc-toolbar" }, app.components.select({ id: uid + "-type", ariaLabel: "Тип нового блока", value: () => local.type, options: () => schema.types.map(b => ({ value: b.key, label: b.label })), onchange: opts => local.type = opts[0]?.value || "" }),
                button("Добавить блок", () => { const block = schema.types.find(b => b.key === local.type); if (!block) return; const id = "i" + app.utils.randomString(14); expanded.add(id); replace([...items(), { id, type: block.key, data: objectDefaults(schema, block.fields) }]); }, () => !local.type || items().length >= 100)),
            t.div({ className: "field tc-search", hidden: () => items().length < 5 }, t.input({ type: "search", ariaLabel: "Поиск блоков", placeholder: "Найти блок…", value: () => local.query, oninput: e => local.query = e.target.value })),
            t.div({ className: "tc-cards" }, () => (local.query ? items().filter(i => `${schema.types.find(b => b.key === i.type)?.label} ${summary(i, schema)}`.toLowerCase().includes(local.query.toLowerCase())) : items()).map(card)),
            t.p({ className: "tc-empty", hidden: () => items().length > 0 }, "Здесь пока нет блоков. Выберите тип и добавьте первый."),
            () => app.store.errors?.[name] ? t.p({ role: "alert", className: "field-error" }, app.store.errors[name].message || "Проверьте поля блоков и связанные записи.") : null);
        return root;
    },
    view(props) {
        const items = props.record[props.field.name] || [], schema = props.field.schema || { types: [] };
        if (!items.length) return t.span({ className: "txt-hint" }, "Нет блоков");
        const types = new Map();
        for (const item of items) types.set(item.type, (types.get(item.type) || 0) + 1);
        const description = [...types].map(([type, count]) => {
            const label = schema.types.find(b => b.key === type)?.label || "Блок";
            return count > 1 ? `${label} × ${count}` : label;
        }).join(" · ");
        const count = items.length;
        const word = count % 100 >= 11 && count % 100 <= 14 ? "блоков" :
            count % 10 === 1 ? "блок" : count % 10 >= 2 && count % 10 <= 4 ? "блока" : "блоков";
        return t.div({ className: "tc-preview record-field-view", title: `${count} ${word}: ${description}` },
            t.strong(null, `${count} ${word}`), t.span({ className: "txt-hint" }, description));
    },
};
