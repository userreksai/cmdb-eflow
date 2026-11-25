package workflow

import (
	"fmt"
	"sync/atomic"

	"github.com/Duke1616/eflow/internal/domain"
)

// enginePolicy 约束新建/修改/发布流程时可用的底层引擎。
//
// 为什么需要：上游保留 easy-workflow 是为了兼容存量工单；本部署没有存量数据，统一使用 flowx，
// 避免同时维护两套引擎语义（驳回、转签、自动化节点在两套引擎下行为不同）。
// 以包级策略而不是改构造函数注入，是为了不改动上游 NewWorkflowService 的签名，降低合并冲突。
type enginePolicy struct {
	defaultEngine domain.EngineType
	allowEasyflow bool
}

var policy atomic.Value

func init() {
	policy.Store(enginePolicy{defaultEngine: domain.EngineTypeFlowx})
}

// SetEnginePolicy 设置流程引擎策略：defaultEngine 为未指定时使用的引擎；allowEasyflow 为是否允许选择 easy-workflow。
func SetEnginePolicy(defaultEngine domain.EngineType, allowEasyflow bool) {
	if !defaultEngine.IsValid() {
		defaultEngine = domain.EngineTypeFlowx
	}
	if defaultEngine.IsEasyFlow() {
		allowEasyflow = true
	}
	policy.Store(enginePolicy{defaultEngine: defaultEngine, allowEasyflow: allowEasyflow})
}

// ApplyEnginePolicy 规范化流程的引擎类型：未指定时使用默认引擎，不允许的引擎返回错误。
func ApplyEnginePolicy(wf *domain.Workflow) error {
	p := policy.Load().(enginePolicy)
	if wf.EngineType == "" {
		wf.EngineType = p.defaultEngine
	}
	if err := wf.ValidateEngineType(); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrInvalidParameter, err)
	}
	if wf.EngineType.IsEasyFlow() && !p.allowEasyflow {
		return fmt.Errorf("%w: 当前部署仅支持 flowx 流程引擎", domain.ErrInvalidParameter)
	}
	return nil
}
