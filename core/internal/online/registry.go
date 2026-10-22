package online

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/vault"
	"rsc.io/qr"
)

// Common config keys every plugin gets (merged in front of its own fields).
const (
	KeyQuality = "quality"
	KeyRate    = "rate_per_min"
)

func f64(v float64) *float64 { return &v }

func commonFields(m Manifest) []ConfigField {
	opts := []ConfigOption{}
	labels := map[Quality]string{QStandard: "标准 (128k)", QHigh: "极高 (320k)", QLossless: "无损 (FLAC)", QHiRes: "Hi-Res"}
	for _, q := range m.Qualities {
		opts = append(opts, ConfigOption{Value: string(q), Label: labels[q]})
	}
	def := QHigh
	if len(m.Qualities) > 0 && QHigh.Rank() > m.Qualities[len(m.Qualities)-1].Rank() {
		def = m.Qualities[len(m.Qualities)-1]
	}
	rateDef := m.DefaultRate
	if rateDef == 0 {
		rateDef = 60
	}
	return []ConfigField{
		{Key: KeyQuality, Label: "音质偏好", Type: "select", Options: opts, Default: string(def),
			Help: "播放时请求的最高音质；账号无权限时由平台降级（Core 不会绕过限制）。"},
		{Key: KeyRate, Label: "请求频率上限（次/分钟）", Type: "number", Default: float64(rateDef), Min: f64(5), Max: f64(600),
			Help: "触发平台风控时会自动指数退避。"},
	}
}

type instance struct {
	factory  Factory
	manifest Manifest
	plugin   Plugin
	client   *Client
	enabled  bool
	config   Config
	method   LoginMethod
	loggedIn bool
	account  *Account
	accAt    time.Time
	lastErr  string
}

// Registry owns plugin instances, their settings (plugin_settings table) and
// their sessions (Vault entries "plugin.<id>").
type Registry struct {
	db    *sql.DB
	vault vault.Vault
	log   *slog.Logger
	hc    *http.Client

	mu    sync.Mutex
	inst  map[string]*instance
	order []string
}

func NewRegistry(db *sql.DB, v vault.Vault, log *slog.Logger, hc *http.Client) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{db: db, vault: v, log: log, hc: hc, inst: map[string]*instance{}}
}

func vaultKey(id string) string { return "plugin." + id }

