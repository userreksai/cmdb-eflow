package ioc

import (
	oncallv1 "github.com/Duke1616/eflow/api/proto/gen/ealert/oncall/v1"
	teamv1 "github.com/Duke1616/eflow/api/proto/gen/ealert/team"
	templatev1 "github.com/Duke1616/eflow/api/proto/gen/ealert/template/v1"
	departmentv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/department/v1"
	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	"github.com/Duke1616/eflow/internal/client/ealert"
	"github.com/Duke1616/eflow/internal/client/ealert/local"
	"github.com/Duke1616/eflow/internal/client/ecmdb"
	"github.com/Duke1616/eflow/internal/client/eiam"
	"github.com/Duke1616/eflow/internal/client/etask"
	grpcpkg "github.com/Duke1616/etask/pkg/grpc"
	"github.com/Duke1616/etask/pkg/grpc/registry"
	"github.com/spf13/viper"
)

// InitECMDBGrpcClient 初始化 ECMDB gRPC 客户端
func InitECMDBGrpcClient(reg registry.Registry) ecmdb.ECMDBConn {
	var cfg grpcpkg.ClientConfig
	if err := viper.UnmarshalKey("grpc.client.ecmdb", &cfg); err != nil {
		panic(err)
	}

	cc, err := grpcpkg.NewClientConn(
		reg,
		grpcpkg.WithServiceName(cfg.Name),
		grpcpkg.WithClientJWTAuth(cfg.AuthToken),
	)
	if err != nil {
		panic(err)
	}

	return cc
}

// InitEIAMGrpcClient 初始化 EIAM gRPC 客户端
func InitEIAMGrpcClient(reg registry.Registry) eiam.EIAMConn {
	var cfg grpcpkg.ClientConfig
	if err := viper.UnmarshalKey("grpc.client.eiam", &cfg); err != nil {
		panic(err)
	}

	cc, err := grpcpkg.NewClientConn(
		reg,
		grpcpkg.WithServiceName(cfg.Name),
		grpcpkg.WithClientJWTAuth(cfg.AuthToken),
	)
	if err != nil {
		panic(err)
	}

	return cc
}

// InitLocalEALERTClient 在未部署 ealert 的环境中构建本地 ealert 客户端网关：
// 审批人解析在本地完成（用户/部门走 ECMDB 提供的 eiam 兼容服务，值班走 ECMDB rota），
// 其余 ealert 能力（团队、群聊、通知模板）返回 Unimplemented，消息改由通知脚本发送。
func InitLocalEALERTClient(users userv1.UserServiceClient, departments departmentv1.DepartmentServiceClient,
	ecmdbClient *ecmdb.ECMDBClient) *ealert.EALERTClient {
	unavailable := local.NewUnavailableConn()
	return &ealert.EALERTClient{
		TeamClient:         teamv1.NewTeamServiceClient(unavailable),
		NotificationClient: local.NewNotificationClient(users, departments, ecmdbClient.RotaClient),
		TemplateClient:     templatev1.NewTemplateServiceClient(unavailable),
		OnCallClient:       oncallv1.NewOnCallServiceClient(unavailable),
	}
}

// InitEALERTGrpcClient 初始化 EALERT gRPC 客户端（保留上游实现，接入 ealert 时在 grpcSet 中替换回来）
func InitEALERTGrpcClient(reg registry.Registry) ealert.EALERTConn {
	var cfg grpcpkg.ClientConfig
	if err := viper.UnmarshalKey("grpc.client.ealert", &cfg); err != nil {
		panic(err)
	}

	cc, err := grpcpkg.NewClientConn(
		reg,
		grpcpkg.WithServiceName(cfg.Name),
		grpcpkg.WithClientJWTAuth(cfg.AuthToken),
	)
	if err != nil {
		panic(err)
	}

	return cc
}

// InitETASKGrpcClient 初始化 ETASK gRPC 客户端
func InitETASKGrpcClient(reg registry.Registry) etask.ETASKConn {
	var cfg grpcpkg.ClientConfig
	if err := viper.UnmarshalKey("grpc.client.etask", &cfg); err != nil {
		panic(err)
	}

	cc, err := grpcpkg.NewClientConn(
		reg,
		grpcpkg.WithServiceName(cfg.Name),
		grpcpkg.WithClientJWTAuth(cfg.AuthToken),
	)
	if err != nil {
		panic(err)
	}

	return cc
}
