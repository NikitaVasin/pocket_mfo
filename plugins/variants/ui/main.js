// Native PocketBase components with scoped layout styles for the variants editor.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/variants/editor.css?v=2" }));
const pvApi = "/api/variants/admin/collections/";
const pvService = name => ["pv_configs", "pv_sets", "pv_states", "pv_history"].includes(name) || name?.startsWith("pv_choice_");
const pvKey = prefix => prefix + app.utils.randomString(8).toLowerCase();
const pvField = () => ({ kind: "field", field: "", op: "eq", value: "" });
const pvCollections = () => app.store.collections.filter(c => !c.system && !pvService(c.name) && c.type !== "view" && !c.fields?.some(f => f.name === "content_set"));
const pvManagedCollection = collection => collection?.type === "base" && collection.fields?.some(f => f.name === "content_set" && app.store.collections.find(c => c.id === f.collectionId)?.name === "pv_sets");

// Keep the preset in the native page filter so counts, pagination, sorting and
// shared URLs all use the same query. Only unwrap a complete parenthesized tail.
function pvPresetFilter(filter) {
    const original = filter || "";
    const match = original.trim().match(/^content_set\s*=\s*(['"])([a-z0-9]{15})\1\s*([\s\S]*)$/);
    if (!match) return { set: "", search: original };
    if (!match[3]) return { set: match[2], search: "" };
    const tail = match[3].match(/^&&\s*(\([\s\S]*\))$/)?.[1];
    if (tail) {
        let depth = 0, quote = "", escaped = false;
        for (let i = 0; i < tail.length; i++) {
            const char = tail[i];
            if (quote) {
                if (escaped) escaped = false;
                else if (char === "\\") escaped = true;
                else if (char === quote) quote = "";
            } else if (char === "'" || char === '"') quote = char;
            else if (char === "(") depth++;
            else if (char === ")" && (--depth === 0 && i !== tail.length - 1 || depth < 0)) return { set: "", search: original };
        }
        if (depth === 0 && !quote) return { set: match[2], search: tail.slice(1, -1) };
    }
    return { set: "", search: original };
}

const pvRecordsSearchbar = app.components.recordsSearchbar;
function pvRecordsControls(propsArg = {}) {
    const props = store({ collection: null, value: "", hidden: false, presetsOnly: false, mainPage: false, onsubmit: () => {}, beforechange: () => true });
    const watchers = app.utils.extendStore(props, propsArg);
    const state = store({ mounted: false, managed: false, loading: false, error: "", sets: [], config: null, encoded: null, search: "" });
    let generation = 0;
    const selection = () => state.managed ? pvPresetFilter(props.value) : { set: "", search: props.value };
    const search = () => state.managed && state.encoded === props.value ? state.search : selection().search;
    function submit(set, value) {
        if (!props.beforechange()) return;
        let filter = value;
        if (set) {
            const normalized = app.utils.normalizeSearchFilter(value, props.collection.fields.filter(f => !f.hidden).map(f => f.name));
            filter = `content_set = '${set}'` + (normalized ? ` && (${normalized})` : "");
        }
        state.search = value;
        state.encoded = filter;
        props.onsubmit(filter);
    }
    async function load() {
        const current = ++generation;
        const collection = props.collection;
        state.sets = []; state.config = null; state.error = ""; state.encoded = null;
        state.managed = !!(state.mounted && pvManagedCollection(collection));
        state.loading = state.managed;
        if (!state.managed) return;
        try {
            const data = await app.pb.send(pvApi + collection.id, { requestKey: null });
            if (current !== generation) return;
            state.config = data.config;
            // Published order first, preserved archive sets last.
            const order = [];
            for (const variant of [data.config.default, ...(data.config.variants || [])]) {
                order.push([variant.key, "", ""]);
                for (const experiment of variant.experiments || []) for (const group of experiment.groups || []) order.push([variant.key, experiment.key, group.key]);
            }
            const rank = s => {
                const index = order.findIndex(([v, e, g]) => s.variant === v && s.experiment === e && s.group === g);
                return index < 0 ? order.length : index;
            };
            state.sets = data.sets.sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
        } catch (err) {
            if (current === generation) state.error = err.response?.message || err.message;
        } finally {
            if (current === generation) state.loading = false;
        }
    }
    watchers.push(watch(() => [state.mounted, props.collection?.id, props.collection?.updated], load));
    watchers.push(watch(() => [state.managed, state.sets, props.value], () => {
        if (!state.managed || !state.sets.length || selection().set) return;
        const baseline = state.sets.find(s => s.variant === "default" && !s.experiment);
        if (baseline) submit(baseline.id, props.value);
    }));
    const id = "pv_preset_" + app.utils.randomString();
    const selectedSet = () => state.sets.find(s => s.id === selection().set);
    const variants = () => {
        const configured = [state.config?.default, ...(state.config?.variants || [])].filter(Boolean);
        return [...new Set(state.sets.map(s => s.variant))].map(key => {
            const variant = configured.find(v => v.key === key);
            return { value: key, label: key === "default" ? "default" : variant?.name || key, archived: !variant, experiments: state.sets.some(s => s.variant === key && s.experiment) };
        });
    };
    const onlyDefault = () => variants().length === 1 && variants()[0].value === "default";
    const selectedVariant = () => selectedSet()?.variant || "default";
    function groupLabel(set, variantSets) {
        if (!set.experiment) return "default";
        const variant = [state.config?.default, ...(state.config?.variants || [])].find(v => v?.key === set.variant);
        const experiment = variant?.experiments?.find(e => e.key === set.experiment);
        const group = experiment?.groups?.find(g => g.key === set.group);
        const multiple = new Set(variantSets.filter(s => s.experiment).map(s => s.experiment)).size > 1;
        const label = group?.name || set.group;
        return (multiple ? `${experiment?.name || set.experiment} / ${label}` : label) + (set.active ? "" : " (архив)");
    }
    function selector(label, suffix, options, value, change) {
        return t.div({ className: "field m-0", style: "flex:1 1 260px;min-width:0;max-width:560px" },
            t.label({ htmlFor: id + suffix }, label),
            app.components.select({ id: id + suffix, required: true, options, value, onchange: opts => change(opts[0]?.value || "default") }));
    }
    return t.div({
        className: "full-width pv-records-presets",
        hidden: () => props.hidden,
        // Pickers also use recordsSearchbar; presets belong only to the main page.
        onmount: el => state.mounted = props.presetsOnly || props.mainPage || el.parentElement?.classList.contains("page-content") || false,
        onunmount: () => { generation++; watchers.forEach(w => w?.unwatch()); },
    },
    () => {
        if (!state.managed) return null;
        if (state.loading) return t.p({ className: "txt-hint m-b-15", role: "status" }, "Загрузка вариантов…");
        if (state.error) return t.div({ role: "alert", className: "txt-danger m-b-15" }, state.error, pvButton("Повторить", load));
        const variantOptions = variants();
        const variant = selectedVariant();
        const variantSets = state.sets.filter(s => s.variant === variant);
        const hasExperiment = variantSets.some(s => s.experiment);
        if (onlyDefault() && !hasExperiment) return null;
        return t.div({ className: "flex flex-wrap gap-15 m-b-15" },
            onlyDefault() ? null : selector("Вариант", "_variant",
                variantOptions.map(v => ({ value: v.value, label: () => pvVariantLabel(v.label + (v.archived ? " (архив)" : ""), v.experiments) })),
                () => selectedVariant(), value => {
                    const base = state.sets.find(s => s.variant === value && !s.experiment);
                    if (base) submit(base.id, search());
                }),
            hasExperiment ? selector("Вариант эксперимента", "_group",
                variantSets.map(s => ({ value: s.id, label: groupLabel(s, variantSets) })),
                () => selection().set, value => submit(value, search())) : null,
        );
    },
    props.presetsOnly ? null : pvRecordsSearchbar({ ...propsArg, value: search, onsubmit: value => state.managed ? submit(selection().set, value) : props.onsubmit(value) }));
};
app.components.recordsSearchbar = pvRecordsControls;
// Shared by the singleton page; normal collections retain their native search.
app.components.variantPresets = props => pvRecordsControls({ ...props, presetsOnly: true });

// Omit only the table's field definition; record editors and stored schemas keep
// content_set intact. Hold the list empty until its default preset is ready.
const pvRecordsList = app.components.recordsList;
app.components.recordsList = function(props = {}) {
    const read = value => typeof value === "function" ? value() : value;
    return pvRecordsList({ ...props,
        collection: () => {
            const collection = read(props.collection);
            return pvManagedCollection(collection) ? { ...collection, fields: collection.fields.filter(f => f.name !== "content_set") } : collection;
        },
        filter: () => {
            const filter = read(props.filter) || "";
            return pvManagedCollection(read(props.collection)) && !pvPresetFilter(filter).set ? "id = ''" : filter;
        },
    });
};

function pvEditorState(upsert) {
    return upsert.pvEditor || (upsert.pvEditor = store({ initialized: false, loading: true, busy: false, error: "", config: null, sets: [], preview: null, history: null, user: "", published: false, expanded: {} }));
}
function pvLoadEditor(upsert, state) {
    const cid = upsert.collection.id;
    if (state.initialized) return;
    state.initialized = true;
    (async () => {
        try {
            if (!cid || upsert.isNew) return;
            const data = await app.pb.send(pvApi + cid, { requestKey: null });
            state.sets = data.sets; state.config = data.config || { collection: cid, authCollection: app.store.collections.find(c => c.type === "auth" && !c.system)?.id || "", variables: false, experiments: false, version: 0, default: { key: "default", name: "Default", experiments: [] }, variants: [], listRule: upsert.collection.listRule, viewRule: upsert.collection.viewRule };
            state.config.variants ||= []; state.config.default.experiments ||= [];
        } catch (err) { state.error = err.message; } finally { state.loading = false; }
    })();
}
async function pvPublishEditor(upsert, state) {
    if (state.busy || upsert.isSaving) return;
    if (upsert.hasChanges) {
        state.error = "Сначала сохраните изменения схемы коллекции через Save and continue (Ctrl+S), затем опубликуйте Variants.";
        return;
    }
    state.busy = true; state.error = ""; state.published = false;
    // Use the native modal lock so in-flight publication cannot discard edits.
    upsert.isSaving = true;
    try {
        const cid = upsert.collection.id;
        state.config = await app.pb.send(pvApi + cid, { requestKey: null, method: "PUT", body: JSON.parse(JSON.stringify(state.config)) });
        const schema = await app.pb.collections.getOne(cid);
        upsert.collection = JSON.parse(JSON.stringify(schema)); upsert.originalCollection = JSON.parse(JSON.stringify(schema));
        await app.store.loadCollections();
        const data = await app.pb.send(pvApi + cid, { requestKey: null });
        state.sets = data.sets; state.published = true;
    } catch (err) { state.error = err.response?.message || err.message; }
    finally { state.busy = false; upsert.isSaving = false; }
}
function pvAccessRules(state, collection) {
    const autocomplete = word => app.utils.collectionAutocompleteKeys(collection, word);
    return t.div(null,
        t.div({ className: "m-b-15" }, app.components.ruleField({ label: "List/Search rule", autocomplete, disabled: () => !state.config.version || state.busy, value: () => state.config.listRule, oninput: v => { state.config.listRule = v; state.published = false; } })),
        t.div({ className: "m-b-15" }, app.components.ruleField({ label: "View rule", autocomplete, disabled: () => !state.config.version || state.busy, value: () => state.config.viewRule, oninput: v => { state.config.viewRule = v; state.published = false; } })));
}

const pvCollectionRulesTab = app.collectionTypes.base.tabs["API rules"];
app.collectionTypes.base.tabs["API rules"] = function(upsert) {
    if (!pvManagedCollection(upsert.collection)) return pvCollectionRulesTab(upsert);
    const state = pvEditorState(upsert);
    pvLoadEditor(upsert, state);
    return t.div({ className: "collection-tab-content" },
        t.p({ className: "txt-hint" }, "Ваши правила дополнительно ограничивают доступ к выбранному набору Variants. Должны выполняться оба условия; служебные правила добавляются автоматически."),
        t.div({ role: "alert", className: "alert danger", hidden: () => !state.error }, () => state.error),
        () => state.loading ? t.p(null, "Загрузка…") : state.config ? t.div(null,
            pvAccessRules(state, upsert.collection),
            t.p({ className: "txt-hint" }, "Создавать, изменять и удалять записи этой коллекции могут только суперпользователи."),
            pvButton("Опубликовать правила", () => pvPublishEditor(upsert, state), () => state.busy),
            t.p({ role: "status" }, () => state.busy ? "Публикация…" : state.published ? `Опубликовано, версия ${state.config.version}` : ""),
        ) : null);
};

function pvButton(label, onclick, disabled = false) {
    return t.button({ type: "button", className: "btn sm secondary m-r-5 m-b-5", disabled, onclick }, label);
}
function pvInput(label, value, change, type = "text", extra = {}) {
    const id = "pv_" + app.utils.randomString();
    return t.div({ className: "field m-b-15" }, t.label({ htmlFor: id }, label), t.input({ id, type, value, oninput: e => change(type === "number" ? Number(e.target.value) : e.target.value), ...extra }));
}
function pvSelect(label, options, value, change, disabled = false) {
    const id = "pv_" + app.utils.randomString();
    // The native select synchronizes labels when value changes, not options.
    // Recreate it on option changes, keeping ordinary value edits reactive.
    return t.div({ className: "field m-b-15" }, t.label({ htmlFor: id }, label),
        () => app.components.select({ id, options: typeof options === "function" ? options() : options, value, required: true, disabled, onchange: opts => change(opts[0]?.value || "") }));
}
function pvCheck(label, value, change, disabled = false) {
    const id = "pv_" + app.utils.randomString();
    return t.div({ className: "field m-b-15" }, t.input({ id, type: "checkbox", checked: value, disabled, onchange: e => change(e.target.checked) }), t.label({ htmlFor: id }, label));
}
function pvRelations(collection) {
    if (!collection) return [];
    const allowed = pvCollections();
    const result = (collection.fields || []).filter(f => f.type === "relation" && allowed.some(c => c.id === f.collectionId))
        .map(f => ({ value: f.name, label: f.name, target: allowed.find(c => c.id === f.collectionId) }));
    for (const c of allowed) for (const f of c.fields || []) if (f.type === "relation" && f.collectionId === collection.id) result.push({ value: `${c.name}_via_${f.name}`, label: `${c.name} via ${f.name}`, target: c });
    return result;
}
const pvOperators = { eq: "Равно", ne: "Не равно", gt: "Больше", gte: "Больше или равно", lt: "Меньше", lte: "Меньше или равно", contains: "Содержит", notContains: "Не содержит", empty: "Пусто", notEmpty: "Не пусто" };
function pvConditionSummary(node) {
    if (!node) return "Без условий";
    if (node.kind === "all" || node.kind === "any") return (node.children || []).map(pvConditionSummary).join(node.kind === "all" ? " И " : " ИЛИ ");
    if (node.kind === "exists" || node.kind === "notExists") return `${node.kind === "exists" ? "Есть" : "Нет"} ${node.relation || "связанной записи"}: ${(node.children || []).map(pvConditionSummary).join("")}`;
    const value = typeof node.value === "boolean" ? (node.value ? "Да" : "Нет") : String(node.value ?? "");
    return node.field ? `${node.field} ${(pvOperators[node.op] || "").toLowerCase()} ${["empty", "notEmpty"].includes(node.op) ? "" : value}`.trim() : "Условие не задано";
}
function pvVariantLabel(label, experiments) {
    return t.span({ className: "flex gap-5", style: "min-width:0" }, t.span({ className: "txt" }, label),
        experiments ? t.i({ className: "ri-flask-line txt-hint pv-experiment-mark", title: "Есть эксперименты", ariaLabel: "Есть эксперименты" }) : null);
}
function pvDisclosure(state, key, title, hint, content, className = "", initiallyOpen = false) {
    return t.details({
        className: "accordion noanimation pv-disclosure " + className,
        "html-data-pv-section": key,
        open: () => state.expanded[key] ?? initiallyOpen,
        ontoggle: e => { if (e.target.isConnected) state.expanded[key] = e.target.open; },
    },
    t.summary(null, t.div({ className: "pv-summary" },
        t.strong(null, title),
        hint ? t.div({ className: "txt-hint txt-sm pv-summary-hint", title: hint }, hint) : null)),
    t.div({ className: "pv-section-body" }, content));
}
function pvCondition(node, collection, state, path, depth = 0, remove = null) {
    const fields = () => (collection?.fields || []).filter(f => !f.hidden && ["text", "email", "url", "number", "bool", "date", "autodate", "select"].includes(f.type) && !(f.type === "select" && f.maxSelect > 1));
    function add(kind) {
        const child = kind === "field" ? pvField() : kind === "all" ? { kind: "all", children: [pvField()] } : { kind: "exists", relation: "", children: [{ kind: "all", children: [pvField()] }] };
        if (node.kind === "all" || node.kind === "any") node.children.push(child);
        else {
            const previous = JSON.parse(JSON.stringify(node));
            Object.keys(node).forEach(key => delete node[key]);
            Object.assign(node, { kind: "all", children: previous.field ? [previous, child] : [child] });
        }
        state.expanded[path] = true;
        const childPath = `${path}/${node.children.length - 1}`;
        state.expanded[childPath] = true;
        state.expanded[childPath + "/related"] = true;
    }
    const actions = () => t.div({ className: "flex flex-wrap gap-5 m-t-10" },
        pvButton("Добавить условие", () => add("field"), depth >= 5),
        pvButton("Добавить группу", () => add("all"), depth >= 4),
        pvButton("Добавить связь", () => add("exists"), depth >= 3),
        remove ? pvButton("Удалить условие", remove) : null);
    return t.div({ className: "pv-condition" }, () => {
        if (node.kind === "all" || node.kind === "any") return pvDisclosure(state, path,
            () => node.kind === "all" ? "Все условия (И)" : "Любое условие (ИЛИ)",
            () => pvConditionSummary(node),
            t.div(null,
                pvSelect("Совпадение условий", () => [{ value: "all", label: "Все условия (И)" }, { value: "any", label: "Любое условие (ИЛИ)" }], () => node.kind, value => node.kind = value),
                () => node.children.map((child, i) => pvCondition(child, collection, state, `${path}/${i}`, depth + 1, node.children.length > 1 ? () => node.children.splice(i, 1) : null)),
                actions()), "pv-condition-group");
        if (node.kind === "exists" || node.kind === "notExists") return pvDisclosure(state, path,
            () => node.kind === "exists" ? "Есть связанная запись" : "Нет связанной записи",
            () => pvConditionSummary(node),
            t.div(null,
                t.div({ className: "pv-condition-relation" },
                    pvSelect("Условие связи", () => [{ value: "exists", label: "Есть запись" }, { value: "notExists", label: "Нет записи" }], () => node.kind, value => node.kind = value),
                    pvSelect("Связь", () => pvRelations(collection), () => node.relation, value => { node.relation = value; node.children = [{ kind: "all", children: [pvField()] }]; state.expanded[path + "/related"] = true; })),
                t.p({ className: "txt-hint txt-sm m-t-0" }, "Условия ниже проверяются для одной и той же связанной записи."),
                () => pvCondition(node.children[0], pvRelations(collection).find(r => r.value === node.relation)?.target, state, path + "/related", depth + 1),
                remove ? pvButton("Удалить условие", remove) : null), "pv-condition-group");
        return t.div({ className: "pv-condition-leaf" },
            t.div({ className: "pv-condition-row" },
                pvSelect("Поле", () => fields().map(f => ({ value: f.name, label: f.name })), () => node.field, value => { node.field = value; const f = fields().find(f => f.name === value); node.value = f?.type === "bool" ? true : f?.type === "number" ? 0 : ""; }),
                pvSelect("Оператор", () => Object.entries(pvOperators).map(([value, label]) => ({ value, label })), () => node.op, value => node.op = value),
                () => {
                    if (["empty", "notEmpty"].includes(node.op)) return null;
                    const f = fields().find(f => f.name === node.field);
                    if (f?.type === "bool") return pvSelect("Значение", () => [{ value: "true", label: "Да" }, { value: "false", label: "Нет" }], () => String(node.value), value => node.value = value === "true");
                    if (f?.type === "select") return pvSelect("Значение", () => f.values.map(value => ({ value, label: value })), () => node.value, value => node.value = value);
                    return pvInput("Значение", () => node.value, value => node.value = value, f?.type === "number" ? "number" : "text");
                },
                remove ? t.button({ type: "button", className: "btn sm circle transparent danger pv-condition-delete", ariaLabel: "Удалить условие", title: "Удалить условие", onclick: remove }, t.i({ className: "ri-close-line", ariaHidden: true })) : null),
            depth === 0 ? actions() : null);
    });
}
function pvExperiments(variant, enabled, state) {
    return t.div(null,
        () => (variant.experiments || []).map((ex, i) => pvDisclosure(state, `${variant.key}/experiment/${ex.key}`,
            () => ex.name || "Эксперимент",
            () => `${ex.active && enabled() ? "Активен" : "Неактивен"} · ${ex.groups.length} группы`,
            t.div(null,
            pvInput("Название эксперимента", () => ex.name, v => ex.name = v),
            pvCheck("Активный эксперимент", () => ex.active, v => { if (v) variant.experiments.forEach(e => e.active = false); ex.active = v; }, () => !enabled()),
            () => ex.groups.map((g, gi) => t.div({ className: "grid" },
                t.div({ className: "col-sm-4" }, pvInput("Название группы", () => g.name, v => g.name = v)),
                t.div({ className: "col-sm-3" }, pvInput("Бакет от", () => g.from, v => g.from = v, "number", { min: 1, max: 10000, step: 1 })),
                t.div({ className: "col-sm-3" }, pvInput("Бакет до", () => g.to, v => g.to = v, "number", { min: 1, max: 10000, step: 1 })),
                t.div({ className: "col-sm-2" }, t.p(null, () => `${Math.max(0, (g.to - g.from + 1) / 100).toFixed(2)}%`), pvButton("Убрать", () => ex.groups.splice(gi, 1), () => ex.groups.length <= 2)))),
            pvButton("Добавить группу", () => ex.groups.push({ key: pvKey("g"), name: "Группа", from: 1, to: 100 })),
            pvButton("Распределить поровну", () => ex.groups.forEach((g, gi) => { g.from = Math.floor(gi * 10000 / ex.groups.length) + 1; g.to = Math.floor((gi + 1) * 10000 / ex.groups.length); })),
            pvInput("Доля первой группы (%)", () => (ex.groups[0].to - ex.groups[0].from + 1) / 100, v => {
                const size = Math.round(v * 100); ex.groups[0].from = 1; ex.groups[0].to = size;
                const remaining = 10000 - size;
                for (let j = 1; j < ex.groups.length; j++) { ex.groups[j].from = size + Math.floor((j - 1) * remaining / (ex.groups.length - 1)) + 1; ex.groups[j].to = size + Math.floor(j * remaining / (ex.groups.length - 1)); }
            }, "number", { min: 0.01, max: 99.99, step: 0.01 }),
            pvButton("Удалить эксперимент из конфигурации", () => variant.experiments.splice(i, 1))), "pv-experiment")),
        pvButton("Добавить эксперимент", () => {
            variant.experiments ||= [];
            const key = pvKey("e");
            state.expanded[`${variant.key}/experiment/${key}`] = true;
            variant.experiments.push({ key, name: "Эксперимент", active: !variant.experiments.some(e => e.active), groups: [{ key: "a", name: "A", from: 1, to: 5000 }, { key: "b", name: "B", from: 5001, to: 10000 }] });
        }, () => !enabled()));
}

app.collectionTypes.base.tabs.Variants = function(upsert) {
    if (upsert.collection.system) return t.p({ className: "txt-hint" }, "Настройки этой коллекции управляются системой.");
    const state = pvEditorState(upsert);
    const cid = upsert.collection.id;
    let errorBox;
    const root = t.div({ className: "collection-tab-content pv-editor" },
        t.p({ className: "txt-hint" }, "Один набор записей для пользователя. Правила проверяются сверху вниз; распределение экспериментов использует общий постоянный бакет."),
        t.div({ role: "alert", tabIndex: -1, className: "alert danger", hidden: () => !state.error, onmount: el => errorBox = el }, () => state.error),
        () => state.loading ? t.p(null, "Загрузка…") : !state.config ? t.p(null, "Сначала сохраните коллекцию.") : t.div(null,
            pvDisclosure(state, "settings", "Настройки",
                () => `${state.config.variables ? "Variants включены" : "Variants выключены"} · ${state.config.experiments ? "эксперименты включены" : "эксперименты выключены"}`,
                t.div(null,
                    pvSelect("Auth-коллекция", () => app.store.collections.filter(c => c.type === "auth" && !c.system).map(c => ({ value: c.id, label: c.name })), () => state.config.authCollection, v => state.config.authCollection = v, () => state.config.version > 0),
                    pvCheck("Variables — варианты контента", () => state.config.variables, v => { state.config.variables = v; if (!v) state.config.experiments = false; }),
                    pvCheck("Experiments — распределение по бакетам", () => state.config.experiments, v => state.config.experiments = v, () => !state.config.variables)), "", !state.config.version),
            t.h4({ className: "m-t-20 m-b-10" }, "Варианты"),
            t.div({ className: "pv-variants-list" },
                pvDisclosure(state, "variant/default", () => pvVariantLabel("default", state.config.default.experiments?.length > 0), "Если ни одно условие не совпало",
                    pvExperiments(state.config.default, () => state.config.experiments, state), "pv-variant"),
                () => state.config.variants.map((variant, i) => pvDisclosure(state, `variant/${variant.key}`,
                    () => pvVariantLabel(`${i + 1}. ${variant.name || "Новый вариант"}`, variant.experiments?.length > 0),
                    () => pvConditionSummary(variant.condition),
                    t.div(null,
                        pvInput(`Вариант ${i + 1}`, () => variant.name, v => variant.name = v),
                        t.div({ className: "flex flex-wrap gap-5 m-b-10" },
                            pvButton("Выше", () => { const a = state.config.variants; [a[i - 1], a[i]] = [a[i], a[i - 1]]; }, i === 0),
                            pvButton("Ниже", () => { const a = state.config.variants; [a[i + 1], a[i]] = [a[i], a[i + 1]]; }, () => i === state.config.variants.length - 1),
                            pvButton("Удалить вариант", () => state.config.variants.splice(i, 1))),
                        t.h5({ className: "m-b-10" }, "Условия аудитории"),
                        pvCondition(variant.condition, app.store.collections.find(c => c.id === state.config.authCollection), state, `${variant.key}/condition`),
                        t.h5({ className: "m-t-20 m-b-10" }, "Эксперименты"),
                        pvExperiments(variant, () => state.config.experiments, state)), "pv-variant"))),
            t.div({ className: "m-t-10 m-b-15" }, pvButton("Добавить вариант", () => {
                const key = pvKey("v");
                state.expanded[`variant/${key}`] = true;
                state.config.variants.push({ key, name: "Новый вариант", condition: pvField(), experiments: [] });
            }, () => !state.config.variables)),
            pvDisclosure(state, "access", "Исходные правила доступа", "Дополнительные ограничения для списка и просмотра", pvAccessRules(state, upsert.collection)),
            t.p({ className: "txt-hint" }, "Публикация применяется сразу. Запись контента становится доступна только суперпользователям. Удалённые наборы и история сохраняются."),
            pvButton("Опубликовать варианты", async () => {
                await pvPublishEditor(upsert, state);
                if (state.error) queueMicrotask(() => errorBox?.focus());
            }, () => state.busy || !state.config.authCollection),
            t.p({ role: "status" }, () => state.busy ? "Публикация…" : state.published ? `Опубликовано, версия ${state.config.version}` : ""),
            pvDisclosure(state, "sets", "Наборы записей", () => `${state.sets.length} наборов`, t.div(null,
            () => state.sets.map(s => t.div({ className: "m-b-5" }, t.span(null, s.name + (s.active ? " " : " (архив) ")), pvButton("Открыть записи набора", () => {
                app.modals.close(null, true); location.hash = `#/collections?collection=${encodeURIComponent(cid)}&filter=${encodeURIComponent(`content_set = '${s.id}'`)}`;
            }))))),
            pvDisclosure(state, "preview", "Предпросмотр опубликованных правил", "Проверить выбор для пользователя и историю назначений", t.div(null,
            pvButton("Выбрать пользователя", () => app.modals.openRecordsPicker({ collection: state.config.authCollection, maxSelect: 1, onselect: async records => {
                state.user = records[0]?.id || ""; if (!state.user) return;
                try {
                    const path = `/api/variants/admin/users/${state.config.authCollection}/${state.user}`;
                    const data = await app.pb.send(path, { query: { collection: cid } }); state.preview = data.items[0];
                    state.history = await app.pb.send(path + "/history", { query: { collection: cid, perPage: 20 } });
                } catch (err) { state.error = err.message; }
            } }), () => !state.config.version),
            () => state.preview ? t.div(null,
                t.p(null, `Пользователь ${state.user} · бакет ${state.preview.bucket || "ещё не назначен"}`),
                t.p(null, `Вариант: ${state.preview.variant} · эксперимент: ${state.preview.experiment || "—"} · группа: ${state.preview.group || "—"}`),
                t.p(null, `Причина: ${state.preview.reason} · версия: ${state.preview.version}`),
                t.p(null, `История: ${state.history?.totalItems || 0} назначений (последние 20). Предпросмотр не записывается.`),
                () => (state.history?.items || []).map(h => t.p(null, `${h.created} — ${h.decision.variant} / ${h.decision.experiment || "base"} / ${h.decision.group || "—"} · v${h.decision.version}`))) : null))));
    pvLoadEditor(upsert, state);
    return root;
};

// Content sets are selected on the collection page, never in the record form.
const pvIsSetField = props => props.field.name === "content_set" && pvManagedCollection(props.collection);
const pvRelationInput = app.fieldTypes.relation.input;
app.fieldTypes.relation.input = function(props) {
    return pvIsSetField(props) ? t.div({ className: "pv-content-set-field", hidden: true }) : pvRelationInput(props);
};
const pvRelationSave = app.fieldTypes.relation.onrecordsave;
app.fieldTypes.relation.onrecordsave = async function(props) {
    if (!pvIsSetField(props)) return pvRelationSave?.(props);
    if (props.originalRecord?.id) {
        props.payload.content_set = props.originalRecord.content_set;
        return;
    }
    // Apply at save time so restored drafts and duplicated records cannot carry
    // a stale set. A nested form for another collection uses its server default.
    const [route, query = ""] = location.hash.split("?");
    const params = new URLSearchParams(query);
    const sameCollection = [props.collection.id, props.collection.name].includes(params.get("collection"));
    props.payload.content_set = route === "#/collections" && sameCollection ? pvPresetFilter(params.get("filter")).set : "";
};
