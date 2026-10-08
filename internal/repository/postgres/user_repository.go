// Package postgres implements repositories backed by PostgreSQL via GORM.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/z-alamsyah/codebase-go/internal/model"
)

// userEntity is the GORM mapping of the users table. It stays inside the
// repository; callers only see model.User.
type userEntity struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name         string
	Email        string
	Phone        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"` // enables GORM soft delete
}

func (userEntity) TableName() string { return "users" }

// BeforeCreate is a GORM hook that assigns a time-ordered UUID (v7), which
// keeps the primary key index compact compared to random UUIDs.
func (e *userEntity) BeforeCreate(*gorm.DB) error {
	if e.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		e.ID = id
	}
	return nil
}

func (e userEntity) toModel() model.User {
	return model.User{
		ID:           e.ID,
		Name:         e.Name,
		Email:        e.Email,
		Phone:        e.Phone,
		PasswordHash: e.PasswordHash,
		CreatedAt:    e.CreatedAt.UTC(),
		UpdatedAt:    e.UpdatedAt.UTC(),
	}
}

func newUserEntity(u model.User) userEntity {
	return userEntity{
		ID:           u.ID,
		Name:         u.Name,
		Email:        u.Email,
		Phone:        u.Phone,
		PasswordHash: u.PasswordHash,
	}
}

// UserRepository implements user.Repository.
type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	var e userEntity
	err := r.db.WithContext(ctx).First(&e, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, fmt.Errorf("%w: user %s", model.ErrNotFound, id)
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	return e.toModel(), nil
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&userEntity{}).Where("email = ?", email).Limit(1).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("count users by email: %w", err)
	}
	return count > 0, nil
}

// Create inserts u and fills the generated ID and timestamps back into it.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	e := newUserEntity(*u)
	err := r.db.WithContext(ctx).Create(&e).Error
	// gorm.ErrDuplicatedKey requires gorm.Config{TranslateError: true}.
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: email %s is already registered", model.ErrConflict, u.Email)
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	*u = e.toModel()
	return nil
}
