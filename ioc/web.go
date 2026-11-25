package ioc

import (
	"net"

	"github.com/Duke1616/eflow/internal/pkg/ecmdbauth"
	"github.com/Duke1616/eflow/internal/web/dispatch"
	"github.com/Duke1616/eflow/internal/web/task"
	"github.com/Duke1616/eflow/internal/web/template"
	"github.com/Duke1616/eflow/internal/web/ticket"
	"github.com/Duke1616/eflow/internal/web/workflow"
	"github.com/Duke1616/eiam/pkg/web/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gotomicro/ego/server/egin"
)

// InitGinWebServer 初始化并装载 Gin Web Server，显式挂载工单模板和工作流的 API 私有路由
//
// 登录与权限统一委托给 ECMDB（见 internal/pkg/ecmdbauth）。上游的 EIAM 能力资产同步
// (capability syncer) 在本部署中不启用：接口与菜单权限在 ECMDB 的菜单/权限模块中维护。
func InitGinWebServer(mdls []gin.HandlerFunc, auth *ecmdbauth.Middleware,
	templateHdl *template.Handler, workflowHdl *workflow.Handler,
	taskHdl *task.Handler,
	ticketHdl *ticket.Handler, dispatchHdl *dispatch.Handler,
	listener net.Listener) *egin.Component {

	server := egin.Load("server.egin").Build(egin.WithListener(listener))
	// 开启 ContextWithFallback：使 ctx.Context.Value() 自动 fallback 到 ctx.Request.Context().Value()
	server.Engine.ContextWithFallback = true
	server.Use(mdls...)

	// 登录检查
	server.Use(auth.CheckLogin())

	// 权限策略检查
	server.Use(auth.CheckPolicy())

	templateHdl.PrivateRoutes(server.Engine)
	workflowHdl.PrivateRoutes(server.Engine)
	taskHdl.PrivateRoutes(server.Engine)
	ticketHdl.PrivateRoutes(server.Engine)
	dispatchHdl.PrivateRoutes(server.Engine)

	return server
}

func InitGinMiddlewares() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		middleware.AccessLogger(),
		middleware.NewCorsBuilder().Build(),
	}
}