// Register adds a plugin factory. The factory is called once with a zero Env
// to read the manifest, then again by Load with the real environment.
func (r *Registry) Register(f Factory) error {
	p, err := f(Env{HTTP: NewClient("probe", 60, r.hc), Config: Config{}})
	if err != nil {
		return err
	}
	m := p.Manifest()
	m.Config = append(commonFields(m), m.Config...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.inst[m.ID]; dup {
		return fmt.Errorf("online: duplicate plugin %q", m.ID)
	}
	r.inst[m.ID] = &instance{factory: f, manifest: m}
	r.order = append(r.order, m.ID)
	return nil
}

// Load reads settings and sessions and instantiates every plugin.
func (r *Registry) Load(ctx context.Context) error {
	r.mu.Lock()
	ids := append([]string(nil), r.order...)
	r.mu.Unlock()
	for _, id := range ids {
		if err := r.load(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) load(ctx context.Context, id string) error {
	var enabled bool
	var raw string
	err := r.db.QueryRowContext(ctx, `SELECT enabled, config FROM plugin_settings WHERE id=?`, id).Scan(&enabled, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	cfg := Config{}
	if raw != "" {
		json.Unmarshal([]byte(raw), &cfg)
	}
	r.mu.Lock()
	in := r.inst[id]
	r.mu.Unlock()
	cfg = fillDefaults(in.manifest, cfg)
	client := NewClient(id, cfg.Int(KeyRate, 60), r.hc)
	method := LoginMethod("")
	loggedIn := false
	if r.vault != nil {
		if c, err := r.vault.Get(ctx, vaultKey(id)); err == nil {
			client.ReplaceCookies(c.Fields)
			client.TakeDirty()
			method = LoginMethod(c.Method)
			loggedIn = hasSession(c.Fields)
		}
	}
	p, err := in.factory(Env{HTTP: client, Config: cfg})
	if err != nil {
		return err
	}
	r.mu.Lock()
	in.plugin, in.client, in.enabled, in.config, in.method, in.loggedIn = p, client, enabled, cfg, method, loggedIn
	r.mu.Unlock()
	return nil
}

func fillDefaults(m Manifest, c Config) Config {
	out := Config{}
	for _, f := range m.Config {
		if v, ok := c[f.Key]; ok {
			out[f.Key] = v
		} else if f.Default != nil {
			out[f.Key] = f.Default
		}
	}
	return out
}

// ValidateConfig checks values against the schema and returns a clean Config.
func ValidateConfig(m Manifest, in map[string]any) (Config, error) {
	out := Config{}
	byKey := map[string]ConfigField{}
	for _, f := range m.Config {
		byKey[f.Key] = f
	}
	for k, v := range in {
		f, ok := byKey[k]
		if !ok {
			return nil, fmt.Errorf("unknown config key %q", k)
		}
		switch f.Type {
		case "select":
			s, ok := v.(string)
			valid := false
			for _, o := range f.Options {
				valid = valid || o.Value == s
			}
			if !ok || !valid {
				return nil, fmt.Errorf("%s: invalid option", k)
			}
			out[k] = s
		case "number":
			n, ok := v.(float64)
			if !ok {
				if s, isStr := v.(string); isStr {
					if p, err := strconv.ParseFloat(s, 64); err == nil {
						n, ok = p, true
					}
				}
			}
			if !ok || (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
				return nil, fmt.Errorf("%s: out of range", k)
			}
			out[k] = n
		case "bool":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: must be boolean", k)
			}
			out[k] = b
		default:
			s, ok := v.(string)
			if !ok || len(s) > 500 {
				return nil, fmt.Errorf("%s: must be a short string", k)
			}
			out[k] = s
		}
	}
	return fillDefaults(m, out), nil
}

// Status is the client-facing view of a plugin. No secrets.
type Status struct {
	Manifest      Manifest    `json:"manifest"`
	Enabled       bool        `json:"enabled"`
	Config        Config      `json:"config"`
	LoggedIn      bool        `json:"logged_in"`
	LoginMethod   LoginMethod `json:"login_method,omitempty"`
	Account       *Account    `json:"account,omitempty"`
	CooldownUntil *time.Time  `json:"cooldown_until,omitempty"`
	LastError     string      `json:"last_error,omitempty"`
}

func (r *Registry) get(id string) (*instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.inst[id]
	if !ok || in.plugin == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknown, id)
	}
	return in, nil
}

func (r *Registry) status(in *instance) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := Status{Manifest: in.manifest, Enabled: in.enabled, Config: in.config, LoggedIn: in.loggedIn,
		LoginMethod: in.method, Account: in.account, LastError: in.lastErr}
	if t := in.client.CooldownUntil(); !t.IsZero() {
		st.CooldownUntil = &t
	}
	return st
}

// List returns all plugins in registration order. With check=true it
// revalidates sessions older than 10 minutes.
func (r *Registry) List(ctx context.Context, check bool) []Status {
	r.mu.Lock()
	ids := append([]string(nil), r.order...)
	r.mu.Unlock()
	out := make([]Status, 0, len(ids))
	for _, id := range ids {
		in, err := r.get(id)
		if err != nil {
			continue
		}
		if check {
			r.checkAccount(ctx, in, false)
		}
		out = append(out, r.status(in))
	}
	return out
}

func (r *Registry) Status(ctx context.Context, id string) (Status, error) {
	in, err := r.get(id)
	if err != nil {
		return Status{}, err
	}
	r.checkAccount(ctx, in, false)
	return r.status(in), nil
}

func (r *Registry) checkAccount(ctx context.Context, in *instance, force bool) {
	r.mu.Lock()
	need := in.loggedIn && (force || in.account == nil || time.Since(in.accAt) > 10*time.Minute)
	r.mu.Unlock()
	auth, ok := in.plugin.(Authenticator)
	if !need || !ok {
		return
	}
	cctx, cancel := withTimeout(ctx, 8*time.Second)
	defer cancel()
	acc, err := auth.Account(cctx)
	r.mu.Lock()
	switch {
	case err == nil:
		in.account, in.accAt, in.lastErr = &acc, time.Now(), ""
	case errors.Is(err, ErrNeedsLogin):
		in.account, in.lastErr = nil, "登录已失效，请重新登录"
	default:
		in.lastErr = err.Error()
	}
	r.mu.Unlock()
	r.Persist(ctx, in.manifest.ID)
}

