package workflow

import (
	"testing"

	"github.com/Duke1616/eflow/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyEnginePolicy(t *testing.T) {
	t.Cleanup(func() { SetEnginePolicy(domain.EngineTypeFlowx, false) })

	testCases := []struct {
		name          string
		defaultEngine domain.EngineType
		allowEasyflow bool
		input         domain.EngineType
		want          domain.EngineType
		wantErr       bool
	}{
		{name: "默认策略：未指定时使用 flowx", input: "", want: domain.EngineTypeFlowx},
		{name: "默认策略：显式 flowx", input: domain.EngineTypeFlowx, want: domain.EngineTypeFlowx},
		{name: "默认策略：拒绝 easy-workflow", input: domain.EngineTypeEasyFlow, wantErr: true},
		{name: "非法引擎类型", input: "camunda", wantErr: true},
		{name: "开启兼容后允许 easy-workflow", allowEasyflow: true,
			input: domain.EngineTypeEasyFlow, want: domain.EngineTypeEasyFlow},
		{name: "默认引擎为 easy-workflow 时自动允许", defaultEngine: domain.EngineTypeEasyFlow,
			input: "", want: domain.EngineTypeEasyFlow},
		{name: "非法默认引擎回落为 flowx", defaultEngine: "bad", input: "", want: domain.EngineTypeFlowx},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetEnginePolicy(tc.defaultEngine, tc.allowEasyflow)
			wf := domain.Workflow{EngineType: tc.input}
			err := ApplyEnginePolicy(&wf)
			if tc.wantErr {
				require.ErrorIs(t, err, domain.ErrInvalidParameter)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, wf.EngineType)
		})
	}
}
