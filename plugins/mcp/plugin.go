// Package mcp exposes opt-in PocketBase tools over authenticated Streamable HTTP.
package mcp

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

//go:embed ui/*
var assets embed.FS

type Options struct {
	// Only these ordinary base collections can be exposed by content tools.
	ContentCollections []string
	// Browser origins are denied unless listed explicitly. Native clients omit Origin.
	AllowedOrigins []string
}

// Provider is implemented by plugins that opt into the MCP server.
// Tools must enforce their own domain invariants, just like their admin API.
type Provider interface{ MCPTools() []Tool }

type Tool struct {
	Name        string                                   `json:"name"`
	Description string                                   `json:"description"`
	InputSchema any                                      `json:"inputSchema"`
	ReadOnly    bool                                     `json:"readOnly"`
	Idempotent  bool                                     `json:"idempotent"`
	Handle      func(context.Context, Call) (any, error) `json:"-"`
}

type Call struct {
	App       core.App
	Key       Key
	Arguments json.RawMessage
}

// Server owns a registry per PocketBase application. Register providers before Start.
type Server struct {
	app   core.App
	opts  Options
	mu    sync.RWMutex
	tools map[string]Tool
}

func Register(app core.App, opts Options) *Server {
	s := &Server{app: app, opts: Options{slices.Clone(opts.ContentCollections), slices.Clone(opts.AllowedOrigins)}, tools: map[string]Tool{}}
	_ = s.Use(contentProvider{s})
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "mcp", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return install(e.App)
	}})
	protect(app)
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "mcp", Func: func(e *core.ServeEvent) error {
		ui, err := fs.Sub(assets, "ui")
		if err != nil {
			return err
		}
		e.UIExtensions = append(e.UIExtensions, core.UIExtension{Name: "mcp", FS: ui})
		for _, method := range []string{"GET", "POST", "DELETE"} {
			e.Router.Route(method, "/api/mcp", s.serve)
		}
		e.Router.GET("/api/mcp/admin/keys", func(r *core.RequestEvent) error {
			keys, err := listKeys(r.App)
			if err != nil {
				return err
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(200, map[string]any{"items": keys, "tools": s.Tools(), "collections": s.opts.ContentCollections, "endpoint": "/api/mcp"})
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.POST("/api/mcp/admin/keys", func(r *core.RequestEvent) error {
			var in Key
			if err := Decode(r.Request.Body, &in); err != nil {
				return r.BadRequestError("Некорректный ключ", nil)
			}
			in.Owner = r.Auth.Id
			key, token, err := s.CreateKey(in)
			if err != nil {
				return r.BadRequestError(err.Error(), nil)
			}
			r.Response.Header().Set("Cache-Control", "no-store")
			return r.JSON(201, map[string]any{"key": key, "token": token})
		}).Bind(apis.RequireSuperuserAuth())
		e.Router.DELETE("/api/mcp/admin/keys/{id}", func(r *core.RequestEvent) error {
			if err := Revoke(r.App, r.Request.PathValue("id")); err != nil {
				return r.NotFoundError("Ключ не найден", nil)
			}
			return r.NoContent(204)
		}).Bind(apis.RequireSuperuserAuth())
		return e.Next()
	}})
	return s
}

func (s *Server) Use(p Provider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := p.MCPTools()
	seen := map[string]bool{}
	for _, t := range pending {
		if t.Name == "" || strings.ContainsAny(t.Name, " /\n") || t.Handle == nil || t.InputSchema == nil {
			return fmt.Errorf("mcp: invalid tool")
		}
		if _, ok := s.tools[t.Name]; ok || seen[t.Name] {
			return fmt.Errorf("mcp: duplicate tool %s", t.Name)
		}
		seen[t.Name] = true
	}
	for _, t := range pending {
		s.tools[t.Name] = t
	}
	return nil
}

func (s *Server) Tools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		result = append(result, t)
	}
	slices.SortFunc(result, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	return result
}

func (s *Server) serve(r *core.RequestEvent) error {
	r.Response.Header().Set("Cache-Control", "no-store")
	if origin := r.Request.Header.Get("Origin"); origin != "" && !slices.Contains(s.opts.AllowedOrigins, origin) {
		return r.ForbiddenError("Origin не разрешён", nil)
	}
	parts := strings.Fields(r.Request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return r.UnauthorizedError("Требуется MCP Bearer key", nil)
	}
	key, err := authenticate(r.App, parts[1])
	if err != nil {
		return r.UnauthorizedError("MCP key недействителен", nil)
	}
	if r.Request.Method != "POST" {
		r.Response.Header().Set("Allow", "POST")
		return r.NoContent(405)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "pocket-mfo", Version: "1"}, nil)
	for _, t := range s.Tools() {
		if !slices.Contains(key.Tools, t.Name) {
			continue
		}
		server.AddTool(&sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: t.ReadOnly, IdempotentHint: t.Idempotent}}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			// Recheck revocation and rights at execution, including in-flight requests.
			current, err := authenticate(r.App, parts[1])
			if err != nil || !slices.Contains(current.Tools, t.Name) {
				return toolError("Доступ отозван"), nil
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			value, err := t.Handle(ctx, Call{App: r.App, Key: current, Arguments: req.Params.Arguments})
			if err != nil {
				return toolError(err.Error()), nil
			}
			data, err := json.Marshal(value)
			if err != nil {
				return toolError("Не удалось сформировать результат"), nil
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}, nil
		})
	}
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	r.Request.Body = http.MaxBytesReader(r.Response, r.Request.Body, 1<<20)
	h.ServeHTTP(r.Response, r.Request)
	return nil
}

func toolError(message string) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: message}}}
}

// Decode rejects bodies larger than 1 MiB, unknown fields and trailing JSON.
// Tool handlers must also validate values.
func Decode(reader io.Reader, value any) error {
	limited := &io.LimitedReader{R: reader, N: (1 << 20) + 1}
	d := json.NewDecoder(limited)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var tail any
	if err := d.Decode(&tail); err != io.EOF {
		return fmt.Errorf("ожидался один JSON объект")
	}
	if limited.N == 0 {
		return fmt.Errorf("JSON превышает 1 MiB")
	}
	return nil
}

func Object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
