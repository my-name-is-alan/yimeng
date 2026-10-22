package netease

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func jsonUnmarshal(b []byte, v any) error {
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: bad json: %v", online.ErrUpstream, err)
	}
	return nil
}

// flexInt accepts JSON numbers in integer or float form (NetEase sometimes
// sends 314797.0) and numeric strings; anything else decodes to 0.
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
	if x, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt(int64(x))
		return nil
	}
	*f = 0
	return nil
}
