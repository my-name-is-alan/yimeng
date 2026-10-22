// Package kugou is a skeleton of the Kugou Music (酷狗音乐) plugin. It is
// registered so the settings UI can show it, but it declares no
// capabilities yet: enabling it has no effect until search, login and
// stream resolve are implemented (protocol notes: NOTICE → KuGouMusicApi).
package kugou

import "github.com/my-name-is-alan/yimeng/core/internal/online"

type Plugin struct{ env online.Env }

func New() online.Factory {
	return func(env online.Env) (online.Plugin, error) { return &Plugin{env: env}, nil }
}

func (p *Plugin) Manifest() online.Manifest {
	return online.Manifest{
		ID: "kugou", Name: "酷狗音乐", Version: "0.1.0", Homepage: "https://www.kugou.com",
		Description: "开发中：将支持扫码登录、搜索、歌单与按账号权益播放。",
		Skeleton:    true,
		Qualities:   []online.Quality{online.QStandard, online.QHigh, online.QLossless},
		Notice:      "仅播放账号有权播放的内容；不解密 kgm，不绕过任何限制。",
		DefaultRate: 30,
	}
}
