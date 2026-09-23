package gateway

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

func init() {
	// SQLite 内置 lower 只处理 ASCII；搜索内容和查询词都使用 Go 的 Unicode 转换。
	sqlite.MustRegisterDeterministicScalarFunction("gateway_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] == nil {
			return nil, nil
		}
		value, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("搜索内容必须是文本")
		}
		return strings.ToLower(value), nil
	})
}
