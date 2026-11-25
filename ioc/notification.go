package ioc

import (
	etaskclient "github.com/Duke1616/eflow/internal/client/etask"
	"github.com/Duke1616/eflow/internal/pkg/notification"
	"github.com/Duke1616/eflow/internal/pkg/notification/channel"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider/feishu"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider/script"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider/sequential"
	"github.com/Duke1616/eflow/internal/pkg/notification/provider/wechat"
	"github.com/Duke1616/eflow/internal/pkg/notification/sender"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/spf13/viper"
	"github.com/xen0n/go-workwx"
)

// InitNotificationSender 初始化通知发送器，提供给底层 Service 和 Event 逻辑使用
//
// 本部署不接入 ealert，发送方式由配置决定：
//   - notify.runner_id > 0（推荐）：所有渠道统一提交给 ecmdb-task 的通知脚本，由脚本投递飞书/企微/邮件等；
//   - 未配置：退回上游的直连发送（需要配置 lark.* / wechat.* 应用凭证）。
func InitNotificationSender(
	larkClient *lark.Client,
	workApp *workwx.WorkwxApp,
	dispatcher etaskclient.Client,
) (sender.NotificationSender, error) {
	if runnerID := viper.GetInt64("notify.runner_id"); runnerID > 0 {
		scriptProvider := script.NewProvider(script.Config{
			RunnerID:            runnerID,
			MaxExecutionSeconds: viper.GetInt64("notify.max_execution_seconds"),
			Timeout:             viper.GetDuration("notify.timeout"),
		}, dispatcher)
		selector := sequential.NewSelectorBuilder([]provider.Provider{scriptProvider})
		return sender.NewSender(channel.NewDispatcher(map[notification.Channel]channel.Channel{
			notification.ChannelLarkCard: channel.NewLarkCardChannel(selector),
			notification.ChannelLarkText: channel.NewLarkTextChannel(selector),
			notification.ChannelWechat:   channel.NewWechatChannel(selector),
		})), nil
	}

	larkCardProvider, err := feishu.NewLarkCardProvider(larkClient)
	if err != nil {
		return nil, err
	}

	larkTextProvider, err := feishu.NewLarkTextProvider(larkClient)
	if err != nil {
		return nil, err
	}

	// LarkCard / LarkText 渠道：飞书应用直连发送
	larkCardChannel := channel.NewLarkCardChannel(sequential.NewSelectorBuilder([]provider.Provider{larkCardProvider}))
	larkTextChannel := channel.NewLarkTextChannel(sequential.NewSelectorBuilder([]provider.Provider{larkTextProvider}))

	// Wechat 渠道：企微应用 markdown 消息，个人/群聊由 SDK 按收件人类型自动路由
	wechatChannel := channel.NewWechatChannel(sequential.NewSelectorBuilder([]provider.Provider{wechat.NewProvider(workApp)}))

	dispatcherCh := channel.NewDispatcher(map[notification.Channel]channel.Channel{
		notification.ChannelLarkCard: larkCardChannel,
		notification.ChannelLarkText: larkTextChannel,
		notification.ChannelWechat:   wechatChannel,
	})
	return sender.NewSender(dispatcherCh), nil
}
