package script

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Duke1616/eflow/internal/domain"
	"github.com/Duke1616/eflow/internal/pkg/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	got domain.TaskAttempt
	err error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, attempt domain.TaskAttempt) (int64, error) {
	f.got = attempt
	return 42, f.err
}

func (f *fakeDispatcher) TerminateExecution(context.Context, int64, string, string) error { return nil }

func TestScriptProviderSend(t *testing.T) {
	msg := notification.Notification{
		Receiver: "ou_123", ReceiverType: notification.ReceiverTypeUser,
		Channel: notification.ChannelLarkCard, WorkFlowID: 9,
		Template: notification.Template{Name: domain.NotifyTypeApproval, Title: "待审批"},
	}

	testCases := []struct {
		name       string
		runnerID   int64
		n          notification.Notification
		dispatchEr error
		wantErr    error
		wantSubmit bool
	}{
		{name: "提交通知脚本", runnerID: 3, n: msg, wantSubmit: true},
		{name: "未配置执行单元", runnerID: 0, n: msg},
		{name: "更新已发送消息不支持", runnerID: 3,
			n: func() notification.Notification { m := msg; m.MessageID = "om_1"; return m }(), wantErr: ErrPatchUnsupported},
		{name: "提交失败返回不可用错误", runnerID: 3, n: msg, dispatchEr: errors.New("boom"),
			wantErr: notification.ErrNotificationUnavailable, wantSubmit: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDispatcher{err: tc.dispatchEr}
			resp, err := NewProvider(Config{RunnerID: tc.runnerID, MaxExecutionSeconds: 60}, d).Send(context.Background(), tc.n)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}
			if !tc.wantSubmit {
				require.Error(t, err)
				assert.Empty(t, d.got.RequestID, "不应提交执行")
				return
			}
			assert.True(t, strings.HasPrefix(d.got.RequestID, RequestIDPrefix))
			assert.Equal(t, tc.runnerID, d.got.RunnerID)
			assert.Equal(t, int64(60), d.got.MaxExecutionSeconds)
			assert.Equal(t, PayloadKind, d.got.Input["kind"])
			assert.Equal(t, "ou_123", d.got.Input["receiver"])
			assert.Equal(t, string(domain.NotifyTypeApproval), d.got.Input["notify_type"])
			if tc.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, int64(42), resp.NotificationId)
			}
		})
	}
}
