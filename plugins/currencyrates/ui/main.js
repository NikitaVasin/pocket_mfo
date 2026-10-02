document.head.append(t.link({ rel: "stylesheet", href: "/_/extensions/currencyrates/editor.css?v=1" }));
app.store.headerLinks = [...app.store.headerLinks, { label: "Курсы валют", href: "#/currency-rates", icon: "ri-exchange-line" }];
app.routes.superuserOnly("#/currency-rates", currencyRatesPage);

function currencyRatesPage() {
    app.store.title = "Курсы валют";
    const initialDate = app.utils.getHashQueryParams().date || "";
    const s = store({ date: /^\d{4}-\d{2}-\d{2}$/.test(initialDate) ? initialDate : "", latest: "", previousDate: "", rows: [], previous: {}, query: "", loading: true, error: "", comparisonError: false });
    let generation = 0;
    const api = app.pb.collection("currency_rates");
    const number = new Intl.NumberFormat("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 8 });
    const day = value => value?.slice(0, 10) || "";
    const dateLabel = value => value ? new Date(value + "T12:00:00Z").toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" }) : "Дата не выбрана";
    const rubles = value => number.format(value) + " ₽";
    const visibleRows = () => {
        const q = s.query.trim().toLocaleLowerCase("ru-RU");
        return s.rows.filter(r => !q || `${r.currency} ${r.name} ${r.numCode}`.toLocaleLowerCase("ru-RU").includes(q));
    };
    function delta(record) {
        const prior = s.previous[record.currency];
        if (prior === undefined) return t.span({ className: "cr-muted" }, "Нет сравнения");
        const diff = record.rate - prior;
        return t.span({ className: "cr-change" }, Math.abs(diff) < 1e-10 ? "Без изменений" : (diff > 0 ? "+" : "−") + rubles(Math.abs(diff)));
    }
    async function load(date = s.date) {
        const current = ++generation;
        s.loading = true; s.error = ""; s.comparisonError = false;
        try {
            const newest = await api.getList(1, 1, { sort: "-date", fields: "date", requestKey: null });
            if (current !== generation) return;
            const latest = day(newest.items[0]?.date);
            const selected = date || latest;
            const rows = selected ? await api.getFullList({ sort: "currency", filter: app.pb.filter("date = {:date}", { date: selected + " 00:00:00.000Z" }), requestKey: null }) : [];
            if (current !== generation) return;
            let previousDate = "", previous = {}, comparisonError = false;
            if (rows.length) {
                try {
                    const last = await api.getList(1, 1, { sort: "-date", fields: "date", filter: app.pb.filter("date < {:date}", { date: selected + " 00:00:00.000Z" }), requestKey: null });
                    previousDate = day(last.items[0]?.date);
                    if (previousDate) {
                        const records = await api.getFullList({ filter: app.pb.filter("date = {:date}", { date: previousDate + " 00:00:00.000Z" }), fields: "currency,rate", requestKey: null });
                        previous = Object.fromEntries(records.map(r => [r.currency, r.rate]));
                    }
                } catch { comparisonError = true; previousDate = ""; }
            }
            if (current !== generation) return;
            s.latest = latest; s.date = selected; s.rows = rows; s.previousDate = previousDate; s.previous = previous; s.comparisonError = comparisonError;
            app.utils.replaceHashQueryParams({ date: selected || null });
        } catch (error) {
            if (current === generation) s.error = error?.response?.message || "Не удалось загрузить курсы. Проверьте соединение и повторите запрос.";
        } finally { if (current === generation) s.loading = false; }
    }
    const uid = "cr-" + app.utils.randomString();
    return t.div({ className: "page" }, t.div({ className: "page-content cr-page", pbEvent: "pageCurrencyRates", onmount: () => load(), onunmount: () => generation++ },
        t.header({ className: "page-header" }, t.nav({ className: "breadcrumbs" }, t.div({ className: "breadcrumb-item" }, "Курсы валют"))),
        t.div({ className: "wrapper cr-dashboard" },
            t.section({ className: "cr-intro", ariaLabel: "Источник и дата курсов" },
                t.div(null, t.p({ className: "cr-eyebrow" }, "БАНК РОССИИ · RUB"), t.h1(null, "Курсы к рублю"), t.p({ className: "cr-muted" }, "Официальные значения ЦБ. Все курсы приведены к одной единице валюты.")),
                t.button({ className: "btn secondary", type: "button", disabled: () => s.loading, onclick: () => load() }, t.i({ className: "ri-refresh-line", ariaHidden: true }), "Обновить данные")),
            t.div({ className: "cr-toolbar" },
                t.div({ className: "field cr-date" }, t.label({ htmlFor: uid + "-date" }, "Дата действия курса"), t.input({ id: uid + "-date", type: "date", value: () => s.date, onchange: e => { if (e.target.value) { s.date = e.target.value; load(s.date); } } })),
                t.button({ className: "btn secondary", type: "button", disabled: () => s.loading || (!!s.latest && s.date === s.latest), onclick: () => load("") }, "Последние курсы"),
                t.p({ className: "cr-muted cr-date-note" }, () => s.latest ? `Последняя сохранённая дата: ${dateLabel(s.latest)}` : "История накапливается с момента подключения.")),
            () => s.error ? t.div({ className: "alert alert-danger", role: "alert" }, s.error, t.button({ type: "button", className: "btn secondary", onclick: () => load() }, "Повторить")) : null,
            t.p({ role: "status", hidden: () => !s.loading }, "Загрузка курсов…"),
            t.div({ hidden: () => s.loading || !!s.error },
                () => !s.rows.length ? t.section({ className: "cr-empty", role: "status" }, t.i({ className: "ri-exchange-line", ariaHidden: true }), t.h2(null, s.latest ? "На эту дату курсов нет" : "Курсы пока не загружены"), t.p({ className: "cr-muted" }, s.latest ? "Выберите другую дату или откройте последние сохранённые курсы. На выходные и пропущенные дни отдельные записи не создаются." : "Данные появятся после успешной загрузки с сайта ЦБ. Состояние задания можно проверить в разделе Cron.")) :
                    t.div({ className: "cr-content" },
                        t.section({ className: "cr-highlights", ariaLabel: "Основные валюты" }, ["USD", "EUR", "CNY"].map(code => {
                            const record = s.rows.find(r => r.currency === code);
                            return t.article({ className: "cr-highlight" }, t.div({ className: "cr-highlight-top" }, t.strong(null, code), t.span({ className: "cr-muted" }, "за 1 " + code)),
                                t.div({ className: "cr-value" }, record ? rubles(record.rate) : "Нет курса"), t.div({ className: "cr-highlight-bottom" }, record ? delta(record) : null, t.span({ className: "cr-muted" }, { USD: "Доллар США", EUR: "Евро", CNY: "Китайский юань" }[code])));
                        })),
                        t.p({ className: "cr-comparison cr-muted", role: "status" }, () => s.comparisonError ? "Курсы загружены, но сравнение недоступно. Повторите обновление данных." : s.previousDate ? `Изменение относительно ${dateLabel(s.previousDate)} — предыдущей сохранённой даты.` : "Для сравнения нужна ещё одна сохранённая дата."),
                        t.section({ className: "cr-list", ariaLabel: "Все валюты" },
                            t.div({ className: "cr-list-heading" }, t.div(null, t.h2(null, "Все валюты"), t.p({ className: "cr-muted" }, () => `${dateLabel(s.date)} · ${visibleRows().length} из ${s.rows.length}`)),
                                t.div({ className: "field cr-search" }, t.label({ htmlFor: uid + "-search" }, "Поиск валюты"), t.input({ id: uid + "-search", type: "search", placeholder: "Код, название или номер", value: () => s.query, oninput: e => { s.query = e.target.value; } }))),
                            t.table({ className: "cr-table" }, t.caption({ className: "cr-sr-only" }, () => "Курсы валют на " + dateLabel(s.date)),
                                t.thead(null, t.tr(null, t.th({ scope: "col" }, "Валюта"), t.th({ scope: "col", className: "cr-numeric" }, "За 1 единицу"), t.th({ scope: "col", className: "cr-numeric cr-desktop" }, "Изменение, ₽"), t.th({ scope: "col", className: "cr-numeric cr-desktop" }, "Номинал ЦБ"))),
                                t.tbody(null, () => visibleRows().map(record => t.tr(null,
                                    t.th({ scope: "row" }, t.strong({ className: "cr-code" }, record.currency), t.span({ className: "cr-name cr-muted" }, record.name), t.small({ className: "cr-mobile cr-muted" }, `${record.nominal} ${record.currency} = ${rubles(record.value)}`)),
                                    t.td({ className: "cr-numeric" }, t.strong(null, rubles(record.rate)), t.small({ className: "cr-mobile" }, delta(record))),
                                    t.td({ className: "cr-numeric cr-desktop" }, delta(record)),
                                    t.td({ className: "cr-numeric cr-desktop" }, `${record.nominal} ${record.currency}`, t.small({ className: "cr-muted" }, rubles(record.value))))))),
                            t.div({ className: "cr-empty", hidden: () => visibleRows().length > 0 }, t.p(null, "Валюта не найдена"), t.button({ className: "btn secondary", type: "button", onclick: () => { s.query = ""; } }, "Сбросить поиск"))))),
            t.footer({ className: "cr-footer cr-muted" }, t.span(null, "Обновление данных перечитывает сохранённые курсы. Загрузка из ЦБ выполняется сервером по расписанию."), t.span(null, "Курс за единицу = стоимость номинала ÷ номинал. История хранится до пяти лет.")))));
}
