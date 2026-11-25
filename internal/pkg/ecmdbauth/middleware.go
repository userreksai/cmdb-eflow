// Package ecmdbauth 让 eflow 以"卫星服务"的身份接入 ECMDB 的登录与权限体系，替代上游的 EIAM SDK。
//
// 为什么不直接使用 eiam/pkg/web/sdk：
//   - EIAM SDK 要求 check_login 返回 tenant_id 与 username，check_policy 返回 PBAC 残余条件与数据画像，
//     ECMDB 只提供"是否登录 / 是否有权"的二元判定（ecmdb-task 走的也是这一套兼容接口）；
//   - 本部署为单租户，PBAC 数据范围只需要"管理员看全部、其他人看与自己相关的工单"两档，
//     在本地按用户身份生成决策即可，无需远端策略引擎。
//
// 业务层仍然通过 ctxutil 读取用户与租户、通过 pbac.DecisionFromContext 读取数据范围，
// 因此 Handler / Service / DAO 不需要任何改动，后续合并上游代码时冲突面最小。
package ecmdbauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	"github.com/Duke1616/eflow/internal/pkg/ticketpbac"
	"github.com/Duke1616/eiam/pkg/ctxutil"
	"github.com/Duke1616/eiam/pkg/pbac"
	"github.com/Duke1616/eiam/pkg/web/capability"
	"github.com/ecodeclub/ginx"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/core/elog"
)

const (
	checkLoginPath  = "/api/permission/check_login"
	checkPolicyPath = "/api/permission/check_policy"
	defaultTimeout  = 5 * time.Second
)

// Config 鉴权中间件配置。
type Config struct {
	// AuthURL ECMDB 服务地址，如 http://127.0.0.1:8000
	AuthURL string
	// TenantID 单租户部署下注入到请求上下文的固定租户 ID（多租户 GORM 插件与 MQ 元数据依赖它）
	TenantID int64
	// Timeout 调用 ECMDB 鉴权接口的超时时间
	Timeout time.Duration
}

// Middleware 提供登录校验与接口鉴权两个 Gin 中间件。
type Middleware struct {
	cfg    Config
	client *http.Client
	users  userv1.UserServiceClient
	logger *elog.Component
}

// New 创建鉴权中间件；users 用于解析用户名与管理员身份，
// 由 ECMDB 提供的 eiam.user.v1 兼容 gRPC 服务实现，与业务 Handler 使用同一个客户端。
func New(cfg Config, users userv1.UserServiceClient) *Middleware {
	cfg.AuthURL = strings.TrimRight(strings.TrimSpace(cfg.AuthURL), "/")
	if cfg.AuthURL == "" {
		panic("policy.auth_url 未配置：eflow 需要通过 ECMDB 完成登录与权限校验")
	}
	if !strings.HasPrefix(cfg.AuthURL, "http://") && !strings.HasPrefix(cfg.AuthURL, "https://") {
		cfg.AuthURL = "http://" + cfg.AuthURL
	}
	if cfg.TenantID <= 0 {
		cfg.TenantID = ctxutil.SystemTenantID
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Middleware{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		users:  users,
		logger: elog.DefaultLogger.With(elog.FieldComponentName("ecmdbauth")),
	}
}

type apiResult[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

type checkLoginResp struct {
	Uid json.Number `json:"uid"`
}

type checkPolicyReq struct {
	Service  string `json:"service"`
	Path     string `json:"path"`
	Method   string `json:"method"`
	Resource string `json:"resource"`
}

type checkPolicyResp struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// 请求上下文中缓存当前用户，避免 CheckPolicy 重复查询。
type principalKey struct{}

type principal struct {
	uid      int64
	username string
	isAdmin  bool
}

// CheckLogin 登录校验：向 ECMDB 校验会话，解析用户后注入用户 ID、用户名与固定租户。
func (m *Middleware) CheckLogin() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var res apiResult[checkLoginResp]
		if err := m.callAPI(ctx, checkLoginPath, nil, &res); err != nil {
			m.logger.Warn("登录校验失败", elog.FieldErr(err))
			return
		}
		uid, err := res.Data.Uid.Int64()
		if err != nil || uid <= 0 {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, ginx.Result{Code: 401001, Msg: "登录凭据无效"})
			return
		}

		user, err := m.resolveUser(ctx.Request.Context(), uid)
		if err != nil {
			m.logger.Error("解析登录用户失败", elog.Int64("uid", uid), elog.FieldErr(err))
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, ginx.Result{Code: 401003, Msg: "无法识别当前登录用户"})
			return
		}

		reqCtx := ctxutil.WithUserInfo(ctx.Request.Context(), uid, m.cfg.TenantID, user.GetUsername())
		reqCtx = ctxutil.WithOriginTenantID(reqCtx, m.cfg.TenantID)
		reqCtx = context.WithValue(reqCtx, principalKey{}, principal{
			uid: uid, username: user.GetUsername(), isAdmin: user.GetIsAdmin(),
		})
		ctx.Request = ctx.Request.WithContext(reqCtx)
		ctx.Next()
	}
}

