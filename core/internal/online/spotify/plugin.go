// Package spotify registers Spotify as a metadata-only entry in the plugin
// list so its client credentials can be managed from the web UI and stored in
// the Vault. It declares no catalog, login or playback capabilities: the
// actual lookups live in internal/adapters/spotify and feed the metadata
// editor's match UI (see cmd/core).
package spotify

import (
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

const (
	ID           = "spotify"
	KeyClientID  = "client_id"
	KeySecret    = "client_secret"
	KindMetadata = "metadata"
)

type plugin struct{}

func (plugin) Manifest() online.Manifest {
	return online.Manifest{
		ID: ID, Name: "Spotify", Version: "1.0",
		Description: "元数据提供者：匹配曲目/专辑/艺人，补全 ISRC、封面、流派、发行日期、热度与艺人图片",
		Homepage:    "https://developer.spotify.com/documentation/web-api",
		// No capabilities: never a catalog, login or stream source.
		Capabilities: []online.Capability{},
		LoginMethods: []online.LoginMethod{},
		DefaultRate:  60,
		Config: []online.ConfigField{
			{Key: KeyClientID, Label: "Client ID", Type: "secret", Help: "在 Spotify Developer Dashboard 创建应用获得；只保存在 Vault，不会回显。"},
			{Key: KeySecret, Label: "Client Secret", Type: "secret", Help: "只保存在 Vault，不会回显；留空表示不修改。"},
		},
		Notice: "仅使用 Client Credentials 读取公开元数据；不登录用户账号、不导入歌单、不提供播放。匹配结果只作为建议，应用后写入本地覆盖（不修改音频文件）。",
		Extra:  map[string]any{"kind": KindMetadata},
	}
}

// New is the plugin factory.
func New() online.Factory {
	return func(online.Env) (online.Plugin, error) { return plugin{}, nil }
}
