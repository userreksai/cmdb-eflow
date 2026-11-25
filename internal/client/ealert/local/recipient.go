package local

import (
	"context"
	"fmt"

	notificationv1 "github.com/Duke1616/eflow/api/proto/gen/ealert/notification/v1"
	departmentv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/department/v1"
	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	rotav1 "github.com/Duke1616/ecmdb/api/proto/gen/ecmdb/rota/v1"
	"github.com/gotomicro/ego/core/elog"
	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const departmentMemberPageSize = 500

// notificationClient 本地实现的 ealert NotificationService 客户端：只负责审批人解析。
type notificationClient struct {
	users       userv1.UserServiceClient
	departments departmentv1.DepartmentServiceClient
	rota        rotav1.OnCallServiceClient
	logger      *elog.Component
}

// NewNotificationClient 创建本地审批人解析客户端；rota 为空时不支持值班规则。
func NewNotificationClient(users userv1.UserServiceClient, departments departmentv1.DepartmentServiceClient,
	rota rotav1.OnCallServiceClient) notificationv1.NotificationServiceClient {
	return &notificationClient{
		users: users, departments: departments, rota: rota,
		logger: elog.DefaultLogger.With(elog.FieldComponentName("ealert.local")),
	}
}

// SendNotification 不在 eflow 内直接发送：消息通过通知脚本投递，返回错误让通道降级到下一个 Provider。
func (c *notificationClient) SendNotification(_ context.Context, _ *notificationv1.SendNotificationRequest,
	_ ...grpc.CallOption) (*notificationv1.SendNotificationResponse, error) {
	return nil, status.Error(codes.Unimplemented, "本部署未接入 ealert，消息由通知脚本发送")
}

// ResolveRecipients 按选择器类型在本地解析用户 ID。单个选择器失败只记录日志，
// 与 ealert 的语义一致：调用方在结果为空时会降级到静态指定人员或提单人。
func (c *notificationClient) ResolveRecipients(ctx context.Context, in *notificationv1.ResolveRecipientsRequest,
	_ ...grpc.CallOption) (*notificationv1.ResolveRecipientsResponse, error) {
	var ids []int64
	for _, selector := range in.GetRecipients() {
		resolved, err := c.resolve(ctx, selector)
		if err != nil {
			c.logger.Warn("审批人解析失败，已跳过该规则",
				elog.String("type", selector.GetType().String()), elog.FieldErr(err))
			continue
		}
		ids = append(ids, resolved...)
	}
	return &notificationv1.ResolveRecipientsResponse{UserIds: lo.Uniq(ids)}, nil
}

func (c *notificationClient) resolve(ctx context.Context, selector *notificationv1.RecipientSelector) ([]int64, error) {
	targets := selector.GetTargetIds()
	switch selector.GetType() {
	case notificationv1.RecipientSelectorType_RECIPIENT_USER:
		return targets, nil
	case notificationv1.RecipientSelectorType_RECIPIENT_DEPARTMENT_LEADER:
		return c.leadersOf(ctx, targets, false)
	case notificationv1.RecipientSelectorType_RECIPIENT_SUPERVISING_LEADER:
		return c.leadersOf(ctx, targets, true)
	case notificationv1.RecipientSelectorType_RECIPIENT_DEPARTMENT:
		return c.departmentMembers(ctx, targets)
	case notificationv1.RecipientSelectorType_RECIPIENT_ONCALL:
		return c.onCallMembers(ctx, targets)
	case notificationv1.RecipientSelectorType_RECIPIENT_TEAM:
		return nil, fmt.Errorf("本部署未接入 ealert，不支持按团队分配审批人")
	default:
		return nil, fmt.Errorf("不支持的审批人选择器类型: %s", selector.GetType())
	}
}

// leadersOf 解析用户所在部门的负责人（main=false）或分管领导（main=true）。
func (c *notificationClient) leadersOf(ctx context.Context, userIDs []int64, main bool) ([]int64, error) {
	if c.departments == nil {
		return nil, fmt.Errorf("未配置部门服务")
	}
	var usernames []string
	for _, uid := range userIDs {
		resp, err := c.departments.QueryByUserId(ctx, &departmentv1.QueryByUserIdReq{UserId: uid})
		if err != nil {
			return nil, fmt.Errorf("查询用户 %d 所在部门失败: %w", uid, err)
		}
		for _, dept := range resp.GetDepartments() {
			if main {
				if dept.GetMainLeader() != "" {
					usernames = append(usernames, dept.GetMainLeader())
				}
				continue
			}
			usernames = append(usernames, dept.GetLeaders()...)
		}
	}
	return c.idsByUsernames(ctx, usernames)
}

func (c *notificationClient) departmentMembers(ctx context.Context, departmentIDs []int64) ([]int64, error) {
	if c.departments == nil {
		return nil, fmt.Errorf("未配置部门服务")
	}
	var ids []int64
	for _, deptID := range departmentIDs {
		resp, err := c.departments.ListMembers(ctx, &departmentv1.ListMembersReq{
			DepartmentId: deptID, Limit: departmentMemberPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("查询部门 %d 成员失败: %w", deptID, err)
		}
		ids = append(ids, lo.FilterMap(resp.GetMembers(), func(u *userv1.User, _ int) (int64, bool) {
			return u.GetId(), u.GetId() > 0
		})...)
	}
	return ids, nil
}

func (c *notificationClient) onCallMembers(ctx context.Context, scheduleIDs []int64) ([]int64, error) {
	if c.rota == nil {
		return nil, fmt.Errorf("未配置 ECMDB 值班服务")
	}
	resp, err := c.rota.GetCurrentSchedulesByIDs(ctx, &rotav1.GetCurrentSchedulesByIDsRequest{Ids: scheduleIDs})
	if err != nil {
		return nil, fmt.Errorf("查询当前值班失败: %w", err)
	}
	usernames := lo.FlatMap(resp.GetSchedules(), func(s *rotav1.Schedule, _ int) []string {
		return s.GetRotaGroup().GetMembers()
	})
	return c.idsByUsernames(ctx, usernames)
}

func (c *notificationClient) idsByUsernames(ctx context.Context, usernames []string) ([]int64, error) {
	usernames = lo.Uniq(lo.Compact(usernames))
	if len(usernames) == 0 {
		return nil, nil
	}
	resp, err := c.users.QueryByUsernames(ctx, &userv1.QueryByUsernamesReq{Usernames: usernames})
	if err != nil {
		return nil, fmt.Errorf("按用户名查询用户失败: %w", err)
	}
	return lo.FilterMap(resp.GetUsers(), func(u *userv1.User, _ int) (int64, bool) {
		return u.GetId(), u.GetId() > 0
	}), nil
}
