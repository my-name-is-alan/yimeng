package online

import (
	"encoding/json"
)

// App JSON contract (D45): "playable" is a bool, "entitlement" carries the
// full|preview|unavailable detail, "album" is {id,name}. Older cached JSON
// with "playable":"preview" still decodes.

type trackAlias Track

type trackJSON struct {
	trackAlias
	Playable         json.RawMessage `json:"playable"`
	Entitlement      Entitlement     `json:"entitlement,omitempty"`
	UnplayableReason string          `json:"unplayable_reason,omitempty"`
}

func (t Track) MarshalJSON() ([]byte, error) {
	j := trackJSON{trackAlias: trackAlias(t), Entitlement: t.Playable}
	ok := t.Playable != EntUnavailable
	j.Playable, _ = json.Marshal(ok)
	if !ok {
		j.UnplayableReason = unplayableReason(t.Origin)
	}
	return json.Marshal(j)
}

func (t *Track) UnmarshalJSON(b []byte) error {
	var j trackJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*t = Track(j.trackAlias)
	t.Playable = j.Entitlement
	if len(j.Playable) > 0 && j.Playable[0] == '"' { // legacy cache entry
		var e Entitlement
		json.Unmarshal(j.Playable, &e)
		t.Playable = e
	} else if t.Playable == "" && string(j.Playable) == "false" {
		t.Playable = EntUnavailable
	}
	return nil
}

var platformName = map[string]string{"netease": "网易云", "qq": "QQ音乐", "kugou": "酷狗"}

func unplayableReason(origin string) string {
	n := platformName[origin]
	if n == "" {
		n = origin
	}
	return n + "：当前账号无法播放该歌曲（版权或会员限制）"
}
