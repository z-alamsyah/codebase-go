package rpc_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	userv1 "github.com/z-alamsyah/codebase-go/gen/proto/user/v1"
	"github.com/z-alamsyah/codebase-go/internal/controller/rpc"
	"github.com/z-alamsyah/codebase-go/internal/controller/rpc/mocks"
	"github.com/z-alamsyah/codebase-go/internal/model"
)

// newClient starts the gRPC server over an in-memory connection (bufconn).
func newClient(t *testing.T, svc rpc.UserService) userv1.UserServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	userv1.RegisterUserServiceServer(srv, rpc.NewUserServer(svc))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return userv1.NewUserServiceClient(conn)
}

func TestUserServer_GetUser(t *testing.T) {
	id := uuid.MustParse("01928f6a-0000-7000-8000-000000000001")

	tests := []struct {
		name     string
		reqID    string
		setup    func(m *mocks.MockUserService)
		wantCode codes.Code
	}{
		{
			name:  "found",
			reqID: id.String(),
			setup: func(m *mocks.MockUserService) {
				m.EXPECT().GetByID(gomock.Any(), id).Return(model.User{ID: id, Name: "Andi"}, nil)
			},
			wantCode: codes.OK,
		},
		{
			name:     "invalid id",
			reqID:    "nope",
			setup:    func(*mocks.MockUserService) {},
			wantCode: codes.InvalidArgument,
		},
		{
			name:  "not found",
			reqID: id.String(),
			setup: func(m *mocks.MockUserService) {
				m.EXPECT().GetByID(gomock.Any(), id).Return(model.User{}, fmt.Errorf("%w: user", model.ErrNotFound))
			},
			wantCode: codes.NotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := mocks.NewMockUserService(gomock.NewController(t))
			tt.setup(svc)

			resp, err := newClient(t, svc).GetUser(context.Background(), &userv1.GetUserRequest{Id: tt.reqID})

			assert.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				assert.Equal(t, id.String(), resp.GetUser().GetId())
			}
		})
	}
}

func TestUserServer_CreateUser_ValidationDetails(t *testing.T) {
	svc := mocks.NewMockUserService(gomock.NewController(t))

	_, err := newClient(t, svc).CreateUser(context.Background(), &userv1.CreateUserRequest{Email: "bad"})

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	require.Len(t, st.Details(), 1)
	br, ok := st.Details()[0].(*errdetails.BadRequest)
	require.True(t, ok)
	var fields []string
	for _, v := range br.GetFieldViolations() {
		fields = append(fields, v.GetField())
	}
	assert.ElementsMatch(t, []string{"name", "email", "password"}, fields)
}
