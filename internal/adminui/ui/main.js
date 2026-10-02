// Hide navigation entries only. Never filter app.store.collections: native
// relation pickers and plugin pages need these collections and their schemas.
const ownedNames = new Set([
    "appmetrica_config", "currency_rates", "dynamic_link_settings", "mcp_keys",
    "partner_links", "conversations", "pl_config", "ps_configs",
    "pv_configs", "pv_sets", "pv_states", "pv_history",
    "push_config", "push_devices", "push_audiences", "push_campaigns",
    "push_runs", "push_jobs", "push_opens",
]);
const pluginCollection = c => !!c && (ownedNames.has(c.name) || (c.system && /^pv_choice_[a-f0-9]{16}$/.test(c.name)));
document.head.append(t.style(null, () => {
    const selectors = [...ownedNames, ...app.store.collections.filter(pluginCollection).map(c => c.name)]
        .map(name => `[title="${CSS.escape(name)}"]`).join(",");
    return `.collections-sidebar .nav-item:is(${selectors}) { display: none !important; }
        .collections-sidebar .nav-group:not(:has(.nav-item:not(:is(${selectors})))) { display: none !important; }`;
}));

app.store.headerLinks = app.store.headerLinks.map(link => link.href === "#/collections" ? {
    ...link,
    isActive: el => link.isActive?.(el) || app.utils.isActivePath("#/collections"),
    get href() {
        const visible = app.store.collections.filter(c => !pluginCollection(c));
        const active = app.store.activeCollection;
        const saved = localStorage.getItem("pbLastActiveCollection");
        const target = visible.find(c => c.id === active?.id) ||
            visible.find(c => [c.id, c.name].includes(saved)) ||
            visible.find(c => !c.system) || visible[0];
        return target ? "#/collections?collection=" + encodeURIComponent(target.name) : "#/collections";
    },
} : link);
