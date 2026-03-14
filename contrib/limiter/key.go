//
// key.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package limiter

import (
	"fmt"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/contrib/requestmeta"
)

// getPathKeyFunc 基于路径的key生成函数
func GetPathKeyFunc(x *vigo.X) string {
	return fmt.Sprintf("%s:%s", requestmeta.RemoteIP(x), x.Request.URL.Path)
}
