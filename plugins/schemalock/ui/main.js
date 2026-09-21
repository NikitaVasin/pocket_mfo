// PocketBase v0.40.4 extension. Server authorization is independent of this UI.
document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/schemalock/editor.css?v=1" }));
document.body.classList.add("schema-locked");
const blockedSettings = new Set(["#/settings/import-collections", "#/settings/sql"]);
app.store.settingsNavGroups = Object.fromEntries(Object.entries(app.store.settingsNavGroups)
    .map(([name, links]) => [name, links.filter(link => !blockedSettings.has(link.href))])
    .filter(([, links]) => links.length));

for (const path of blockedSettings) {
    app.routes.superuserOnly(path, () => {
        location.replace("#/settings");
        return t.div();
    });
}

const canEditVariants = collection => collection?.id && collection.type === "base" && !collection.system && !!app.collectionTypes.base.tabs.Variants;
const clone = value => JSON.parse(JSON.stringify(value));

app.modals.openCollectionUpsert = function(collection) {
    if (!canEditVariants(collection)) {
        app.toasts.error("Схема коллекции управляется серверным кодом.");
        return;
    }
    const upsert = store({ collection: clone(collection), originalCollection: clone(collection), isNew: false, hasChanges: false, isSaving: false, readOnlyRules: true });
    const titleId = "sl_variants_" + app.utils.randomString();
    const modal = t.div({
        className: "modal popup sl-variants-modal",
        role: "dialog", ariaModal: "true", ariaLabelledby: titleId,
        onbeforeclose: () => {
            if (upsert.isSaving) return false;
            const state = upsert.pvEditor;
            if (!state?.config || !state.savedConfig || JSON.stringify(state.config) === state.savedConfig) return true;
            return new Promise(resolve => app.modals.confirm("Есть неопубликованные изменения Variants. Закрыть окно?", () => resolve(true), () => resolve(false)));
        },
        onafterclose: el => el.remove(),
    },
    t.header({ className: "modal-header" }, t.h5({ id: titleId, className: "modal-title" }, `Variants · ${collection.name}`)),
    t.div({ className: "modal-content", inert: () => upsert.isSaving }, app.collectionTypes.base.tabs.Variants(upsert)),
    t.footer({ className: "modal-footer" }, t.button({ type: "button", className: "btn secondary", disabled: () => upsert.isSaving, onclick: () => app.modals.close(modal) }, "Закрыть")));
    document.body.append(modal);
    app.modals.open(modal);
};

// Replace the schema action with a dedicated Variants action. Mount events are
// the native extension mechanism; no DOM polling or copied collection page.
document.addEventListener("mount:pageHeaderSecondaryBtns", event => {
    const old = event.detail.querySelector(".btn-collection-settings");
    if (!old) return;
    old.replaceWith(t.button({
        type: "button", className: "btn secondary sl-open-variants",
        hidden: () => !canEditVariants(app.store.activeCollection),
        onclick: () => app.modals.openCollectionUpsert(app.store.activeCollection),
    }, "Variants"));
});
document.addEventListener("mount:pageCollections", event => {
    event.detail.querySelectorAll(".new-collection").forEach(el => el.remove());
});

const systemReadOnly = collection => collection?.system && collection.name !== "_superusers";
watch(() => systemReadOnly(app.store.activeCollection), locked => document.body.classList.toggle("sl-system-active", !!locked));
const originalRecordUpsert = app.modals.openRecordUpsert;
app.modals.openRecordUpsert = function(collection, record, options) {
    if (!systemReadOnly(collection)) return originalRecordUpsert(collection, record, options);
    const id = typeof record === "string" ? record : record?.id;
    if (id) return app.modals.openRecordPreview({ id, collectionId: collection.id });
    app.toasts.error("Системные записи доступны только для чтения.");
};
const originalRecordsList = app.components.recordsList;
app.components.recordsList = function(props = {}) {
    return originalRecordsList({ ...props, collection: () => {
        const collection = typeof props.collection === "function" ? props.collection() : props.collection;
        // Native view lists already suppress creation and bulk deletion.
        return systemReadOnly(collection) ? { ...collection, type: "view" } : collection;
    } });
};
