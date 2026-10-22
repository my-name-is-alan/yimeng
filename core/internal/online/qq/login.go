package qq

// Our own QR login: QQ ptlogin (scan with the QQ app) → QQ Connect OAuth for
// QQ Music's app id → musicu QQConnectLogin. Only the resulting musicid and
// musickey (+ refresh tokens) are kept, in the Vault-backed jar; transient
// ptlogin cookies live in a throwaway jar and are discarded.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const (
	ptAppID     = "716027609"
	ptDaid      = "383"
	oauthClient = "100497308"
	loginJump   = "https://graph.qq.com/oauth2.0/login_jump"
)

// loginClient is a throwaway cookie-jar client that does not follow redirects.
func (p *Plugin) loginClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	var tr http.RoundTripper
	if p.c.HC != nil {
		tr = p.c.HC.Transport
	}
	return &http.Client{Jar: jar, Transport: tr, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (p *Plugin) get(ctx context.Context, hc *http.Client, u, referer string) (*http.Response, []byte, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	r.Header.Set("User-Agent", uaWeb)
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	res, err := hc.Do(r)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", online.ErrUpstream, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res, b, nil
}

func cookieFrom(res *http.Response, name string) string {
	for _, c := range res.Cookies() {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// rewriteURL maps an absolute production URL through Options.Rewrite (tests).
func (p *Plugin) rewriteURL(u string) string {
	if p.opts.Rewrite == nil {
		return u
	}
	pu, err := url.Parse(u)
	if err != nil {
		return u
	}
	return p.opts.Rewrite(pu.Scheme+"://"+pu.Host) + pu.RequestURI()
}

func (p *Plugin) StartQR(ctx context.Context) (online.QRChallenge, error) {
	hc := p.loginClient()
	q := url.Values{"appid": {ptAppID}, "daid": {ptDaid}, "style": {"33"}, "login_text": {"登录"}, "hide_title_bar": {"1"},
		"hide_border": {"1"}, "target": {"self"}, "s_url": {loginJump}, "pt_3rd_aid": {oauthClient}, "theme": {"2"}}
	loginSig := ""
	if res, _, err := p.get(ctx, hc, p.base(hPtlogin)+"/cgi-bin/xlogin?"+q.Encode(), ""); err == nil {
		loginSig = cookieFrom(res, "pt_login_sig")
	}
	q = url.Values{"appid": {ptAppID}, "e": {"2"}, "l": {"M"}, "s": {"3"}, "d": {"72"}, "v": {"4"},
		"t": {fmt.Sprintf("%.6f", float64(p.now().UnixNano()%1e9)/1e9)}, "daid": {ptDaid}, "pt_3rd_aid": {oauthClient}, "u1": {loginJump}}
	res, png, err := p.get(ctx, hc, p.base(hPtlogin)+"/ssl/ptqrshow?"+q.Encode(), "https://xui.ptlogin2.qq.com/")
	if err != nil {
		return online.QRChallenge{}, err
	}
	qrsig := cookieFrom(res, "qrsig")
	if res.StatusCode != http.StatusOK || qrsig == "" || len(png) == 0 {
		return online.QRChallenge{}, fmt.Errorf("%w: ptqrshow http %d", online.ErrUpstream, res.StatusCode)
	}
	return online.QRChallenge{Key: qrsig + "|" + loginSig, ImageURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		ExpiresAt: p.now().Add(2 * time.Minute)}, nil
}

var ptuiRe = regexp.MustCompile(`'([^']*)'`)

func (p *Plugin) PollQR(ctx context.Context, key string) (online.QRPoll, error) {
	qrsig, loginSig, _ := strings.Cut(key, "|")
	if qrsig == "" {
		return online.QRPoll{}, fmt.Errorf("%w: qr key", online.ErrBadInput)
	}
	hc := p.loginClient()
	u0, _ := url.Parse(p.base(hPtlogin))
	cs := []*http.Cookie{{Name: "qrsig", Value: qrsig}}
	if loginSig != "" {
		cs = append(cs, &http.Cookie{Name: "pt_login_sig", Value: loginSig})
	}
	hc.Jar.SetCookies(u0, cs)
	q := url.Values{"u1": {loginJump}, "ptqrtoken": {strconv.Itoa(hash33(qrsig))}, "ptredirect": {"0"}, "h": {"1"}, "t": {"1"}, "g": {"1"},
		"from_ui": {"1"}, "ptlang": {"2052"}, "action": {fmt.Sprintf("0-0-%d", p.now().UnixMilli())}, "js_ver": {"26071711"}, "js_type": {"1"},
		"login_sig": {loginSig}, "pt_uistyle": {"40"}, "aid": {ptAppID}, "daid": {ptDaid}, "pt_3rd_aid": {oauthClient}}
	_, body, err := p.get(ctx, hc, p.base(hPtlogin)+"/ssl/ptqrlogin?"+q.Encode(), "https://xui.ptlogin2.qq.com/")
	if err != nil {
		return online.QRPoll{}, err
	}
	m := ptuiRe.FindAllStringSubmatch(string(body), -1)
	if len(m) < 5 {
		return online.QRPoll{}, fmt.Errorf("%w: ptqrlogin response", online.ErrUpstream)
	}
	code, redirect, msg := m[0][1], m[2][1], m[4][1]
	nick := ""
	if len(m) > 5 {
		nick = m[5][1]
	}
	switch code {
	case "66":
		return online.QRPoll{Status: online.QRWaiting}, nil
	case "67":
		return online.QRPoll{Status: online.QRScanned, Nickname: nick}, nil
	case "0":
	default: // 65 expired, 68 refused, …
		return online.QRPoll{Status: online.QRExpired, Message: msg}, nil
	}
	if err := p.finishLogin(ctx, hc, redirect); err != nil {
		return online.QRPoll{}, err
	}
	acc, err := p.Account(ctx)
	if err != nil {
		acc = online.Account{UserID: p.uin(), Nickname: nick}
	}
	if acc.Nickname == "" || strings.HasPrefix(acc.Nickname, "QQ用户") && nick != "" {
		acc.Nickname = nick
	}
	return online.QRPoll{Status: online.QRConfirmed, Nickname: acc.Nickname, Account: &acc}, nil
}

// finishLogin: check_sig redirects → p_skey → OAuth code → musicu QQLogin.
func (p *Plugin) finishLogin(ctx context.Context, hc *http.Client, redirect string) error {
	next := p.rewriteURL(redirect)
	for i := 0; i < 8 && next != ""; i++ {
		res, _, err := p.get(ctx, hc, next, "https://xui.ptlogin2.qq.com/")
		if err != nil {
			return err
		}
		loc := res.Header.Get("Location")
		if loc == "" {
			break
		}
		lu, err := res.Request.URL.Parse(loc)
		if err != nil {
			break
		}
		next = p.rewriteURL(lu.String())
	}
	gu, _ := url.Parse(p.base(hGraph))
	pskey := ""
	for _, c := range hc.Jar.Cookies(gu) {
		if c.Name == "p_skey" {
			pskey = c.Value
		}
	}
	if pskey == "" {
		return fmt.Errorf("%w: qq login: no p_skey after check_sig", online.ErrUpstream)
	}
	form := url.Values{"response_type": {"code"}, "client_id": {oauthClient},
		"redirect_uri": {"https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=" + url.QueryEscape("https://y.qq.com/")},
		"scope":        {"get_user_info,get_app_friends"}, "state": {"state"}, "switch": {""}, "from_ptlogin": {"1"}, "src": {"1"},
		"update_auth": {"1"}, "openapi": {"1010_1030"}, "g_tk": {strconv.Itoa(gtk(pskey))},
		"auth_time": {strconv.FormatInt(p.now().UnixMilli(), 10)}, "ui": {newUUID()}}
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.base(hGraph)+"/oauth2.0/authorize", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Referer", loginJump)
	r.Header.Set("Origin", "https://graph.qq.com")
	r.Header.Set("User-Agent", uaWeb)
	res, err := hc.Do(r)
	if err != nil {
		return fmt.Errorf("%w: %v", online.ErrUpstream, err)
	}
	res.Body.Close()
	lu, err := url.Parse(res.Header.Get("Location"))
	code := ""
	if err == nil {
		code = lu.Query().Get("code")
	}
	if code == "" {
		return fmt.Errorf("%w: qq oauth authorize: no code (http %d)", online.ErrUpstream, res.StatusCode)
	}
	var d map[string]any
	if err := p.one(ctx, "QQConnectLogin.LoginServer", "QQLogin", map[string]any{"code": code}, &d); err != nil {
		return err
	}
	return p.applyLogin(d)
}

// applyLogin stores musicid/musickey (+refresh tokens) from a login reply.
func (p *Plugin) applyLogin(d map[string]any) error {
	s := func(k string) string {
		switch v := d[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
		return ""
	}
	id, key := s("musicid"), s("musickey")
	if id == "" || id == "0" || key == "" {
		return fmt.Errorf("%w: qq login reply without musickey", online.ErrUpstream)
	}
	keep := map[string]string{}
	for k, v := range p.c.Cookies() {
		if strings.HasPrefix(k, online.DevicePrefix) {
			keep[k] = v
		}
	}
	keep[kUin], keep[kMusicKey] = id, key
	if v := s("refresh_key"); v != "" {
		keep[kRefreshKey] = v
	}
	if v := s("refresh_token"); v != "" {
		keep[kRefreshTok] = v
	}
	p.c.ReplaceCookies(keep)
	return nil
}

func newUUID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (i % 8 * 8))
	}
	return fmt.Sprintf("%x-%x-4%x-a%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// LoginCookie accepts a y.qq.com cookie with uin and qm_keyst (or qqmusic_key).
func (p *Plugin) LoginCookie(ctx context.Context, cookie string) (online.Account, error) {
	m := online.ParseCookieString(cookie)
	uin := m["uin"]
	if uin == "" {
		uin = m["wxuin"]
	}
	key := m["qm_keyst"]
	if key == "" {
		key = m["qqmusic_key"]
	}
	if uin == "" || key == "" {
		return online.Account{}, fmt.Errorf("%w: cookie must contain uin and qm_keyst (or qqmusic_key)", online.ErrBadInput)
	}
	prev := p.c.Cookies()
	d := map[string]any{"musicid": strings.TrimLeft(strings.TrimPrefix(uin, "o"), "0"), "musickey": key}
	if v := m["psrf_qqrefresh_token"]; v != "" {
		d["refresh_token"] = v
	}
	if err := p.applyLogin(d); err != nil {
		return online.Account{}, err
	}
	acc, err := p.Account(ctx)
	if err != nil {
		p.c.ReplaceCookies(prev)
		return online.Account{}, err
	}
	return acc, nil
}

func (p *Plugin) LoginPassword(ctx context.Context, user, pass string) (online.Account, error) {
	return online.Account{}, fmt.Errorf("%w: QQ 音乐请使用扫码登录", online.ErrNotSupported)
}

// find returns the first string/number found under any of keys (depth-first).
func find(v any, keys ...string) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range keys {
			switch x := t[k].(type) {
			case string:
				if x != "" {
					return x
				}
			case float64:
				if x != 0 {
					return strconv.FormatInt(int64(x), 10)
				}
			}
		}
		for _, x := range t {
			if s := find(x, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range t {
			if s := find(x, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func (p *Plugin) Account(ctx context.Context) (online.Account, error) {
	if !p.loggedIn() {
		return online.Account{}, online.ErrNeedsLogin
	}
	out, codes, err := p.muLoose(ctx, map[string]mreq{
		"user": {"music.UserInfo.userInfoServer", "GetLoginUserInfo", map[string]any{}},
		"vip":  {"userInfo.VipQueryServer", "SRFVipQuery_V2", map[string]any{"uin_list": []string{p.uin()}}},
	})
	if err != nil {
		return online.Account{}, err
	}
	if classify(codes["user"], "user") != nil && classify(codes["vip"], "vip") != nil {
		return online.Account{}, classify(codes["user"], "user")
	}
	var u, v any
	json.Unmarshal(out["user"], &u)
	json.Unmarshal(out["vip"], &v)
	acc := online.Account{UserID: p.uin(), Nickname: find(u, "nick", "nickname", "Nick"), Avatar: https(find(u, "headpic", "headurl", "avatar", "logo"))}
	if acc.Nickname == "" {
		acc.Nickname = "QQ用户 " + p.uin()
	}
	flag := find(v, "vipFlag", "iVipFlag", "isVip", "svip", "lightVipFlag")
	acc.VIP = flag != "" && flag != "0"
	if acc.VIP {
		acc.Tier = "绿钻"
		p.c.SetCookie(kVIP, "1")
	} else {
		p.c.SetCookie(kVIP, "0")
	}
	return acc, nil
}

// Refresh renews the musickey with the stored refresh tokens.
func (p *Plugin) Refresh(ctx context.Context) error {
	if !p.loggedIn() {
		return online.ErrNeedsLogin
	}
	if p.c.Cookie(kRefreshKey) == "" && p.c.Cookie(kRefreshTok) == "" {
		return fmt.Errorf("%w: no refresh token; log in again", online.ErrNotSupported)
	}
	id, _ := strconv.ParseInt(p.uin(), 10, 64)
	var d map[string]any
	if err := p.one(ctx, "music.login.LoginServer", "Login", map[string]any{"refresh_key": p.c.Cookie(kRefreshKey),
		"refresh_token": p.c.Cookie(kRefreshTok), "musickey": p.c.Cookie(kMusicKey), "musicid": id, "loginMode": 2}, &d); err != nil {
		return err
	}
	return p.applyLogin(d)
}
