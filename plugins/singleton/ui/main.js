// Native PocketBase v0.40.4 field editors; no frontend build or PocketBase fork.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/singleton/editor.css?v=2" }));
const api = "/api/singleton/admin/collections/";
const read = value => typeof value === "function" ? value() : value;
const enabled = c => c?.type === "base" && !c.system && c.indexes?.some(i => /\bidx_ps_[a-f0-9]{15}\b/.test(i));
const personalized = c => c?.fields?.some(f => f.name === "content_set" && app.store.collections.find(s => s.id === f.collectionId)?.name === "pv_sets");
const selectedSet = filter => (filter || "").match(/^content_set\s*=\s*['"]([a-z0-9]{15})['"]/i)?.[1] || "";
const button = (label, onclick, disabled = () => false, className = "btn secondary") => t.button({ type: "button", className, onclick, disabled }, label);
let activeEditor = null;
function leave() { return activeEditor?.leave() ?? true; }

app.collectionTypes.base.tabs.Singleton = function(upsert) {
    if (upsert.collection.system) return t.p({ className: "txt-hint" }, "Системные коллекции не поддерживают Singleton.");
    if (upsert.isNew) return t.p({ className: "txt-hint" }, "Сначала создайте коллекцию, затем включите Singleton в её настройках.");
    const state = upsert.singletonState ||= store({ loaded: false, loading: true, enabled: false, saved: false, busy: false, error: "", conflicts: [], applied: false });
    if (!state.loaded) {
        state.loaded = true;
        app.pb.send(api + upsert.collection.id, { requestKey: null }).then(cfg => { state.enabled = state.saved = cfg.enabled; })
            .catch(err => { state.error = err.message; state.loaded = false; }).finally(() => state.loading = false);
    }
    async function apply() {
        if (state.busy || upsert.isSaving) return;
        if (upsert.hasChanges) { state.error = "Сначала сохраните изменения схемы через Save and continue (Ctrl+S)."; return; }
        if (!leave()) return;
        state.busy = upsert.isSaving = true; state.error = ""; state.conflicts = []; state.applied = false;
        try {
            const cfg = await app.pb.send(api + upsert.collection.id, { method: "PUT", body: { enabled: state.enabled }, requestKey: null });
            state.enabled = state.saved = cfg.enabled;
            const schema = await app.pb.collections.getOne(upsert.collection.id);
            upsert.collection = structuredClone(schema); upsert.originalCollection = structuredClone(schema);
            await app.store.loadCollections();
            state.applied = true;
        } catch (err) { state.error = err.response?.message || err.message; state.conflicts = err.response?.data?.conflicts || []; }
        finally { state.busy = upsert.isSaving = false; }
    }
    const id = "singleton_" + upsert.collection.id;
    return t.div({ className: "collection-tab-content ps-settings" },
        t.p({ className: "txt-hint" }, "Singleton открывает форму вместо таблицы и запрещает создание второй записи. Пустая коллекция и удаление записи разрешены."),
        t.p({ className: "txt-hint", hidden: () => !personalized(upsert.collection) }, "По одной записи на каждый вариант и группу эксперимента"),
        t.div({ className: "field" }, t.input({ id, type: "checkbox", className: "switch", disabled: () => state.loading || state.busy,
            checked: () => state.enabled, onchange: e => { state.enabled = e.target.checked; state.applied = false; } }), t.label({ htmlFor: id }, "Одна запись на коллекцию")),
        t.p({ role: "alert", className: "txt-danger", hidden: () => !state.error, textContent: () => state.error }),
        t.ul(null, () => state.conflicts.map(c => t.li(null, `${c.name || c.set}: ${c.count} записей`))),
        button("Применить", apply, () => state.loading || state.busy || state.enabled === state.saved, "btn"),
        t.p({ role: "status", hidden: () => !state.applied }, "Настройка Singleton применена."));
};

// Scope replacements to the main page. The same components in relation pickers
// must retain their lists/search, even when their target is a singleton.
const originalSearchbar = app.components.recordsSearchbar;
app.components.recordsSearchbar = function(props = {}) {
    const state = store({ mounted: false, main: false });
    return t.div({ className: "ps-search-host", onmount: el => {
        state.main = !!el.parentElement?.classList.contains("page-content"); state.mounted = true;
    } }, () => {
        if (!state.mounted) return null;
        if (!state.main || !enabled(read(props.collection))) return originalSearchbar({ ...props, mainPage: state.main });
        return app.components.variantPresets?.({ ...props, beforechange: leave,
            onsubmit: filter => {
                props.onsubmit(filter);
                app.utils.replaceHashQueryParams({ filter, record: null }, true);
            } }) || null;
    });
};

const originalList = app.components.recordsList;
app.components.recordsList = function(props = {}) {
    // The main-page call has onchange; picker lists use onselect only.
    if (!props.onchange || !enabled(read(props.collection))) return originalList(props);
    return singletonForm(props);
};

function singletonForm(props) {
    const collection = read(props.collection);
    const state = store({ loading: true, loadFailed: false, busy: false, error: "", message: "", record: {}, original: {}, revision: 0, dirty: false, loadedSet: null });
    const watchers = [];
    let generation = 0, disposed = false;
    const serialize = r => JSON.stringify(r, (key, value) => key === "expand" || key.startsWith("@@") && key !== "@@filesToDelete" ? undefined : value);
    let fileDirty = false;
    const controller = {
        collection: collection.id,
        openRecord(record) {
            const filter = record?.content_set && personalized(collection) ? `content_set = '${record.content_set}'` : "";
            props.onchange(filter, "");
            app.utils.replaceHashQueryParams({ filter: filter || null, record: null, sort: null });
        },
        leave() {
            if (state.busy) return false;
            if (!state.dirty && !fileDirty) return true;
            if (!window.confirm("Есть несохранённые изменения. Покинуть форму без сохранения?")) return false;
            state.dirty = false; fileDirty = false; return true;
        },
        dirty: () => state.dirty || fileDirty,
    };
    function assign(record) {
        record = record?.__raw || record;
        state.original = structuredClone(record);
        state.record = structuredClone(record);
        fileDirty = false; state.dirty = false; state.revision++;
        app.store.errors = null;
    }
    async function load() {
        const filter = read(props.filter) || "";
        const set = personalized(collection) ? selectedSet(filter) : "";
        if (personalized(collection) && !set) return;
        const current = ++generation;
        state.loading = true; state.loadFailed = false; state.error = ""; state.message = "";
        try {
            const result = await app.pb.collection(collection.id).getList(1, 2, { filter: set ? app.pb.filter("content_set = {:set}", { set }) : "", requestKey: null });
            if (disposed || current !== generation) return;
            if (result.items.length > 1) throw new Error("В наборе больше одной записи. Проверьте настройку Singleton.");
            assign(result.items[0] || (set ? { content_set: set } : {}));
            state.loadedSet = set;
            // The inline editor owns direct record links too; no modal is needed.
            app.utils.replaceHashQueryParams({ record: null, sort: null });
        } catch (err) {
            if (!disposed && current === generation) { state.loadFailed = true; state.error = err.response?.message || err.message; }
        }
        finally { if (!disposed && current === generation) state.loading = false; }
    }
    async function save() {
        if (state.busy || state.loading || state.loadFailed || state.loadedSet === null) return;
        state.busy = true; state.error = ""; state.message = ""; app.store.errors = null;
        try {
            const payload = {};
            for (const key of Object.keys(state.record)) {
                if (key === "expand" || key.startsWith("@@")) continue;
                payload[key] = state.record[key]?.__raw ?? state.record[key] ?? null;
            }
            for (const field of collection.fields) {
                await app.fieldTypes[field.type]?.onrecordsave?.({ collection, originalRecord: state.original, record: state.record, field, payload });
            }
            if (personalized(collection)) payload.content_set = state.loadedSet;
            const isNew = !state.original.id;
            const record = isNew ? await app.pb.collection(collection.id).create(payload) : await app.pb.collection(collection.id).update(state.original.id, payload);
            assign(record); state.message = "Сохранено.";
            document.dispatchEvent(new CustomEvent(isNew ? "record:create" : "record:update", { detail: record }));
        } catch (err) {
            app.checkApiError(err, false);
            state.error = err.response?.data?.singleton?.message || err.response?.message || err.message;
        } finally { state.busy = false; }
    }
    async function remove() {
        if (!state.original.id || state.busy || state.loadFailed || !window.confirm("Удалить запись этого набора?")) return;
        state.busy = true; state.error = "";
        try {
            const record = structuredClone(state.original.__raw || state.original);
            await app.pb.collection(collection.id).delete(record.id);
            assign(state.loadedSet ? { content_set: state.loadedSet } : {}); state.message = "Запись удалена. Можно создать новую.";
            document.dispatchEvent(new CustomEvent("record:delete", { detail: record }));
        } catch (err) { state.error = err.response?.message || err.message; }
        finally { state.busy = false; }
    }
    const root = t.form({ className: "ps-record-form", onsubmit: e => { e.preventDefault(); save(); },
        oninput: e => { if (e.target.type === "file") fileDirty = true; },
        onchange: e => { if (e.target.type === "file") fileDirty = true; },
        onmount: () => {
            activeEditor = controller;
            watchers.push(watch(() => serialize(state.record), () => { state.dirty = serialize(state.record) !== serialize(state.original); }));
            watchers.push(watch(() => [read(props.filter), read(props.reset)], () => { if (controller.leave()) load(); }));
        },
        onunmount: () => { disposed = true; generation++; watchers.forEach(w => w?.unwatch()); if (activeEditor === controller) activeEditor = null; },
    },
    t.p({ role: "status", hidden: () => !state.loading }, "Загрузка записи…"),
    t.div({ role: "alert", className: "txt-danger", hidden: () => !state.error }, () => state.error,
        button("Обновить форму", () => { if (controller.leave()) load(); })),
    t.p({ role: "status", hidden: () => !state.message, textContent: () => state.message }),
    t.p({ className: "txt-hint", hidden: () => state.loading || state.loadedSet === null || !!state.original.id }, "Записи пока нет. Заполните поля и нажмите «Сохранить»."),
    t.fieldset({ disabled: () => state.busy || state.loading || state.loadFailed || state.loadedSet === null, className: "ps-fields" },
        () => {
            state.revision;
            if (state.loadedSet === null) return null;
            return collection.fields.filter(f => !f.primaryKey && !(f.name === "content_set" && personalized(collection)) && app.fieldTypes[f.type]?.input).map(field =>
                t.div({ className: "ps-field" }, app.fieldTypes[field.type].input({ collection, field,
                    get record() { return state.record; }, get originalRecord() { return state.original; } })));
        }),
    t.div({ className: "ps-actions flex flex-wrap gap-10" },
        t.button({ type: "submit", className: "btn", disabled: () => state.busy || state.loading || state.loadFailed || state.loadedSet === null }, () => state.busy ? "Сохранение…" : "Сохранить"),
        button("Сбросить", () => { if (controller.leave()) { assign(state.original); state.message = ""; state.error = ""; } }, () => state.busy || state.loading || state.loadFailed),
        button("Удалить", remove, () => state.busy || state.loading || state.loadFailed || !state.original.id, "btn danger")));
    return root;
}

// Hide only list-specific chrome, leaving collection settings/API preview intact.
watch(() => [app.store.activeCollection?.id, app.store.activeCollection?.indexes, app.store.page], () => {
    document.body.classList.toggle("ps-active", !!enabled(app.store.activeCollection));
});
document.addEventListener("click", e => {
    const nav = e.target.closest("[data-collection-id], a[href], .new-collection button");
    if (!nav || nav.matches("[data-collection-id]") && nav.dataset.collectionId === app.store.activeCollection?.id) return;
    if (nav.closest(".modal") || nav.getAttribute("target") === "_blank") return;
    if (!leave()) { e.preventDefault(); e.stopImmediatePropagation(); }
}, true);
window.addEventListener("beforeunload", e => { if (activeEditor?.dirty()) { e.preventDefault(); e.returnValue = ""; } });
// Capture runs before PocketBase's router and keeps the current form on cancel.
window.addEventListener("hashchange", e => {
    if (!leave()) {
        e.stopImmediatePropagation();
        history.replaceState(null, "", e.oldURL);
    }
}, true);

// Native record deep links should open inline, including links from API previews.
const originalOpenRecord = app.modals.openRecordUpsert;
app.modals.openRecordUpsert = function(collection, record, options) {
    if (!enabled(collection) || document.querySelector('.records-picker-modal[data-modal-state="open"]')) return originalOpenRecord(collection, record, options);
    if (!leave()) return;
    (async () => {
        try {
            const id = typeof record === "string" ? record : record?.id;
            const value = id ? await app.pb.collection(collection.id).getOne(id) : record;
            if (activeEditor?.collection === collection.id) {
                activeEditor.openRecord(value);
                return;
            }
            const filter = value?.content_set ? `content_set = '${value.content_set}'` : "";
            const hash = `#/collections?collection=${encodeURIComponent(collection.id)}` + (filter ? `&filter=${encodeURIComponent(filter)}` : "");
            if (location.hash !== hash) location.hash = hash;
        } catch (err) { app.toasts.error(err.message); }
    })();
};
