// Demo partner is a local test cabinet, not a production affiliate network.
package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed ui/*
var assets embed.FS

type settings struct {
	URL            string `json:"url"`
	Method         string `json:"method"`
	SecretLocation string `json:"secretLocation"`
	SecretName     string `json:"secretName"`
	Secret         string `json:"secret,omitempty"`
	HasSecret      bool   `json:"hasSecret,omitempty"`
}
type conversion struct {
	ID             string     `json:"id"`
	Token          string     `json:"token,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	Status         string     `json:"status"`
	Amount         string     `json:"amount"`
	Currency       string     `json:"currency"`
	EventID        string     `json:"eventId"`
	EventTimestamp int64      `json:"eventTimestamp"`
	Attempts       int        `json:"attempts"`
	ResponseCode   int        `json:"responseCode"`
	Delivery       string     `json:"delivery"`
	SentAt         *time.Time `json:"sentAt,omitempty"`
}
type state struct {
	Settings settings     `json:"settings"`
	Records  []conversion `json:"records"`
}
type cabinet struct {
	mu         sync.Mutex
	deliveryMu sync.Mutex
	file       string
	state      state
	client     *http.Client
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func openCabinet(file string) (*cabinet, error) {
	c := &cabinet{file: file, client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.state = state{Settings: settings{URL: env("POSTBACK_URL", "http://127.0.0.1:8090/api/partnerlinks/postbacks/demo"), Method: "POST JSON", SecretLocation: "query", SecretName: "secret", Secret: "public-demo-postback-secret"}, Records: []conversion{}}
	b, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(b, &c.state)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return c, err
}

// Write a complete new snapshot before publishing it to readers.
func (c *cabinet) save(s state) error {
	if err := os.MkdirAll(filepath.Dir(c.file), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.file), ".partner-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), c.file); err != nil {
		return err
	}
	c.state = s
	return nil
}
func (c *cabinet) snapshot() state { s := c.state; s.Records = slices.Clone(s.Records); return s }
func jsonResponse(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "Нужен Content-Type application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "Некорректные параметры")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		fail(w, 400, "Некорректный JSON")
		return false
	}
	return true
}
func (c *cabinet) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("GET /click", c.click)
	mux.HandleFunc("GET /api/conversions", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		rows := slices.Clone(c.state.Records)
		for i := range rows {
			rows[i].Token = ""
		}
		jsonResponse(w, 200, rows)
	})
	mux.HandleFunc("POST /api/conversions/{id}/status", c.changeStatus)
	mux.HandleFunc("POST /api/conversions/{id}/retry", c.retry)
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		s := c.state.Settings
		s.HasSecret = s.Secret != ""
		s.Secret = ""
		jsonResponse(w, 200, s)
	})
	mux.HandleFunc("PUT /api/settings", c.configure)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		if name != "/index.html" && name != "/main.js" && name != "/style.css" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("ui" + name)
		switch name {
		case "/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case "/main.js":
			w.Header().Set("Content-Type", "text/javascript")
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
		}
		_, _ = w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					fail(w, 403, "Запрос из другого сайта запрещён")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

func (c *cabinet) click(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	token := r.URL.Query().Get("subid")
	if !tokenPattern.MatchString(token) {
		fail(w, 400, "Откройте оффер из приложения: в ссылке нужен subid")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, row := range c.state.Records {
		if row.Token == token {
			http.Redirect(w, r, "https://www.google.com/", 302)
			return
		}
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		fail(w, 500, "Не удалось создать заявку")
		return
	}
	s := c.snapshot()
	s.Records = append(s.Records, conversion{ID: hex.EncodeToString(id), Token: token, CreatedAt: time.Now().UTC(), Status: "pending", Amount: "100", Currency: "RUB", Delivery: "Не отправлен"})
	if err := c.save(s); err != nil {
		fail(w, 500, "Не удалось сохранить переход")
		return
	}
	http.Redirect(w, r, "https://www.google.com/", 302)
}
func (c *cabinet) configure(w http.ResponseWriter, r *http.Request) {
	var input settings
	if !decode(w, r, &input) {
		return
	}
	u, err := url.Parse(input.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		fail(w, 400, "Укажите HTTP(S) URL постбека без логина и фрагмента")
		return
	}
	if !slices.Contains([]string{"GET", "POST JSON", "POST form"}, input.Method) || !slices.Contains([]string{"query", "header", "body"}, input.SecretLocation) || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`).MatchString(input.SecretName) || strings.ContainsAny(input.Secret, "\r\n") || (input.Method == "GET" && input.SecretLocation == "body") {
		fail(w, 400, "Проверьте формат и способ передачи секрета; GET не имеет body")
		return
	}
	if slices.Contains([]string{"subid", "status", "lead_id", "event_id", "timestamp", "amount", "currency"}, input.SecretName) {
		fail(w, 400, "Имя секрета пересекается с полями постбека")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot()
	if input.Secret == "" {
		input.Secret = s.Settings.Secret
	}
	input.HasSecret = false
	s.Settings = input
	if err := c.save(s); err != nil {
		fail(w, 500, "Не удалось сохранить настройки")
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,8})?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (c *cabinet) changeStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status   string `json:"status"`
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !slices.Contains([]string{"lead", "hold", "approved", "rejected"}, input.Status) || !amountPattern.MatchString(input.Amount) || !currencyPattern.MatchString(input.Currency) {
		fail(w, 400, "Выберите статус, положительную сумму и валюту из трёх букв")
		return
	}
	c.deliveryMu.Lock()
	defer c.deliveryMu.Unlock()
	c.mu.Lock()
	s := c.snapshot()
	index := slices.IndexFunc(s.Records, func(v conversion) bool { return v.ID == r.PathValue("id") })
	if index < 0 {
		c.mu.Unlock()
		fail(w, 404, "Заявка не найдена")
		return
	}
	row := &s.Records[index]
	row.Status = input.Status
	row.Amount = input.Amount
	row.Currency = input.Currency
	row.EventTimestamp = time.Now().Unix()
	row.EventID = fmt.Sprintf("%s-%d", row.ID, time.Now().UnixNano())
	row.Delivery = "Ожидает отправки"
	row.ResponseCode = 0
	if err := c.save(s); err != nil {
		c.mu.Unlock()
		fail(w, 500, "Не удалось сохранить статус")
		return
	}
	cfg := s.Settings
	copy := *row
	c.mu.Unlock()
	c.send(w, r, cfg, copy)
}
func (c *cabinet) retry(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !decode(w, r, &input) {
		return
	}
	c.deliveryMu.Lock()
	defer c.deliveryMu.Unlock()
	c.mu.Lock()
	index := slices.IndexFunc(c.state.Records, func(v conversion) bool { return v.ID == r.PathValue("id") })
	if index < 0 {
		c.mu.Unlock()
		fail(w, 404, "Заявка не найдена")
		return
	}
	row := c.state.Records[index]
	cfg := c.state.Settings
	c.mu.Unlock()
	if row.Status == "pending" {
		fail(w, 400, "Сначала выберите статус заявки")
		return
	}
	c.send(w, r, cfg, row)
}
func postbackRequest(cfg settings, row conversion) (*http.Request, error) {
	fields := map[string]string{"subid": row.Token, "status": row.Status, "lead_id": row.ID, "event_id": row.EventID, "timestamp": fmt.Sprint(row.EventTimestamp), "amount": row.Amount, "currency": row.Currency}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if cfg.SecretLocation == "query" {
		q.Set(cfg.SecretName, cfg.Secret)
	}
	if cfg.SecretLocation == "body" {
		fields[cfg.SecretName] = cfg.Secret
	}
	method := "POST"
	var body io.Reader
	contentType := ""
	switch cfg.Method {
	case "GET":
		method = "GET"
		for k, v := range fields {
			q.Set(k, v)
		}
	case "POST JSON":
		b, _ := json.Marshal(fields)
		body = bytes.NewReader(b)
		contentType = "application/json"
	case "POST form":
		form := url.Values{}
		for k, v := range fields {
			form.Set(k, v)
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	default:
		return nil, errors.New("unknown method")
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cfg.SecretLocation == "header" {
		req.Header.Set(cfg.SecretName, cfg.Secret)
	}
	return req, nil
}
func (c *cabinet) send(w http.ResponseWriter, r *http.Request, cfg settings, row conversion) {
	req, err := postbackRequest(cfg, row)
	code := 0
	delivery := "Ошибка соединения; повторите отправку"
	if err == nil {
		res, e := c.client.Do(req.WithContext(r.Context()))
		if e == nil {
			code = res.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			_ = res.Body.Close()
			delivery = fmt.Sprintf("HTTP %d", code)
		}
	}
	if code == 200 {
		delivery = "Доставлен"
	}
	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.snapshot()
	index := slices.IndexFunc(s.Records, func(v conversion) bool { return v.ID == row.ID })
	s.Records[index].ResponseCode = code
	s.Records[index].Delivery = delivery
	s.Records[index].Attempts++
	s.Records[index].SentAt = &now
	if err := c.save(s); err != nil {
		fail(w, 500, "Постбек отправлен, но результат не сохранён; проверьте получателя перед повтором")
		return
	}
	result := s.Records[index]
	result.Token = ""
	jsonResponse(w, 200, result)
}
func main() {
	c, err := openCabinet(env("PARTNER_DATA", "./pb_data/demo-partner.json"))
	if err != nil {
		log.Fatal("Cannot load demo partner state")
	}
	log.Print("Demo partner listening on ", env("PARTNER_ADDR", "127.0.0.1:8091"))
	server := http.Server{Addr: env("PARTNER_ADDR", "127.0.0.1:8091"), Handler: c.handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
