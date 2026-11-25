package ioc

import (
	"github.com/Duke1616/eflow/internal/domain"
	"github.com/Duke1616/eflow/internal/repository"
	engineSvc "github.com/Duke1616/eflow/internal/service/engine"
	easyflowDrv "github.com/Duke1616/eflow/internal/service/engine/easyflow"
	flowxDrv "github.com/Duke1616/eflow/internal/service/engine/flowx"
	workflowSvc "github.com/Duke1616/eflow/internal/service/workflow"
	"github.com/Duke1616/eflow/pkg/flowx"
	"github.com/Duke1616/eflow/pkg/flowx/middleware"
	flowxMysql "github.com/Duke1616/eflow/pkg/flowx/storage/mysql"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// InitFlowxEngine 初始化现代工作流引擎 flowx 实例并挂载业务生命周期监听器
func InitFlowxEngine(db *gorm.DB, ticketRepo repository.TicketRepository) flowx.Engine {
	store := flowxMysql.NewStorage(db)
	eng := flowx.NewEngine(store)
	// Logger 置外层：Recovery 把 panic 转成 error 后，仍会留下一条带耗时的流转审计记录
	eng.Use(middleware.Logger(), middleware.Recovery())
	eng.AddListener(flowxDrv.NewEventListener(ticketRepo))
	return eng
}

// InitEngine 基于微内核驱动架构组装底层引擎，对外暴露统一 IEngine
func InitEngine(repo repository.IEngineRepository, flowxEngine flowx.Engine, ticketRepo repository.TicketRepository) engineSvc.IEngine {
	InitEnginePolicy()
	easyflowEngine := easyflowDrv.NewEngine(repo)
	flowxDriver := flowxDrv.NewEngine(flowxEngine, ticketRepo)
	return engineSvc.NewEngineDispatcher(easyflowEngine, flowxDriver)
}

// InitEnginePolicy 从配置加载流程引擎策略（workflow.default_engine 默认 flowx，workflow.allow_easyflow 默认 false）。
// 本部署没有存量 easy-workflow 工单，新建与发布的流程统一使用 flowx。
func InitEnginePolicy() {
	workflowSvc.SetEnginePolicy(
		domain.EngineType(viper.GetString("workflow.default_engine")),
		viper.GetBool("workflow.allow_easyflow"),
	)
}