// Configure updates enable flag and/or config and re-instantiates the plugin.
func (r *Registry) Configure(ctx context.Context, id string, enabled *bool, cfg map[string]any) (Status, error) {
	in, err := r.get(id)
	if err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	newCfg, newEnabled := in.config, in.enabled
	r.mu.Unlock()
	if cfg != nil {
		merged := map[string]any{}
		for k, v := range newCfg {
			merged[k] = v
		}
		for k, v := range cfg {
			merged[k] = v
		}
		if newCfg, err = ValidateConfig(in.manifest, merged); err != nil {
			return Status{}, err
		}
	}
	if enabled != nil {
		newEnabled = *enabled
	}
	raw, _ := json.Marshal(newCfg)
	if _, err := r.db.ExecContext(ctx, `INSERT INTO plugin_settings(id, enabled, config, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled, config=excluded.config, updated_at=excluded.updated_at`,
		id, newEnabled, string(raw), time.Now().UnixMilli()); err != nil {
		return Status{}, err
	}
	p, err := in.factory(Env{HTTP: in.client, Config: newCfg})
	if err != nil {
		return Status{}, err
	}
	in.client.SetRate(newCfg.Int(KeyRate, 60))
	r.mu.Lock()
	in.plugin, in.config, in.enabled = p, newCfg, newEnabled
	r.mu.Unlock()
	return r.status(in), nil
}

// Enabled returns an enabled plugin and its config.
func (r *Registry) Enabled(id string) (Plugin, Config, error) {
	in, err := r.get(id)
	if err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !in.enabled {
		return nil, nil, fmt.Errorf("%w: %s", ErrDisabled, id)
	}
	return in.plugin, in.config, nil
}

// EnabledIDs lists enabled plugins (registration order).
func (r *Registry) EnabledIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, id := range r.order {
		if in := r.inst[id]; in.plugin != nil && in.enabled {
			out = append(out, id)
		}
	}
	return out
}

// LoggedIn reports whether a plugin has a stored session.
func (r *Registry) LoggedIn(id string) bool {
	in, err := r.get(id)
	if err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return in.loggedIn
}

// Persist writes the cookie jar to the Vault when it changed while logged in.
func (r *Registry) Persist(ctx context.Context, id string) {
	in, err := r.get(id)
	if err != nil || r.vault == nil {
		return
	}
	r.mu.Lock()
	logged, method := in.loggedIn, in.method
	r.mu.Unlock()
	if !in.client.TakeDirty() {
		return
	}
	fields := in.client.Cookies()
	if !logged {
		// Anonymous: only the device identity (DevicePrefix keys) is kept.
		fields = deviceFields(fields)
		if len(fields) == 0 {
			return
		}
		method = ""
	}
	if err := r.vault.Put(ctx, vaultKey(id), vault.Credential{Method: vault.LoginMethod(method), Fields: fields}); err != nil {
		r.log.Error("plugin session persist", "plugin", id, "err", err)
	}
}

func (r *Registry) auth(id string) (*instance, Authenticator, error) {
	in, err := r.get(id)
	if err != nil {
		return nil, nil, err
	}
	a, ok := in.plugin.(Authenticator)
	if !ok {
		return nil, nil, ErrNotSupported
	}
	return in, a, nil
}

func (r *Registry) loggedIn(ctx context.Context, in *instance, m LoginMethod, acc Account) error {
	r.mu.Lock()
	in.loggedIn, in.method, in.account, in.accAt, in.lastErr = true, m, &acc, time.Now(), ""
	r.mu.Unlock()
	in.client.TakeDirty()
	if r.vault == nil {
		return nil
	}
	return r.vault.Put(ctx, vaultKey(in.manifest.ID), vault.Credential{Method: vault.LoginMethod(m), Fields: in.client.Cookies()})
}

func (r *Registry) StartQR(ctx context.Context, id string) (QRChallenge, error) {
	_, a, err := r.auth(id)
	if err != nil {
		return QRChallenge{}, err
	}
	ch, err := a.StartQR(ctx)
	if err != nil {
		return ch, err
	}
	if ch.Content != "" && ch.ImageURL == "" {
		code, err := qr.Encode(ch.Content, qr.M)
		if err != nil {
			return ch, err
		}
		code.Scale = 6
		ch.ImageURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())
	}
	return ch, nil
}

