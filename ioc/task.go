package ioc

import (
	"time"

	"github.com/spf13/viper"

	processConsumer "github.com/Duke1616/eflow/internal/event/process"
	taskConsumer "github.com/Duke1616/eflow/internal/event/task"
	templateConsumer "github.com/Duke1616/eflow/internal/event/template"
	feishuCallback "github.com/Duke1616/eflow/internal/callback/feishu"
	wechatConsumerPkg "github.com/Duke1616/eflow/internal/event/wechat"
	"github.com/Duke1616/eflow/internal/repository"
	"github.com/Duke1616/eflow/internal/service/engine"
	serviceTask "github.com/Duke1616/eflow/internal/service/task"
	withdrawalSvc "github.com/Duke1616/eflow/internal/service/withdrawal"
)

// InitTasks 初始化所有后台任务
// NOTE: 新增后台任务时在此处注入，打通定时任务、后台作业补偿及全量大事件 Kafka 消费监听
func InitTasks(
	taskSvc serviceTask.Service,
	withdrawalRepo repository.WithdrawalRepository,
	compensationPlanner withdrawalSvc.CompensationPlanner,
	engine engine.IEngine,
	executeResultConsumer *taskConsumer.ExecuteResultConsumer,
	processConsumer *processConsumer.ProcessEventConsumer,
	wechatConsumer *wechatConsumerPkg.WechatTicketConsumer,
	larkWsServer *feishuCallback.LarkCallbackServer,
	wechatCallbackConsumer *templateConsumer.WechatApprovalCallbackConsumer,
) []Task {
	tasks := []Task{
		executeResultConsumer,
		serviceTask.NewStartTaskJob(taskSvc, 100, 8, 10*time.Second, 30*time.Second),
		serviceTask.NewTaskRecoveryJob(taskSvc, 100, time.Minute, time.Minute, 30*time.Second),
		serviceTask.NewPassProcessTaskJob(taskSvc, engine, 100, 10*time.Second),
		withdrawalSvc.NewRecoveryJob(withdrawalRepo, compensationPlanner, 100, time.Minute, 5*time.Minute),
		processConsumer,
		wechatConsumer,
		wechatCallbackConsumer,
	}
	// 飞书卡片回调长连接仅在配置了飞书应用时启动（通知脚本模式下卡片由脚本发送，通常无需回调）。
	// 未接入 ealert，上游的通知模板自愈任务（TemplateBootstrapTask）不再启动。
	if viper.GetString("lark.app_id") != "" {
		tasks = append(tasks, larkWsServer)
	}
	return tasks
}
