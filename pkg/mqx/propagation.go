package mqx

import (
	"context"
	"strconv"

	"github.com/Duke1616/eiam/pkg/ctxutil"
	"github.com/Duke1616/etask/pkg/grpc/interceptors/bizid"
	"github.com/ecodeclub/mq-api"
)

const (
	HeaderTenantID = "x-tenant-id"
	HeaderUserID   = "x-user-id"
	HeaderBizID    = "x-biz-id"
)

// InjectContext 提取 Context 中的租户 ID、用户 ID 和业务 ID，注入到 mq.Message.Header 中
func InjectContext(ctx context.Context, msg *mq.Message) {
	if msg == nil {
		return
	}
	if msg.Header == nil {
		msg.Header = make(mq.Header)
	}

	// 1. 提取租户 ID 并注入
	if tid := ctxutil.GetTenantID(ctx); tid > 0 {
		msg.Header[HeaderTenantID] = tid.String()
	}

	// 2. 提取用户 ID 并注入
	if uid := ctxutil.GetUserID(ctx); uid > 0 {
		msg.Header[HeaderUserID] = uid.String()
	}

	// 3. 提取业务 ID (biz_id) 并注入
	if bid, err := bizid.FromContext(ctx); err == nil && bid > 0 {
		msg.Header[HeaderBizID] = strconv.FormatInt(bid, 10)
	}
}

// defaultTenantID 单租户部署下的兜底租户：上游服务（如 ecmdb-task）未携带租户头时使用，
// 避免消费者因缺少租户元数据而丢弃消息。0 表示不兜底（上游多租户行为）。
var defaultTenantID int64

// SetDefaultTenantID 设置消息缺少租户头时使用的兜底租户 ID。
func SetDefaultTenantID(tid int64) {
	defaultTenantID = tid
}

// ExtractContext 从 mq.Message.Header 中提取租户 ID、用户 ID 和业务 ID，重新注入并返回新的 Context
func ExtractContext(ctx context.Context, msg *mq.Message) context.Context {
	if msg == nil {
		return ctx
	}
	if msg.Header == nil {
		msg.Header = make(mq.Header)
	}

	// 1. 提取并注入租户 ID（缺失时使用单租户兜底）
	tid := getHeaderID(msg.Header, HeaderTenantID)
	if tid <= 0 {
		tid = defaultTenantID
	}
	if tid > 0 {
		ctx = ctxutil.WithTenantID(ctx, tid)
		ctx = ctxutil.WithOriginTenantID(ctx, tid)
	}

	// 2. 提取并注入用户 ID
	if uid := getHeaderID(msg.Header, HeaderUserID); uid > 0 {
		ctx = ctxutil.WithUserID(ctx, uid)
	}

	// 3. 提取并注入业务 ID (biz_id)
	if bid := getHeaderID(msg.Header, HeaderBizID); bid > 0 {
		ctx = bizid.Set(ctx, bid)
	}

	return ctx
}

// ProduceMessage 注入上下文元数据后发送原始 MQ 消息。
func ProduceMessage(ctx context.Context, producer mq.Producer, msg *mq.Message) (*mq.ProducerResult, error) {
	InjectContext(ctx, msg)
	return producer.Produce(ctx, msg)
}

// ConsumeMessage 消费原始 MQ 消息，并返回从消息头恢复后的业务上下文。
func ConsumeMessage(ctx context.Context, consumer mq.Consumer) (context.Context, *mq.Message, error) {
	msg, err := consumer.Consume(ctx)
	if err != nil {
		return ctx, nil, err
	}
	return ExtractContext(ctx, msg), msg, nil
}

// getHeaderID 从 Header 中安全提取并解析 int64 类型的正整数 ID
func getHeaderID(header mq.Header, key string) int64 {
	valStr, ok := header[key]
	if !ok {
		return 0
	}
	id, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}
