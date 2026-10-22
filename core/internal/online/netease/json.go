package netease

import (
	"encoding/json"
	"fmt"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func jsonUnmarshal(b []byte, v any) error {
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: bad json: %v", online.ErrUpstream, err)
	}
	return nil
}
