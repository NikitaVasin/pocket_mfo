package polymorphicrelation

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Translate public expand paths to native relations so PocketBase itself performs
// batched loading, ViewRule checks and auth-field visibility handling.
func enrich(e *core.RecordEnrichEvent) error {
	r := e.Record
	fs := fields(r.Collection())
	if len(fs) == 0 {
		return e.Next()
	}
	info := e.RequestInfo
	original := info.Query["expand"]
	translated := expandPaths(r.Collection(), info, original)
	if info.Context == core.RequestInfoContextRealtime && translated != original {
		// Subscription option maps are shared between broadcasts. Never mutate them.
		// Reuse HTTP enrichment with an isolated query map after the native broadcast hook.
		if err := e.Next(); err != nil {
			return err
		}
		query := url.Values{}
		for k, v := range info.Query {
			query.Set(k, v)
		}
		query.Set("expand", translated)
		request, err := http.NewRequest(http.MethodGet, "http://localhost/?"+query.Encode(), nil)
		if err != nil {
			return err
		}
		for k, v := range info.Headers {
			request.Header.Set(strings.ReplaceAll(k, "_", "-"), v)
		}
		re := &core.RequestEvent{App: e.App, Auth: info.Auth}
		re.Request = request
		re.Set(core.RequestEventKeyInfoContext, info.Context)
		return apis.EnrichRecord(re, r)
	}
	if translated != original && info.Context != core.RequestInfoContextExpand {
		info.Query["expand"] = translated
		defer func() { info.Query["expand"] = original }()
	}
	if err := e.Next(); err != nil {
		return err
	}
	expanded := r.Expand()
	hadExpand := len(expanded) > 0
	for _, f := range fs {
		for _, id := range f.CollectionIDs {
			name := ServiceFieldName(f.Id, id)
			r.Hide(name)
			if value, ok := expanded[name]; ok {
				if !f.Hidden || info.HasSuperuserAuth() {
					expanded[f.Name] = value
				}
				delete(expanded, name)
			}
		}
	}
	if hadExpand {
		r.SetExpand(expanded)
	}
	return nil
}

func expandPaths(c *core.Collection, info *core.RequestInfo, original string) string {
	var paths []string
	for _, path := range strings.Split(original, ",") {
		head, tail, nested := strings.Cut(strings.TrimSpace(path), ".")
		f, ok := c.Fields.GetByName(head).(*Field)
		if !ok {
			paths = append(paths, path)
			continue
		}
		if f.Hidden && !info.HasSuperuserAuth() {
			continue
		}
		for _, id := range f.CollectionIDs {
			p := ServiceFieldName(f.Id, id)
			if nested {
				p += "." + tail
			}
			paths = append(paths, p)
		}
	}
	return strings.Join(paths, ",")
}

// PocketBase's auth response reads expand from the URL, unlike the Records API
// which uses RequestInfo.Query. Translate that URL only while the handler runs.
func expandAuthResponse(e *core.RecordAuthRequestEvent) error {
	info, err := e.RequestInfo()
	if err != nil {
		return err
	}
	authInfo := *info
	authInfo.Auth = e.Record
	original := e.Request.URL.RawQuery
	query := e.Request.URL.Query()
	if expand := query.Get("expand"); expand != "" {
		query.Set("expand", expandPaths(e.Record.Collection(), &authInfo, expand))
		e.Request.URL.RawQuery = query.Encode()
		defer func() { e.Request.URL.RawQuery = original }()
	}
	return e.Next()
}
