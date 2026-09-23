package push

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/mcp"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

func Register(app core.App, options Options) *Plugin {
	return register(app, options, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}
func register(app core.App, options Options, client *http.Client) *Plugin {
	options.AuthCollections = slices.Clone(options.AuthCollections)
	setManaged(app, options.Managed)
	app.Store().Set(adminLockStoreKey, options.LockAdminConfig)
	p := &Plugin{app: app, options: options, client: client, analyticsGate: make(chan struct{}, 1), analyticsCache: map[string]analyticsCacheEntry{}, overviewCache: map[string]AnalyticsOverview{}}
	lifetime, stop := context.WithCancel(context.Background())
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{Id: "push", Func: func(e *core.TerminateEvent) error {
		stop()
		app.Cron().Remove("push_dispatch")
		p.worker.Lock()
		p.worker.Unlock()
		return e.Next()
	}})
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "push", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return e.App.RunInTransaction(func(tx core.App) error {
			if err := install(tx); err != nil {
				return err
			}
			return applyManaged(tx)
		})
	}})
	protect(app)
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "push", Func: func(e *core.ServeEvent) error {
		if err := applyManaged(e.App); err != nil {
			return err
		}
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "push", FS: ui})
		if err = e.App.Cron().Add("push_dispatch", "* * * * *", func() {
			ctx, cancel := context.WithTimeout(lifetime, 50*time.Second)
			defer cancel()
			if err := p.Process(ctx); err != nil {
				e.App.Logger().Error("push: queue processing failed")
			}
		}); err != nil {
			return err
		}
		e.Router.GET("/api/push/admin/state", func(r *core.RequestEvent) error {
			result, err := p.state(r.App)
			if err != nil {
				return err
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, result)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.PUT("/api/push/admin/config", func(r *core.RequestEvent) error {
			if r.App.Store().Get(adminLockStoreKey) == true {
				return r.ForbiddenError("Настройки закреплены в Go-коде", nil)
			}
			var c Config
			if err := mcp.Decode(r.Request.Body, &c); err != nil {
				return r.BadRequestError("Некорректные настройки", nil)
			}
			result, err := Configure(r.App, c)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			result.OAuthToken = ""
			return r.JSON(200, result)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/push/admin/{action}", func(r *core.RequestEvent) error {
			var args json.RawMessage
			if err := mcp.Decode(r.Request.Body, &args); err != nil {
				return r.BadRequestError("Некорректный JSON", nil)
			}
			value, err := p.call(r.Request.Context(), r.App, r.Request.PathValue("action"), args)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			return r.JSON(200, value)
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/push/devices", func(r *core.RequestEvent) error {
			var in DeviceInput
			if err := mcp.Decode(r.Request.Body, &in); err != nil {
				return r.BadRequestError("Некорректное устройство", nil)
			}
			id, err := p.RegisterDevice(r.App, r.Auth, in)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			return r.JSON(200, map[string]string{"id": id})
		}).Bind(apis.RequireAuth())
		e.Router.POST("/api/push/devices/disable", func(r *core.RequestEvent) error {
			var in struct {
				ID     string `json:"id"`
				Secret string `json:"secret"`
			}
			if err := mcp.Decode(r.Request.Body, &in); err != nil {
				return r.BadRequestError("Некорректное устройство", nil)
			}
			if err := p.DisableDevice(r.App, r.Auth, in.ID, in.Secret); err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			return r.NoContent(204)
		}).Bind(apis.RequireAuth())
		e.Router.POST("/api/push/open", func(r *core.RequestEvent) error {
			var in OpenInput
			if err := mcp.Decode(r.Request.Body, &in); err != nil {
				return r.BadRequestError("Некорректное открытие", nil)
			}
			if err := p.TrackOpen(r.App, r.Auth, in); err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			return r.NoContent(204)
		}).Bind(apis.RequireAuth())
		return e.Next()
	}})
	if options.MCP != nil {
		if err := options.MCP.Use(p); err != nil {
			panic(err)
		}
	}
	return p
}
func (p *Plugin) state(app core.App) (map[string]any, error) {
	c, err := Load(app)
	if err != nil {
		return nil, err
	}
	c.OAuthToken = ""
	a, err := definitions(app, AudiencesCollection)
	if err != nil {
		return nil, err
	}
	campaigns, err := definitions(app, CampaignsCollection)
	if err != nil {
		return nil, err
	}
	runs, err := p.Runs(app)
	if err != nil {
		return nil, err
	}
	devices, err := app.FindRecordsByFilter(DevicesCollection, "", "-lastSeen", 100, 0)
	if err != nil {
		return nil, err
	}
	dd := []any{}
	for _, d := range devices {
		dd = append(dd, map[string]any{"id": d.Id, "userId": d.GetString("userId"), "platform": d.GetString("platform"), "language": d.GetString("language"), "appVersion": d.GetString("appVersion"), "enabled": d.GetBool("enabled"), "notificationPermission": d.GetString("notificationPermission"), "lastSeen": d.GetString("lastSeen")})
	}
	return map[string]any{"config": c, "configLocks": settingsLocks(app), "oauthClientId": p.options.OAuthClientID, "audiences": a, "campaigns": campaigns, "runs": runs, "devices": dd, "fields": p.AudienceFields(app), "authCollections": p.options.AuthCollections}, nil
}
func (p *Plugin) call(ctx context.Context, app core.App, action string, args json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decode := func(v any) error { return mcp.Decode(bytes.NewReader(args), v) }
	switch action {
	case "audiences":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		return definitions(app, AudiencesCollection)
	case "campaigns":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		return definitions(app, CampaignsCollection)
	case "fields":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		return p.AudienceFields(app), nil
	case "audience_save":
		var in Audience
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.SaveAudience(app, normalizeCondition(in))
	case "audience_preview":
		var in Audience
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.AudiencePreview(app, normalizeCondition(in))
	case "campaign_save":
		var in Campaign
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.SaveCampaign(app, in)
	case "preview":
		var in struct {
			CampaignID string `json:"campaignId"`
		}
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.Preview(app, in.CampaignID)
	case "launch", "test":
		var in Launch
		if err := decode(&in); err != nil {
			return nil, err
		}
		if action == "test" && len(in.TestDeviceIDs) == 0 {
			return nil, textError("выберите тестовые устройства")
		}
		if action == "launch" && len(in.TestDeviceIDs) > 0 {
			return nil, textError("для теста используйте push_test")
		}
		return p.Launch(app, in)
	case "runs":
		if err := decode(&struct{}{}); err != nil {
			return nil, err
		}
		return p.Runs(app)
	case "report", "refresh_report", "cancel":
		var in struct {
			RunID string `json:"runId"`
		}
		if err := decode(&in); err != nil {
			return nil, err
		}
		if action == "report" {
			return p.Report(app, in.RunID)
		}
		if action == "refresh_report" {
			return p.RefreshReport(ctx, app, in.RunID)
		}
		err := p.Cancel(app, in.RunID)
		return map[string]any{"cancelled": err == nil}, err
	case "analytics_overview":
		var in OverviewInput
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.AnalyticsOverview(ctx, app, in)
	case "analytics":
		var in AnalyticsInput
		if err := decode(&in); err != nil {
			return nil, err
		}
		return p.Analytics(ctx, app, in)
	default:
		return nil, textError("неизвестное действие")
	}
}

