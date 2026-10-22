package netease

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const (
	defaultDomain     = "https://music.163.com"
	defaultEapiDomain = "https://interface.music.163.com"
	uaWeb             = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	uaClient          = "NeteaseMusic 9.0.90/5038 (iPhone; iOS 16.2; zh_CN)"
)

// Options lets tests point the plugin at a fake server.
type Options struct {
	Domain     string // weapi host
	EapiDomain string // eapi host
}

type api struct {
	c    *online.Client
	opts Options
}

// apiError carries a non-200 business code.
type apiError struct {
	Code int
	Msg  string
}

func (e *apiError) Error() string { return fmt.Sprintf("netease: code %d %s", e.Code, e.Msg) }

func classify(code int, msg string) error {
	switch code {
	case 200, 0:
		return nil
	case 301, 302:
		return fmt.Errorf("%w: %s", online.ErrNeedsLogin, msg)
	case -462, 405, 429, 8810, 8821:
		return fmt.Errorf("%w: code %d %s", online.ErrRiskControl, code, msg)
	case 404:
		return fmt.Errorf("%w: %s", online.ErrUnavailable, msg)
	}
	return fmt.Errorf("%w: %v", online.ErrUpstream, &apiError{code, msg})
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ensureDevice creates stable anonymous device cookies once per jar.
func (a *api) ensureDevice() {
	if a.c.Cookie("deviceId") == "" {
		a.c.SetCookie("deviceId", strings.ToUpper(randHex(16)))
	}
	if a.c.Cookie("_ntes_nuid") == "" {
		a.c.SetCookie("_ntes_nuid", randHex(16))
	}
	if a.c.Cookie("NMTID") == "" {
		a.c.SetCookie("NMTID", "00O"+randHex(10))
	}
}

func (a *api) post(ctx context.Context, u string, form url.Values, cookie, ua, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Cookie", cookie)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := a.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", online.ErrUpstream, err)
	}
	if res.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: http %d", online.ErrUpstream, res.StatusCode)
	}
	return body, nil
}

type envelope struct {
	Code    json.Number `json:"code"`
	Msg     string      `json:"msg"`
	Message string      `json:"message"`
}

func decode(body []byte, v any) error {
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return fmt.Errorf("%w: bad json: %v", online.ErrUpstream, err)
	}
	code, _ := strconv.Atoi(e.Code.String())
	msg := e.Msg
	if msg == "" {
		msg = e.Message
	}
	if err := classify(code, msg); err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: decode: %v", online.ErrUpstream, err)
	}
	return nil
}

// weapi posts to <domain>/weapi/<uri without "/api/">.
func (a *api) weapi(ctx context.Context, uri string, data map[string]any, v any) error {
	body, err := a.weapiRaw(ctx, uri, data)
	if err != nil {
		return err
	}
	return decode(body, v)
}

func (a *api) weapiRaw(ctx context.Context, uri string, data map[string]any) ([]byte, error) {
	a.ensureDevice()
	if data == nil {
		data = map[string]any{}
	}
	data["csrf_token"] = a.c.Cookie("__csrf")
	text, _ := json.Marshal(data)
	params, key := weapiEncrypt(text)
	cookie := a.c.CookieHeader(map[string]string{"os": "pc", "appver": "3.1.17.204416", "__remember_me": "true"})
	return a.post(ctx, a.opts.Domain+"/weapi/"+strings.TrimPrefix(uri, "/api/"), url.Values{"params": {params}, "encSecKey": {key}}, cookie, uaWeb, a.opts.Domain)
}

// eapi posts to <eapi domain>/eapi/<uri without "/api/">.
func (a *api) eapi(ctx context.Context, uri string, data map[string]any, v any) error {
	body, err := a.eapiRaw(ctx, uri, data)
	if err != nil {
		return err
	}
	return decode(body, v)
}

func (a *api) eapiRaw(ctx context.Context, uri string, data map[string]any) ([]byte, error) {
	a.ensureDevice()
	if data == nil {
		data = map[string]any{}
	}
	header := map[string]string{
		"osver": "16.2", "deviceId": a.c.Cookie("deviceId"), "os": "iPhone OS", "appver": "9.0.90",
		"versioncode": "140", "mobilename": "", "buildver": strconv.FormatInt(time.Now().Unix(), 10),
		"resolution": "1920x1080", "__csrf": a.c.Cookie("__csrf"), "channel": "distribution",
		"requestId": fmt.Sprintf("%d_%04d", time.Now().UnixMilli(), time.Now().Nanosecond()%1000),
	}
	if v := a.c.Cookie("MUSIC_U"); v != "" {
		header["MUSIC_U"] = v
	}
	if v := a.c.Cookie("MUSIC_A"); v != "" {
		header["MUSIC_A"] = v
	}
	data["header"] = header
	data["e_r"] = false
	text, _ := json.Marshal(data)
	params := eapiEncrypt(uri, text)
	parts := make([]string, 0, len(header))
	for k, v := range header {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
	}
	return a.post(ctx, a.opts.EapiDomain+"/eapi/"+strings.TrimPrefix(uri, "/api/"), url.Values{"params": {params}}, strings.Join(parts, "; "), uaClient, "")
}
