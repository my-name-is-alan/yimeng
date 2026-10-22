package kugou

import (
	"bytes"
	"context"
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
	hGateway  = "https://gateway.kugou.com"
	hLoginQR  = "https://login-user.kugou.com"
	hLogin    = "http://login.user.kugou.com"
	hLoginSMS = "https://loginserviceretry.kugou.com"
	hUserSvc  = "https://userservice.kugou.com"
	hLyrics   = "https://lyrics.kugou.com"
	hImages   = "https://expendablekmr.kugou.com"
	hVIP      = "https://kugouvip.kugou.com"
)

// Jar keys. device.* survive logout and are persisted while anonymous.
const (
	kGUID   = online.DevicePrefix + "guid"
	kMid    = online.DevicePrefix + "mid"
	kDfid   = online.DevicePrefix + "dfid"
	kDfidAt = online.DevicePrefix + "dfid_at"
	kToken  = "token"
	kUserID = "userid"
	kVIP    = "vip_type"
	kT1     = "t1"
)

// Options lets tests send every host to a fake server.
type Options struct {
	// Rewrite maps a production base URL (e.g. https://gateway.kugou.com) to
	// the one actually used. Nil = production.
	Rewrite func(base string) string
}

type api struct {
	c    *online.Client
	opts Options
	now  func() time.Time
}

type sig int

const (
	sigAndroid sig = iota
	sigWeb
	sigNone
)

type req struct {
	method    string
	base      string
	path      string
	params    map[string]string // merged over default params
	noDefault bool
	body      any // map/struct → JSON, string/[]byte raw
	router    string
	headers   map[string]string
	sig       sig
	songKey   string // hash for song_url "key"
}

// ensureDevice creates the stable anonymous device identity once.
func (a *api) ensureDevice() {
	if a.c.Cookie(kGUID) == "" {
		g := newGUID()
		a.c.SetCookie(kGUID, g)
		a.c.SetCookie(kMid, calcMid(g))
	}
}

func (a *api) dfid() string {
	if d := a.c.Cookie(kDfid); d != "" {
		return d
	}
	return "-"
}

func (a *api) base(b string) string {
	if a.opts.Rewrite != nil {
		return a.opts.Rewrite(b)
	}
	return b
}

func (a *api) loggedIn() bool { return a.c.Cookie(kToken) != "" && a.c.Cookie(kUserID) != "" }

func (a *api) raw(ctx context.Context, r req) ([]byte, http.Header, error) {
	a.ensureDevice()
	mid := a.c.Cookie(kMid)
	ct := strconv.FormatInt(a.now().Unix(), 10)
	p := map[string]string{}
	if !r.noDefault {
		p = map[string]string{"dfid": a.dfid(), "mid": mid, "uuid": "-", "appid": appID, "clientver": clientVer, "clienttime": ct}
		if t := a.c.Cookie(kToken); t != "" {
			p["token"] = t
		}
		if u := a.c.Cookie(kUserID); u != "" && u != "0" {
			p["userid"] = u
		}
	}
	for k, v := range r.params {
		p[k] = v
	}
	if r.songKey != "" {
		p["key"] = signKey(r.songKey, p["mid"], p["userid"])
	}
	var body []byte
	switch b := r.body.(type) {
	case nil:
	case string:
		body = []byte(b)
	case []byte:
		body = b
	default:
		var err error
		if body, err = json.Marshal(b); err != nil {
			return nil, nil, err
		}
	}
	switch r.sig {
	case sigAndroid:
		p["signature"] = signAndroid(p, string(body))
	case sigWeb:
		p["signature"] = signWeb(p, string(body))
	}
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	b0 := r.base
	if b0 == "" {
		b0 = hGateway
	}
	u := a.base(b0) + r.path
	if len(q) > 0 {
		if strings.Contains(u, "?") {
			u += "&" + q.Encode()
		} else {
			u += "?" + q.Encode()
		}
	}
	method := r.method
	if method == "" {
		method = http.MethodGet
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	hr, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, nil, err
	}
	hr.Header.Set("User-Agent", userAgent)
	ctHeader := p["clienttime"]
	if ctHeader == "" {
		ctHeader = ct
	}
	for k, v := range map[string]string{"dfid": a.dfid(), "mid": mid, "clienttime": ctHeader, "kg-rc": "1", "kg-thash": "5d816a0", "kg-rec": "1", "kg-rf": "B9EDA08A64250DEFFBCADDEE00F8F25F"} {
		hr.Header.Set(k, v)
	}
	if body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	if r.router != "" {
		hr.Header.Set("x-router", r.router)
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	res, err := a.c.Do(hr)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", online.ErrUpstream, err)
	}
	if c := res.Header.Get("ssa-code"); c != "" {
		a.c.Penalize()
		return nil, res.Header, fmt.Errorf("%w: kugou asks for app verification (ssa-code %s)", online.ErrRiskControl, c)
	}
	if res.StatusCode >= 500 || res.StatusCode == 404 || res.StatusCode == 403 {
		return nil, res.Header, fmt.Errorf("%w: http %d", online.ErrUpstream, res.StatusCode)
	}
	return out, res.Header, nil
}

