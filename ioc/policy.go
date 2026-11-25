package ioc

import (
	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	"github.com/Duke1616/eflow/internal/pkg/ecmdbauth"
	"github.com/Duke1616/eflow/pkg/mqx"
	"github.com/Duke1616/eiam/pkg/ctxutil"
	"github.com/spf13/viper"
)

// InitAuthMiddleware 初始化对接 ECMDB 的登录与权限中间件（替代上游 EIAM SDK）。
//
// 配置项：
//
//	policy.auth_url: ECMDB 地址（与 ecmdb-task 相同）
//	tenant.default_id: 单租户部署注入的固定租户 ID，默认 1
func InitAuthMiddleware(users userv1.UserServiceClient) *ecmdbauth.Middleware {
	tenantID := DefaultTenantID()
	// MQ 消费端同样使用该租户兜底，保证跨服务消息在单租户部署下不因缺少租户头被丢弃
	mqx.SetDefaultTenantID(tenantID)
	return ecmdbauth.New(ecmdbauth.Config{
		AuthURL:  viper.GetString("policy.auth_url"),
		TenantID: tenantID,
		Timeout:  viper.GetDuration("policy.timeout"),
	}, users)
}

// DefaultTenantID 单租户部署的固定租户 ID（tenant.default_id，默认 1）。
func DefaultTenantID() int64 {
	if tid := viper.GetInt64("tenant.default_id"); tid > 0 {
		return tid
	}
	return ctxutil.SystemTenantID
}