func (p *Plugin) MCPTools() []mcp.Tool {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	ids := map[string]any{"type": "array", "items": str}
	audience := mcp.Object(map[string]any{"id": str, "version": num, "name": str, "authCollection": str, "condition": map[string]any{"type": "object"}, "userIds": ids, "excludeUserIds": ids, "deviceIds": ids}, "name", "authCollection", "version")
	campaign := mcp.Object(map[string]any{"id": str, "version": num, "name": str, "audienceIds": ids, "allUsers": map[string]any{"type": "boolean", "description": "true: все пользователи подключённых auth-коллекций; audienceIds должен быть пустым. По умолчанию false."}, "excludeAudienceIds": ids, "lastDeviceOnly": map[string]any{"type": "boolean"}, "cooldownHours": num, "message": mcp.Object(map[string]any{"title": str, "text": str, "image": str, "action": map[string]any{"type": "string", "enum": []string{"app", "route", "partner"}}, "target": str}, "title", "text", "action")}, "name", "version", "message")
	launch := mcp.Object(map[string]any{"campaignId": str, "version": num, "idempotencyKey": str, "scheduledAt": str}, "campaignId", "version", "idempotencyKey")
	test := mcp.Object(map[string]any{"campaignId": str, "version": num, "idempotencyKey": str, "testDeviceIds": ids}, "campaignId", "version", "idempotencyKey", "testDeviceIds")
	result := []mcp.Tool{}
	for _, item := range []struct {
		name, description    string
		schema               any
		readOnly, idempotent bool
	}{
		{"fields", "Поля для условий аудитории. kind: all/any/not с children; field с source user/device/conversion, field, op eq/ne/gt/gte/lt/lte/empty/withinHours/olderHours, value; conversion с одним children (условия одной заявки); variant с collection, variant, experiment, group.", mcp.Object(map[string]any{}), true, true},
		{"audiences", "Сохранённые аудитории", mcp.Object(map[string]any{}), true, true},
		{"audience_save", "Создать/изменить аудиторию с контролем version; ручные userIds ограничивают выборку, excludeUserIds исключают.", audience, false, false},
		{"audience_preview", "Подсчитать аудиторию без сохранения", audience, true, true},
		{"campaigns", "Сохранённые кампании; создание не запускает отправку", mcp.Object(map[string]any{}), true, true},
		{"campaign_save", "Сохранить черновик кампании; version=0 для новой. Получатели: allUsers=true или непустой audienceIds. Сохранение не запускает отправку", campaign, false, false},
		{"preview", "Количество пользователей и устройств кампании", mcp.Object(map[string]any{"campaignId": str}, "campaignId"), true, true},
		{"launch", "Запустить реальную рассылку по явному поручению пользователя. Один idempotencyKey на логический запуск; при повторе запроса сохраняйте его. scheduledAt опционально RFC3339.", launch, false, true},
		{"test", "Отправить только на явно выбранные тестовые устройства", test, false, true},
		{"runs", "Последние 100 запусков", mcp.Object(map[string]any{}), true, true},
		{"report", "Статус, открытия через приложение, бизнес-конверсии и AppMetrica group ID", mcp.Object(map[string]any{"runId": str}, "runId"), true, true},
		{"analytics_overview", "Сравнение последних 10 кампаний с реальными отправками на конец периода. Дневные метрики AppMetrica и итоги; даты YYYY-MM-DD UTC, максимум 90 дней.", mcp.Object(map[string]any{"dateFrom": str, "dateTo": str}, "dateFrom", "dateTo"), true, true},
		{"analytics", "Метрики только из AppMetrica: доставка/открытия, этапы конверсии и revenue RUB. scope=run (по умолчанию) или campaign (все реальные запуски). Кэш 5 минут; ошибки разделов не заменяются нулями.", mcp.Object(map[string]any{"runId": str, "scope": map[string]any{"type": "string", "enum": []string{"run", "campaign"}}}, "runId"), true, true},
		{"refresh_report", "Восстановить причины старых неудачных отправок из AppMetrica (до 20 пакетов). Только запрос статуса, без повторной отправки", mcp.Object(map[string]any{"runId": str}, "runId"), false, true},
		{"cancel", "Отменить запуск до наступления расписания", mcp.Object(map[string]any{"runId": str}, "runId"), false, true},
	} {
		result = append(result, mcp.Tool{Name: "push_" + item.name, Description: item.description, InputSchema: item.schema, ReadOnly: item.readOnly, Idempotent: item.idempotent, Handle: func(ctx context.Context, call mcp.Call) (any, error) {
			return p.call(ctx, call.App, item.name, call.Arguments)
		}})
	}
	return result
}
