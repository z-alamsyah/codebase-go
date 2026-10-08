package rpc

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/z-alamsyah/codebase-go/gen/proto/user/v1"
	"github.com/z-alamsyah/codebase-go/internal/controller/dto"
	"github.com/z-alamsyah/codebase-go/internal/model"
)

// UserServer implements userv1.UserServiceServer.
type UserServer struct {
	userv1.UnimplementedUserServiceServer
	svc UserService
}

func NewUserServer(svc UserService) *UserServer {
	return &UserServer{svc: svc}
}

func (s *UserServer) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return nil, toStatus(ctx, model.NewValidationError("id", "must be a valid UUID"))
	}

	u, err := s.svc.GetByID(ctx, id)
	if err != nil {
		return nil, toStatus(ctx, err)
	}
	return &userv1.GetUserResponse{User: toProtoUser(u)}, nil
}

func (s *UserServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	in := dto.CreateUserRequest{
		Name:     req.GetName(),
		Email:    req.GetEmail(),
		Phone:    req.GetPhone(),
		Password: req.GetPassword(),
	}
	if err := dto.Validate(in); err != nil {
		return nil, toStatus(ctx, err)
	}

	u, err := s.svc.Create(ctx, in.ToInput())
	if err != nil {
		return nil, toStatus(ctx, err)
	}
	return &userv1.CreateUserResponse{User: toProtoUser(u)}, nil
}

func toProtoUser(u model.User) *userv1.User {
	return &userv1.User{
		Id:        u.ID.String(),
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		CreatedAt: timestamppb.New(u.CreatedAt),
		UpdatedAt: timestamppb.New(u.UpdatedAt),
	}
}
