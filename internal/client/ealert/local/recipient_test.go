package local

import (
	"context"
	"testing"

	notificationv1 "github.com/Duke1616/eflow/api/proto/gen/ealert/notification/v1"
	departmentv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/department/v1"
	userv1 "github.com/Duke1616/eflow/api/proto/gen/eiam/user/v1"
	rotav1 "github.com/Duke1616/ecmdb/api/proto/gen/ecmdb/rota/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var directory = map[string]int64{"alice": 1, "lead_a": 10, "lead_b": 11, "boss": 20, "ops1": 30, "ops2": 31}

type fakeUsers struct{ userv1.UserServiceClient }

func (fakeUsers) QueryByUsernames(_ context.Context, in *userv1.QueryByUsernamesReq,
	_ ...grpc.CallOption) (*userv1.QueryUsersResp, error) {
	var users []*userv1.User
	for _, name := range in.GetUsernames() {
		if id, ok := directory[name]; ok {
			users = append(users, &userv1.User{Id: id, Username: name})
		}
	}
	return &userv1.QueryUsersResp{Users: users}, nil
}

type fakeDepartments struct{}

func (fakeDepartments) QueryByUserId(_ context.Context, in *departmentv1.QueryByUserIdReq,
	_ ...grpc.CallOption) (*departmentv1.QueryDepartmentsResp, error) {
	if in.GetUserId() != 1 {
		return &departmentv1.QueryDepartmentsResp{}, nil
	}
	return &departmentv1.QueryDepartmentsResp{Departments: []*departmentv1.Department{
		{Id: 5, Leaders: []string{"lead_a", "lead_b"}, MainLeader: "boss"},
	}}, nil
}

func (fakeDepartments) ListMembers(_ context.Context, in *departmentv1.ListMembersReq,
	_ ...grpc.CallOption) (*departmentv1.ListMembersResp, error) {
	if in.GetDepartmentId() != 5 {
		return &departmentv1.ListMembersResp{}, nil
	}
	return &departmentv1.ListMembersResp{Members: []*userv1.User{{Id: 1}, {Id: 10}}}, nil
}

type fakeRota struct{ rotav1.OnCallServiceClient }

func (fakeRota) GetCurrentSchedulesByIDs(_ context.Context, _ *rotav1.GetCurrentSchedulesByIDsRequest,
	_ ...grpc.CallOption) (*rotav1.Schedules, error) {
	return &rotav1.Schedules{Schedules: []*rotav1.Schedule{
		{RotaGroup: &rotav1.RotaGroup{Members: []string{"ops1", "ops2"}}},
	}}, nil
}

func selector(t notificationv1.RecipientSelectorType, ids ...int64) *notificationv1.RecipientSelector {
	return &notificationv1.RecipientSelector{Type: t, TargetIds: ids}
}

func TestResolveRecipients(t *testing.T) {
	client := NewNotificationClient(fakeUsers{}, fakeDepartments{}, fakeRota{})

	testCases := []struct {
		name      string
		selectors []*notificationv1.RecipientSelector
		want      []int64
	}{
		{name: "指定用户原样返回",
			selectors: []*notificationv1.RecipientSelector{selector(notificationv1.RecipientSelectorType_RECIPIENT_USER, 1, 2)},
			want:      []int64{1, 2}},
		{name: "部门负责人",
			selectors: []*notificationv1.RecipientSelector{selector(notificationv1.RecipientSelectorType_RECIPIENT_DEPARTMENT_LEADER, 1)},
			want:      []int64{10, 11}},
		{name: "分管领导",
			selectors: []*notificationv1.RecipientSelector{selector(notificationv1.RecipientSelectorType_RECIPIENT_SUPERVISING_LEADER, 1)},
			want:      []int64{20}},
		{name: "部门成员",
			selectors: []*notificationv1.RecipientSelector{selector(notificationv1.RecipientSelectorType_RECIPIENT_DEPARTMENT, 5)},
			want:      []int64{1, 10}},
		{name: "当前值班人",
			selectors: []*notificationv1.RecipientSelector{selector(notificationv1.RecipientSelectorType_RECIPIENT_ONCALL, 99)},
			want:      []int64{30, 31}},
		{name: "团队规则不支持时跳过，其余规则正常合并去重",
			selectors: []*notificationv1.RecipientSelector{
				selector(notificationv1.RecipientSelectorType_RECIPIENT_TEAM, 7),
				selector(notificationv1.RecipientSelectorType_RECIPIENT_USER, 10),
				selector(notificationv1.RecipientSelectorType_RECIPIENT_DEPARTMENT_LEADER, 1),
			},
			want: []int64{10, 11}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.ResolveRecipients(context.Background(),
				&notificationv1.ResolveRecipientsRequest{Recipients: tc.selectors})
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.want, resp.GetUserIds())
			assert.Equal(t, notificationv1.ErrorCode_ERROR_CODE_UNSPECIFIED, resp.GetErrorCode())
		})
	}
}

func TestUnavailableClients(t *testing.T) {
	client := NewNotificationClient(fakeUsers{}, nil, nil)
	_, err := client.SendNotification(context.Background(), &notificationv1.SendNotificationRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err), "消息发送应交给通知脚本")

	err = NewUnavailableConn().Invoke(context.Background(), "/ealert.team.v1.TeamService/List", nil, nil)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}
