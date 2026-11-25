// Package script 以"挂脚本"的方式发送流程通知：eflow 不直接调用飞书/企微，
// 而是把渲染好的通知内容作为参数提交给 ecmdb-task 中配置的通知执行单元（Runner），由脚本完成投递。
//
// 为什么这样设计：
//   - 本部署不接入 ealert，通知渠道、模板与凭证统一收敛到脚本中维护，改通知方式无需改 eflow；
//   - 复用自动化节点的执行链路（etask RunRunner），执行记录与日志可在 ecmdb-task 中追踪；
//   - 通知是"尽力而为"的辅助能力：提交失败只返回错误给上层记录日志，不影响流程推进。
package script

import (
	"context"
	"errors"
	"fmt"
	"time"

	etaskclient "github.com/Duke1616/eflow/internal/client/etask"
	"github.com/Duke1616/eflow/internal/domain"
	"github.com/Duke1616/eflow/internal/pkg/notification"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider"
	"github.com/google/uuid"
)

// RequestIDPrefix 通知执行的幂等请求标识前缀。
// ecmdb-task 据此把这类执行的完成事件标记为非工作流来源，eflow 的自动化任务结果消费者不会处理它们。
const RequestIDPrefix = "notify:"

// PayloadKind 通知脚本参数中的类型标识，便于同一个脚本区分不同来源的调用。
const PayloadKind = "eflow.notification"

// ErrPatchUnsupported 脚本模式不支持原地更新已发送的消息（如飞书卡片置灰）。
var ErrPatchUnsupported = errors.New("通知脚本模式不支持更新已发送的消息")

// Config 通知脚本配置。
type Config struct {
	// RunnerID ecmdb-task 中通知脚本对应的执行单元 ID
	RunnerID int64
	// MaxExecutionSeconds 单次通知脚本最长执行时间，<=0 使用 ecmdb-task 默认值
	MaxExecutionSeconds int64
	// Timeout 提交执行的超时时间
	Timeout time.Duration
}

type scriptProvider struct {
	cfg        Config
	dispatcher etaskclient.TaskDispatcher
}

// NewProvider 创建通知脚本 Provider。
func NewProvider(cfg Config, dispatcher etaskclient.TaskDispatcher) provider.Provider {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &scriptProvider{cfg: cfg, dispatcher: dispatcher}
}

// Send 把通知提交为一次脚本执行，只等待提交成功，不等待脚本执行结果。
func (p *scriptProvider) Send(ctx context.Context, n notification.Notification) (notification.Response, error) {
	if n.IsPatch() {
		return notification.Response{}, ErrPatchUnsupported
	}
	if p.cfg.RunnerID <= 0 {
		return notification.Response{}, fmt.Errorf("未配置通知脚本执行单元 notify.runner_id")
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.cfg.Timeout)
	defer cancel()

	executionID, err := p.dispatcher.Dispatch(ctx, domain.TaskAttempt{
		RequestID:           RequestIDPrefix + uuid.NewString(),
		RunnerID:            p.cfg.RunnerID,
		MaxExecutionSeconds: p.cfg.MaxExecutionSeconds,
		Input:               BuildPayload(n),
	})
	if err != nil {
		return notification.NewErrorResponse(string(notification.ErrorCodeServiceUnavailable), err.Error()),
			fmt.Errorf("%w: 提交通知脚本失败: %v", notification.ErrNotificationUnavailable, err)
	}
	return notification.NewSuccessResponse(executionID, "SUBMITTED"), nil
}

// BuildPayload 构造通知脚本接收的参数（脚本从 args 中读取，JSON 结构见 docs/notify-script.md）。
func BuildPayload(n notification.Notification) domain.TaskArgs {
	return domain.TaskArgs{
		"kind":          PayloadKind,
		"channel":       n.Channel.String(),
		"receiver":      n.Receiver,
		"receiver_type": n.ReceiverType,
		"workflow_id":   n.WorkFlowID,
		"notify_type":   string(n.Template.Name),
		"template":      n.Template,
	}
}