// CheckPolicy 接口鉴权：对通过 Define 声明的接口向 ECMDB 校验 (service, path, method)，
// 并为声明了 AccessScope 的接口在本地生成数据范围决策。
func (m *Middleware) CheckPolicy() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		info, ok := capability.GetResourceInfo(reflect.ValueOf(ctx.Handler()).Pointer())
		if !ok {
			// 未声明能力的接口只要求登录
			ctx.Next()
			return
		}

		var res apiResult[checkPolicyResp]
		if err := m.callAPI(ctx, checkPolicyPath, checkPolicyReq{
			Service: info.Service,
			Path:    ctx.FullPath(),
			Method:  ctx.Request.Method,
		}, &res); err != nil {
			m.logger.Warn("接口鉴权调用失败", elog.String("path", ctx.FullPath()), elog.FieldErr(err))
			return
		}
		if !res.Data.Allowed {
			reason := res.Data.Reason
			if reason == "" {
				reason = "没有权限执行该操作"
			}
			ctx.AbortWithStatusJSON(http.StatusForbidden, ginx.Result{Code: 403001, Msg: reason})
			return
		}

		if info.FilterProfile != "" {
			p, _ := ctx.Request.Context().Value(principalKey{}).(principal)
			ctx.Request = ctx.Request.WithContext(
				pbac.WithDecision(ctx.Request.Context(), LocalDecision(info.FilterProfile, p.username, p.isAdmin)))
		}
		ctx.Next()
	}
}

// LocalDecision 生成单租户部署下的数据范围决策：
// 管理员不加过滤；其他用户只能看到自己创建、当前处理或历史参与的工单。
// "相关人"谓词在历史与待办两个画像中都受支持，见 ticketpbac.compileHistory / compileTodo。
func LocalDecision(profile pbac.FilterProfile, username string, isAdmin bool) pbac.Decision {
	decision := pbac.Decision{Allowed: true, FilterProfile: profile}
	if isAdmin {
		return decision
	}
	decision.AllowAccessScope = &pbac.AccessScope{Predicate: &pbac.Predicate{
		Key:      ticketpbac.TicketRelatedUsers,
		Operator: pbac.ForAnyValueStringEquals,
		Values:   []pbac.Operand{pbac.Literal(username)},
	}}
	return decision
}

func (m *Middleware) resolveUser(ctx context.Context, uid int64) (*userv1.User, error) {
	resp, err := m.users.QueryById(ctx, &userv1.QueryByIdReq{Id: uid})
	if err != nil {
		return nil, err
	}
	if resp.GetUser() == nil || resp.GetUser().GetUsername() == "" {
		return nil, fmt.Errorf("用户 %d 不存在", uid)
	}
	return resp.GetUser(), nil
}

// callAPI 透传原请求的认证头调用 ECMDB，并把续期产生的新凭证回写给浏览器。
// 失败时已经写入了响应并 Abort，调用方只需返回。
func (m *Middleware) callAPI(ctx *gin.Context, path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			ctx.AbortWithStatus(http.StatusInternalServerError)
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodPost, m.cfg.AuthURL+path, &buf)
	if err != nil {
		ctx.AbortWithStatus(http.StatusInternalServerError)
		return err
	}
	req.Header = ctx.Request.Header.Clone()
	req.Header.Del("Content-Length")
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		ctx.AbortWithStatusJSON(http.StatusBadGateway, ginx.Result{Code: 502001, Msg: "鉴权中心不可用"})
		return err
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		if k == "Content-Length" || k == "Content-Type" {
			continue
		}
		for _, v := range vals {
			ctx.Writer.Header().Add(k, v)
		}
	}
	if resp.StatusCode != http.StatusOK {
		ctx.AbortWithStatus(resp.StatusCode)
		return fmt.Errorf("鉴权中心返回状态码 %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(out); err != nil {
		ctx.AbortWithStatus(http.StatusInternalServerError)
		return fmt.Errorf("解析鉴权中心响应失败: %w", err)
	}
	return nil
}
