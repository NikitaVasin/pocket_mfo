// PocketBase v0.40.4 native UI extension. Uses its own Shablon runtime and UI kit.
const polymorphicType = "polymorphicRelation";

// Hide generated columns in the admin's schema presentation. The server recreates
// them when a collection is saved; API schema exports remain complete.
const previousAfterSend = app.pb.afterSend;
app.pb.afterSend = async (response, data) => {
    if (previousAfterSend) data = await previousAfterSend(response, data);
    // Record payloads may themselves contain a JSON property named "fields".
    // Only collection list/detail responses describe a schema.
    if (/^\/api\/collections(?:\/[^/]+)?\/?$/.test(new URL(response.url).pathname)) {
        const collections = Array.isArray(data) ? data : data?.items || [data];
        for (const collection of collections) {
            if (!Array.isArray(collection?.fields)) continue;
            const names = new Set(collection.fields.filter(f => f.type === polymorphicType)
                .flatMap(f => Object.values(f.relations || {})));
            collection.fields = collection.fields.filter(f => !names.has(f.name));
            collection.indexes = collection.indexes?.filter(index =>
                ![...names].some(name => index.includes(`idx_${collection.id}_${name}`)));
        }
    }
    return data;
};

function collectionOptions() {
    return app.utils.sortedCollections(app.store.collections.filter(c => !c.system && c.type !== "view"))
        .map(c => ({ value: c.id, label: c.name }));
}

app.fieldTypes[polymorphicType] = {
    icon: "ri-mind-map",
    label: "Polymorphic relation",
    dummyData: f => ({ collectionId: f.collectionIds?.[0] || "COLLECTION_ID", recordId: "RECORD_ID" }),
    settings(props) {
        const uid = "pmr_" + app.utils.randomString();
        return app.components.fieldSettings(props, {
            header: [t.div({ className: "field header-select collections-select" }, app.components.select({
                required: true, max: Number.MAX_SAFE_INTEGER, placeholder: "Select collections*",
                name: () => `fields.${props.fieldIndex}.collectionIds`,
                options: collectionOptions, value: () => props.field.collectionIds || [],
                onchange: options => { props.field.collectionIds = options.map(o => o.value); },
            }))],
            content: () => t.div({ className: "grid sm" },
                t.div({ className: "col-sm-12 field" },
                    t.label({ htmlFor: uid + "_delete" }, "When parent is deleted"),
                    app.components.select({
                        id: uid + "_delete", required: true,
                        name: () => `fields.${props.fieldIndex}.onDelete`,
                        options: () => [
                            { value: "restrict", label: "Prevent deletion" },
                            ...(!props.field.required ? [{ value: "setNull", label: "Clear relation" }] : []),
                            { value: "cascade", label: "Delete child record" },
                        ],
                        value: () => props.field.onDelete || "restrict",
                        onchange: options => { props.field.onDelete = options[0]?.value || "restrict"; },
                    })),
                t.div({ className: "col-sm-12 field" },
                    t.label({ htmlFor: uid + "_help" }, "Help text"),
                    t.input({ id: uid + "_help", type: "text", value: () => props.field.help || "",
                        oninput: e => { props.field.help = e.target.value; } })),
            ),
            footer: () => t.div({ className: "field" },
                t.input({ id: uid + "_required", type: "checkbox", className: "sm",
                    checked: () => !!props.field.required,
                    onchange: e => {
                        props.field.required = e.target.checked;
                        if (props.field.required && props.field.onDelete === "setNull") props.field.onDelete = "restrict";
                    } }),
                t.label({ htmlFor: uid + "_required" }, "Required")),
        });
    },
    input(props) {
        const name = props.field.name;
        const uid = "pmr_input_" + app.utils.randomString();
        // PocketBase may remount the input when record data changes. Persist this
        // draft-only choice using its reserved @@ namespace (excluded from payloads).
        const draftKey = "@@pmr:" + props.field.id;
        const local = store({ collectionId: props.record[name]?.collectionId || props.record[draftKey] || props.field.collectionIds?.[0] || "" });
        function changed() { root.dispatchEvent(new CustomEvent("change", { bubbles: true })); }
        const root = t.div({ className: "record-field-input field-type-relation" },
            t.div({ className: () => `field-list ${props.field.required ? "required" : ""}` },
                t.label({ htmlFor: uid }, t.i({ className: "ri-mind-map", ariaHidden: true }), t.span({ className: "txt" }, name)),
                t.output({ className: "field-content", name },
                    app.components.select({ id: uid, required: true,
                        options: () => collectionOptions().filter(o => props.field.collectionIds?.includes(o.value)),
                        value: () => local.collectionId,
                        onchange: options => {
                            const id = options[0]?.value || "";
                            if (id !== local.collectionId) {
                                props.record[draftKey] = id;
                                local.collectionId = id; props.record[name] = null;
                                if (props.record.expand) delete props.record.expand[name];
                                changed();
                            }
                        },
                    }),
                    // Native list styles are scoped to .list, including the
                    // flex row and the fixed-width action area.
                    t.div({ className: "list" }, () => {
                        if (!props.record[name]) return null;
                        return t.div({ className: "list-item highlight" },
                            t.div({ className: "content" }, () => app.fieldTypes[polymorphicType].view(props)),
                            t.div({ className: "actions" },
                                t.button({ type: "button", className: "btn sm secondary transparent circle", ariaLabel: "Remove relation",
                                    onclick: () => { props.record[name] = null; if (props.record.expand) delete props.record.expand[name]; changed(); } },
                                    t.i({ className: "ri-close-line", ariaHidden: true }))));
                    }),
                    t.hr({ hidden: () => !!props.record[name], className: "m-t-5 m-b-0" }),
                    t.button({ type: "button", className: "btn sm secondary block", disabled: () => !local.collectionId,
                        onclick: () => app.modals.openRecordsPicker({
                            collection: local.collectionId, maxSelect: 1,
                            selectedIds: props.record[name]?.recordId ? [props.record[name].recordId] : [],
                            onselect: records => {
                                props.record[name] = records[0] ? { collectionId: local.collectionId, recordId: records[0].id } : null;
                                props.record.expand = props.record.expand || {};
                                props.record.expand[name] = records[0]; changed();
                            },
                        }) }, t.i({ className: "ri-magic-line", ariaHidden: true }), t.span({ className: "txt" }, "Open records picker")),
                )),
            () => props.field.help ? t.div({ className: "field-help" }, props.field.help) : null,
        );
        return root;
    },
    view(props) {
        const ref = props.record[props.field.name];
        if (!ref) return t.span({ className: "missing-value" });
        const collection = app.store.collections.find(c => c.id === ref.collectionId);
        // Reuse native relation previews and batched lazy loading with an adapted record.
        const adapted = store({ ...props.record, [props.field.name]: ref.recordId });
        return t.div({ className: "record-field-view field-type-relation" },
            t.span({ className: "label" }, collection?.name || ref.collectionId),
            app.fieldTypes.relation.view({ ...props, record: adapted,
                field: { ...props.field, type: "relation", collectionId: ref.collectionId, maxSelect: 1 } }));
    },
};