func (r *Registry) PollQR(ctx context.Context, id, key string) (QRPoll, error) {
	in, a, err := r.auth(id)
	if err != nil {
		return QRPoll{}, err
	}
	res, err := a.PollQR(ctx, key)
	if err != nil {
		return res, err
	}
	if res.Status == QRConfirmed {
		acc := Account{}
		if res.Account != nil {
			acc = *res.Account
		} else if got, err := a.Account(ctx); err == nil {
			acc = got
			res.Account = &got
		}
		if err := r.loggedIn(ctx, in, LoginQR, acc); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (r *Registry) LoginCookie(ctx context.Context, id, cookie string) (Account, error) {
	in, a, err := r.auth(id)
	if err != nil {
		return Account{}, err
	}
	acc, err := a.LoginCookie(ctx, cookie)
	if err != nil {
		return acc, err
	}
	return acc, r.loggedIn(ctx, in, LoginCookie, acc)
}

func (r *Registry) LoginPassword(ctx context.Context, id, user, pass string) (Account, error) {
	in, a, err := r.auth(id)
	if err != nil {
		return Account{}, err
	}
	acc, err := a.LoginPassword(ctx, user, pass)
	if err != nil {
		return acc, err
	}
	return acc, r.loggedIn(ctx, in, LoginPassword, acc)
}

func (r *Registry) smsAuth(id string) (*instance, SMSAuthenticator, error) {
	in, err := r.get(id)
	if err != nil {
		return nil, nil, err
	}
	a, ok := in.plugin.(SMSAuthenticator)
	if !ok {
		return nil, nil, ErrNotSupported
	}
	return in, a, nil
}

// SendSMS asks the platform to text a verification code to phone.
func (r *Registry) SendSMS(ctx context.Context, id, phone string) error {
	in, a, err := r.smsAuth(id)
	if err != nil {
		return err
	}
	err = a.SendSMS(ctx, phone)
	r.Persist(ctx, in.manifest.ID)
	return err
}

func (r *Registry) LoginSMS(ctx context.Context, id, phone, code string) (Account, error) {
	in, a, err := r.smsAuth(id)
	if err != nil {
		return Account{}, err
	}
	acc, err := a.LoginSMS(ctx, phone, code)
	if err != nil {
		return acc, err
	}
	return acc, r.loggedIn(ctx, in, LoginSMS, acc)
}

func (r *Registry) Refresh(ctx context.Context, id string) (Status, error) {
	in, a, err := r.auth(id)
	if err != nil {
		return Status{}, err
	}
	if err := a.Refresh(ctx); err != nil {
		return r.status(in), err
	}
	in.client.MarkDirty()
	r.Persist(ctx, id)
	r.checkAccount(ctx, in, true)
	return r.status(in), nil
}

// Logout forgets the session locally (Vault entry and jar).
func (r *Registry) Logout(ctx context.Context, id string) error {
	in, err := r.get(id)
	if err != nil {
		return err
	}
	dev := deviceFields(in.client.Cookies())
	in.client.ReplaceCookies(dev)
	in.client.TakeDirty()
	r.mu.Lock()
	in.loggedIn, in.account, in.method, in.lastErr = false, nil, "", ""
	r.mu.Unlock()
	if r.vault != nil {
		if len(dev) > 0 {
			return r.vault.Put(ctx, vaultKey(id), vault.Credential{Fields: dev})
		}
		if err := r.vault.Delete(ctx, vaultKey(id)); err != nil && !errors.Is(err, vault.ErrNotFound) {
			return err
		}
	}
	return nil
}

// DevicePrefix marks jar entries that are a plugin's device identity (e.g.
// Kugou dfid/mid) rather than a login session. They are persisted to the
// Vault even while anonymous and survive logout; they never count as login.
const DevicePrefix = "device."

func deviceFields(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if strings.HasPrefix(k, DevicePrefix) {
			out[k] = v
		}
	}
	return out
}

func hasSession(m map[string]string) bool {
	for k := range m {
		if !strings.HasPrefix(k, DevicePrefix) {
			return true
		}
	}
	return false
}

// IDs returns every registered plugin id, sorted by registration.
func (r *Registry) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.order...)
	return out
}

var _ = sort.Strings
