// Package local 在不部署 ealert 的环境中提供 ealert 客户端的本地实现。
//
// 为什么用"本地实现客户端接口"而不是删除调用方：
// 通知策略、审批人解析、模板自愈等上游代码都面向 ealert 的 gRPC 客户端接口编程，
// 在接口层替换实现可以让业务代码保持与上游一致，后续合并上游更新时冲突最小。
//
// 能力映射：
//   - ResolveRecipients（审批人动态解析）：本地完成，用户/部门走 eiam 兼容服务（由 ECMDB 提供），值班走 ECMDB rota
//   - SendNotification：不支持；消息发送改由通知脚本完成（见 internal/pkg/notification/provider/script）
//   - Team / Template / OnCall 其它接口：返回 Unimplemented，调用方均为非阻塞的辅助能力（群聊节点、模板自愈）
package local

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// unavailableConn 让任意 gRPC 客户端的所有调用都返回 Unimplemented，
// 用于生成"未接入 ealert"语义的客户端，而无需逐个手写接口方法。
type unavailableConn struct{}

// NewUnavailableConn 返回一个所有调用都失败的 gRPC 连接。
func NewUnavailableConn() grpc.ClientConnInterface { return unavailableConn{} }

func (unavailableConn) Invoke(_ context.Context, method string, _, _ any, _ ...grpc.CallOption) error {
	return status.Errorf(codes.Unimplemented, "本部署未接入 ealert，%s 不可用", method)
}

func (unavailableConn) NewStream(_ context.Context, _ *grpc.StreamDesc, method string,
	_ ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Errorf(codes.Unimplemented, "本部署未接入 ealert，%s 不可用", method)
}
