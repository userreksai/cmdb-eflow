package ecmdbauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	"github.com/Duke1616/eflow/internal/pkg/ticketpbac"
	"github.com/Duke1616/eiam/pkg/ctxutil"
	"github.com/Duke1616/eiam/pkg/pbac"
	pbacgorm "github.com/Duke1616/eiam/pkg/pbac/gormx"
	"github.com/Duke1616/eiam/pkg/web/capability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeUsers struct {
	userv1.UserServiceClient
	users map[int64]*userv1.User
}

func (f fakeUsers) QueryById(_ context.Context, in *userv1.QueryByIdReq, _ ...grpc.CallOption) (*userv1.QueryUserResp, error) {
	return &userv1.QueryUserResp{User: f.users[in.GetId()]}, nil
}

// fakeECMDB 模拟 ECMDB 的 /api/permission 兼容接口：按 Authorization 头识别用户。
func fakeECMDB(t *testing.T, allowed bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var uid int64
		switch r.Header.Get("Authorization") {
		case "Bearer admin":
			uid = 1
		case "Bearer alice":
			uid = 2
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Access-Token", "renewed")
		switch r.URL.Path {
		case checkLoginPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"uid": uid}})
		case checkPolicyPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"Allowed": allowed, "Reason": "denied"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newEngine(m *Middleware, sink *context.Context) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.ContextWithFallback = true
	engine.Use(m.CheckLogin(), m.CheckPolicy())

	reg := capability.NewRegistry("ticket-test", "manager", "测试")
	engine.GET("/api/ticket/history", reg.Define("历史工单", "history_test").
		AccessScope(ticketpbac.HistoryProfile, ticketpbac.HistoryPresets...).
		Bind(func(ctx *gin.Context) {
			*sink = ctx.Request.Context()
			ctx.Status(http.StatusOK)
		}))
	engine.GET("/api/ticket/open", func(ctx *gin.Context) {
		*sink = ctx.Request.Context()
		ctx.Status(http.StatusOK)
	})
	return engine
}

func TestMiddleware(t *testing.T) {
	users := fakeUsers{users: map[int64]*userv1.User{
		1: {Id: 1, Username: "admin", IsAdmin: true},
		2: {Id: 2, Username: "alice"},
	}}

	testCases := []struct {
		name        string
		token       string
		allowed     bool
		path        string
		wantStatus  int
		wantUser    string
		wantScoped  bool
		wantNoScope bool
	}{
		{name: "未登录返回 401", token: "", allowed: true, path: "/api/ticket/open", wantStatus: http.StatusUnauthorized},
		{name: "未声明能力的接口只校验登录", token: "Bearer alice", allowed: false, path: "/api/ticket/open",
			wantStatus: http.StatusOK, wantUser: "alice"},
		{name: "无权限返回 403", token: "Bearer alice", allowed: false, path: "/api/ticket/history",
			wantStatus: http.StatusForbidden},
		{name: "普通用户只看相关工单", token: "Bearer alice", allowed: true, path: "/api/ticket/history",
			wantStatus: http.StatusOK, wantUser: "alice", wantScoped: true},
		{name: "管理员不加数据范围", token: "Bearer admin", allowed: true, path: "/api/ticket/history",
			wantStatus: http.StatusOK, wantUser: "admin", wantNoScope: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeECMDB(t, tc.allowed)
			defer srv.Close()

			var got context.Context
			m := New(Config{AuthURL: srv.URL, TenantID: 7}, users)
			engine := newEngine(m, &got)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", tc.token)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus != http.StatusOK {
				return
			}
			assert.Equal(t, "renewed", rec.Header().Get("X-Access-Token"), "续期凭证应回写给浏览器")
			assert.Equal(t, tc.wantUser, ctxutil.GetUsername(got))
			assert.Equal(t, int64(7), ctxutil.GetTenantID(got).Int64())

			decision, ok := pbac.DecisionFromContext(got)
			if !tc.wantScoped && !tc.wantNoScope {
				assert.False(t, ok, "未声明数据范围的接口不应注入决策")
				return
			}
			require.True(t, ok)
			assert.Equal(t, ticketpbac.HistoryProfile, decision.FilterProfile)
			if tc.wantNoScope {
				assert.Nil(t, decision.AllowAccessScope)
			} else {
				require.NotNil(t, decision.AllowAccessScope)
			}
		})
	}
}

// 本地决策必须能被工单 DAO 使用的两个画像编译，否则列表查询会在运行期报错。
func TestLocalDecisionCompilesForTicketProfiles(t *testing.T) {
	testCases := []struct {
		name    string
		profile pbacgorm.Profile
		admin   bool
		wantSQL bool
	}{
		{name: "历史-普通用户", profile: ticketpbac.History, wantSQL: true},
		{name: "待办-普通用户", profile: ticketpbac.Todo, wantSQL: true},
		{name: "历史-管理员", profile: ticketpbac.History, admin: true},
		{name: "待办-管理员", profile: ticketpbac.Todo, admin: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := pbacgorm.Compile(LocalDecision(tc.profile.Name, "alice", tc.admin), tc.profile)
			require.NoError(t, err)
			if tc.wantSQL {
				assert.NotEmpty(t, expr.SQL)
				assert.Contains(t, expr.Args, "alice")
			} else {
				assert.Empty(t, expr.SQL)
			}
		})
	}
}
