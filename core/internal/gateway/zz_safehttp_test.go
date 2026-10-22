package gateway

import "github.com/my-name-is-alan/yimeng/core/internal/infra/safehttp"

// Gateway tests serve fake CDNs from httptest on 127.0.0.1.
func init() { safehttp.AllowPrivate(true) }