type envelope struct {
	Status    *flexInt `json:"status"`
	ErrorCode flexInt  `json:"error_code"`
	ErrCode   flexInt  `json:"errcode"`
	Error     any      `json:"error"`
	ErrMsg    string   `json:"errmsg"`
}

func (e envelope) msg() string {
	if s, ok := e.Error.(string); ok && s != "" {
		return s
	}
	return e.ErrMsg
}

// classify maps Kugou business errors onto the framework errors.
func classify(code int, msg string) error {
	switch code {
	case 0:
		return nil
	case 20018, 20017, 51002, 34175, 30768, 20019:
		return fmt.Errorf("%w: kugou %d %s", online.ErrNeedsLogin, code, msg)
	case 20028, 30791, 34126, 20031, 30795, 1002:
		return fmt.Errorf("%w: kugou %d %s", online.ErrRiskControl, code, msg)
	}
	return fmt.Errorf("%w: kugou %d %s", online.ErrUpstream, code, msg)
}

// call sends r and decodes JSON into v after checking status/error_code.
func (a *api) call(ctx context.Context, r req, v any) error {
	body, _, err := a.raw(ctx, r)
	if err != nil {
		return err
	}
	return decode(body, v)
}

func decode(body []byte, v any) error {
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return fmt.Errorf("%w: bad json: %v", online.ErrUpstream, err)
	}
	code := int(e.ErrorCode)
	if code == 0 {
		code = int(e.ErrCode)
	}
	if code != 0 && code != 200 { // lyrics.kugou.com answers errcode 200 on success
		return classify(code, e.msg())
	}
	if e.Status != nil && *e.Status == 0 {
		return fmt.Errorf("%w: kugou status 0 %s", online.ErrUpstream, e.msg())
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: decode: %v", online.ErrUpstream, err)
	}
	return nil
}

// registerDevice obtains a server-issued dfid (risk-control device id),
// refreshed weekly. Failure leaves dfid "-", which read-only endpoints accept.
func (a *api) registerDevice(ctx context.Context) error {
	a.ensureDevice()
	if a.c.Cookie(kDfid) != "" {
		if at, _ := strconv.ParseInt(a.c.Cookie(kDfidAt), 10, 64); a.now().Unix()-at < 7*86400 {
			return nil
		}
	}
	g := a.c.Cookie(kGUID)
	dev := map[string]any{
		"availableRamSize": 4983533568, "availableRomSize": 48114719, "availableSDSize": 48114717,
		"basebandVer": "", "batteryLevel": 100, "batteryStatus": 3, "brand": "Redmi", "buildSerial": "unknown",
		"device": "marble", "imei": g, "imsi": "", "manufacturer": "Xiaomi", "uuid": g,
		"accelerometer": false, "accelerometerValue": "", "gravity": false, "gravityValue": "",
		"gyroscope": false, "gyroscopeValue": "", "light": false, "lightValue": "", "magnetic": false,
		"magneticValue": "", "orientation": false, "orientationValue": "", "pressure": false, "pressureValue": "",
		"step_counter": false, "step_counterValue": "", "temperature": false, "temperatureValue": "",
	}
	plain, _ := json.Marshal(dev)
	enc, key := devAES(plain)
	uid := a.c.Cookie(kUserID)
	if uid == "" {
		uid = "0"
	}
	pj, _ := json.Marshal(map[string]any{"aes": key, "uid": uid, "token": a.c.Cookie(kToken)})
	body, _, err := a.raw(ctx, req{method: http.MethodPost, base: hUserSvc, path: "/risk/v2/r_register_dev",
		params: map[string]string{"part": "1", "platid": "1", "p": rsaPKCS1(pj)}, body: enc})
	if err != nil {
		return err
	}
	dec, err := devAESDecrypt(body, key)
	if err != nil {
		return fmt.Errorf("%w: register_dev: %v", online.ErrUpstream, err)
	}
	var r struct {
		Status int `json:"status"`
		Data   struct {
			Dfid string `json:"dfid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(dec, &r); err != nil || r.Status != 1 || r.Data.Dfid == "" {
		return fmt.Errorf("%w: register_dev status %d", online.ErrUpstream, r.Status)
	}
	a.c.SetCookie(kDfid, r.Data.Dfid)
	a.c.SetCookie(kDfidAt, strconv.FormatInt(a.now().Unix(), 10))
	return nil
}

// flexInt accepts 1, "1", 1.0 and null.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
		return nil
	}
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(x)
	return nil
}

// flexStr accepts strings and numbers.
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = flexStr(v)
		return nil
	}
	*f = flexStr(s)
	return nil
}
