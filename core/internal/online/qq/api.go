package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const (
	hMusicu  = "https://u.y.qq.com"
	hPtlogin = "https://xui.ptlogin2.qq.com"
	hSSL     = "https://ssl.ptlogin2.qq.com"
	hGraph   = "https://graph.qq.com"
	uaWeb    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// Jar keys. device.* survive logout and persist while anonymous.
const (
	kGUID       = online.DevicePrefix + "guid"
	kUin        = "uin"
	kMusicKey   = "qm_keyst"
	kRefreshKey = "refresh_key"
	kRefreshTok = "refresh_token"
	kVIP        = "vip"
)

// Options lets tests send every host to a fake server.
type Options struct {
	Rewrite func(base string) string // nil = production
}

type api struct {
	c    *online.Client
	opts Options
	now  func() time.Time
}

func (a *api) base(b string) string {
	if a.opts.Rewrite != nil {
		return a.opts.Rewrite(b)
	}
	return b
}

func (a *api) guid() string {
	g := a.c.Cookie(kGUID)
	if g == "" {
		g = strconv.FormatInt(1_000_000_000+rand.Int64N(8_999_999_999), 10)
		a.c.SetCookie(kGUID, g)
	}
	return g
}

// uin returns the numeric QQ / music id of the session ("0" anonymous).
func (a *api) uin() string {
	u := strings.TrimLeft(strings.TrimPrefix(a.c.Cookie(kUin), "o"), "0")
	if u == "" {
		return "0"
	}
	return u
}

func (a *api) loggedIn() bool { return a.uin() != "0" && a.c.Cookie(kMusicKey) != "" }

// mreq is one sub-request of a musicu.fcg batch.
type mreq struct {
	Module string         `json:"module"`
	Method string         `json:"method"`
	Param  map[string]any `json:"param"`
}

type subResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func classify(code int, where string) error {
	switch code {
	case 0:
		return nil
	case 1000, 1001, 104401:
		return fmt.Errorf("%w: qq %s code %d", online.ErrNeedsLogin, where, code)
	case 2001, 2000, 500003:
		return fmt.Errorf("%w: qq %s code %d", online.ErrRiskControl, where, code)
	}
	return fmt.Errorf("%w: qq %s code %d", online.ErrUpstream, where, code)
}

// mu posts a batch to musicu.fcg and returns each sub-response's data.
// A failing sub-request fails the call.
func (a *api) mu(ctx context.Context, reqs map[string]mreq) (map[string]json.RawMessage, error) {
	out, codes, err := a.muLoose(ctx, reqs)
	if err != nil {
		return nil, err
	}
	for k, c := range codes {
		if err := classify(c, reqs[k].Module); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// muLoose is mu without per-request failure; codes are returned instead.
func (a *api) muLoose(ctx context.Context, reqs map[string]mreq) (map[string]json.RawMessage, map[string]int, error) {
	return a.muComm(ctx, false, reqs)
}

// muComm: desktop=true sends the desktop-client comm (search only answers
// with results for it); otherwise the web comm (vkey needs it).
func (a *api) muComm(ctx context.Context, desktop bool, reqs map[string]mreq) (map[string]json.RawMessage, map[string]int, error) {
	comm := map[string]any{"uin": a.uin(), "format": "json", "ct": 24, "cv": 0, "platform": "yqq.json"}
	if desktop {
		comm = map[string]any{"uin": a.uin(), "format": "json", "ct": "19", "cv": "1859"}
	}
	if a.loggedIn() {
		comm["authst"] = a.c.Cookie(kMusicKey)
		comm["g_tk"] = gtk(a.c.Cookie(kMusicKey))
	}
	body := map[string]any{"comm": comm}
	for k, r := range reqs {
		body[k] = r
	}
	b, _ := json.Marshal(body)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base(hMusicu)+"/cgi-bin/musicu.fcg", bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Referer", "https://y.qq.com/")
	hr.Header.Set("Origin", "https://y.qq.com")
	hr.Header.Set("User-Agent", uaWeb)
	if a.loggedIn() {
		hr.Header.Set("Cookie", "uin="+a.uin()+"; qm_keyst="+a.c.Cookie(kMusicKey)+"; qqmusic_key="+a.c.Cookie(kMusicKey))
	}
	res, err := a.c.Do(hr)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", online.ErrUpstream, err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%w: musicu http %d", online.ErrUpstream, res.StatusCode)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil, fmt.Errorf("%w: musicu json: %v", online.ErrUpstream, err)
	}
	var code int
	json.Unmarshal(top["code"], &code)
	if err := classify(code, "musicu"); err != nil {
		return nil, nil, err
	}
	out, codes := map[string]json.RawMessage{}, map[string]int{}
	for k := range reqs {
		var s subResp
		if err := json.Unmarshal(top[k], &s); err != nil {
			return nil, nil, fmt.Errorf("%w: musicu %s missing", online.ErrUpstream, k)
		}
		out[k], codes[k] = s.Data, s.Code
	}
	return out, codes, nil
}

// one sends a single sub-request and decodes its data into v.
func (a *api) one(ctx context.Context, module, method string, param map[string]any, v any) error {
	out, err := a.mu(ctx, map[string]mreq{"req_0": {module, method, param}})
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(out["req_0"], v); err != nil {
		return fmt.Errorf("%w: %s.%s decode: %v", online.ErrUpstream, module, method, err)
	}
	return nil
}

// hash33 is ptlogin's ptqrtoken hash of qrsig.
func hash33(s string) int {
	h := 0
	for _, c := range s {
		h += (h << 5) + int(c)
	}
	return h & 0x7fffffff
}

// gtk is the g_tk/bkn hash of skey / p_skey / musickey.
func gtk(s string) int {
	h := 5381
	for _, c := range s {
		h += (h << 5) + int(c)
	}
	return h & 0x7fffffff
}
