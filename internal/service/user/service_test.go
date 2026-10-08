package user_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/z-alamsyah/codebase-go/internal/model"
	"github.com/z-alamsyah/codebase-go/internal/service/user"
	"github.com/z-alamsyah/codebase-go/internal/service/user/mocks"
)

var errBoom = errors.New("boom")

type deps struct {
	repo   *mocks.MockRepository
	cache  *mocks.MockCache
	pub    *mocks.MockPublisher
	idem   *mocks.MockIdempotencyStore
	hasher *mocks.MockPasswordHasher
	mailer *mocks.MockMailer
}

// newService builds the service with strict mocks: anything call that is not
// expected by a test case fails the test.
func newService(t *testing.T) (*user.Service, deps) {
	t.Helper()
	ctrl := gomock.NewController(t)
	d := deps{
		repo:   mocks.NewMockRepository(ctrl),
		cache:  mocks.NewMockCache(ctrl),
		pub:    mocks.NewMockPublisher(ctrl),
		idem:   mocks.NewMockIdempotencyStore(ctrl),
		hasher: mocks.NewMockPasswordHasher(ctrl),
		mailer: mocks.NewMockMailer(ctrl),
	}
	svc := user.NewService(user.Deps{
		Repo:        d.repo,
		Cache:       d.cache,
		Publisher:   d.pub,
		Idempotency: d.idem,
		Hasher:      d.hasher,
		Mailer:      d.mailer,
		Logger:      slog.New(slog.DiscardHandler),
	})
	return svc, d
}

func TestService_GetByID(t *testing.T) {
	id := uuid.MustParse("01928f6a-0000-7000-8000-000000000001")
	stored := model.User{ID: id, Name: "Andi Pratama", Email: "andi@example.com"}
	anything := gomock.Any()

	tests := []struct {
		name    string
		setup   func(d deps)
		want    model.User
		wantErr error
	}{
		{
			name: "cache hit returns cached user without querying the database",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(stored, true, nil)
			},
			want: stored,
		},
		{
			name: "cache miss loads from database and fills the cache",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(model.User{}, false, nil)
				d.repo.EXPECT().FindByID(anything, id).Return(stored, nil)
				d.cache.EXPECT().Set(anything, stored).Return(nil)
			},
			want: stored,
		},
		{
			name: "cache read error falls back to database",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(model.User{}, false, errBoom)
				d.repo.EXPECT().FindByID(anything, id).Return(stored, nil)
				d.cache.EXPECT().Set(anything, stored).Return(nil)
			},
			want: stored,
		},
		{
			name: "cache write error does not fail the request",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(model.User{}, false, nil)
				d.repo.EXPECT().FindByID(anything, id).Return(stored, nil)
				d.cache.EXPECT().Set(anything, stored).Return(errBoom)
			},
			want: stored,
		},
		{
			name: "user not found",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(model.User{}, false, nil)
				d.repo.EXPECT().FindByID(anything, id).Return(model.User{}, fmt.Errorf("%w: user %s", model.ErrNotFound, id))
			},
			wantErr: model.ErrNotFound,
		},
		{
			name: "database error is returned",
			setup: func(d deps) {
				d.cache.EXPECT().Get(anything, id).Return(model.User{}, false, nil)
				d.repo.EXPECT().FindByID(anything, id).Return(model.User{}, errBoom)
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newService(t)
			tt.setup(d)

			got, err := svc.GetByID(context.Background(), id)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_Create(t *testing.T) {
	id := uuid.MustParse("01928f6a-0000-7000-8000-0000000000aa")
	createdAt := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	anything := gomock.Any()

	// Input with messy formatting: the service must trim and lowercase the email.
	input := user.CreateInput{Name: " Andi Pratama ", Email: " Andi@Example.COM ", Phone: "+6281200000001", Password: "Password123!"}
	const email = "andi@example.com"

	// fakeInsert simulates the database assigning an ID and timestamps.
	fakeInsert := func(_ context.Context, u *model.User) error {
		u.ID = id
		u.CreatedAt = createdAt
		u.UpdatedAt = createdAt
		return nil
	}

	tests := []struct {
		name    string
		setup   func(d deps)
		wantErr error
	}{
		{
			name: "creates user and publishes user.created",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(false, nil)
				d.hasher.EXPECT().Hash("Password123!").Return("hashed", nil)
				d.repo.EXPECT().Create(anything, &model.User{Name: "Andi Pratama", Email: email, Phone: "+6281200000001", PasswordHash: "hashed"}).
					DoAndReturn(fakeInsert)
				d.pub.EXPECT().Publish(anything, model.EventUserCreated, model.UserCreatedEvent{
					UserID: id.String(), Name: "Andi Pratama", Email: email, CreatedAt: createdAt,
				}).Return(nil)
			},
		},
		{
			name: "publish failure does not fail the request",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(false, nil)
				d.hasher.EXPECT().Hash(anything).Return("hashed", nil)
				d.repo.EXPECT().Create(anything, anything).DoAndReturn(fakeInsert)
				d.pub.EXPECT().Publish(anything, model.EventUserCreated, anything).Return(errBoom)
			},
		},
		{
			name: "email already registered returns conflict",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(true, nil)
			},
			wantErr: model.ErrConflict,
		},
		{
			name: "email check error is returned",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(false, errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "hash error is returned and nothing is stored",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(false, nil)
				d.hasher.EXPECT().Hash(anything).Return("", errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "duplicate insert (race with another request) returns conflict and publishes nothing",
			setup: func(d deps) {
				d.repo.EXPECT().ExistsByEmail(anything, email).Return(false, nil)
				d.hasher.EXPECT().Hash(anything).Return("hashed", nil)
				d.repo.EXPECT().Create(anything, anything).Return(fmt.Errorf("%w: email taken", model.ErrConflict))
			},
			wantErr: model.ErrConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newService(t)
			tt.setup(d)

			got, err := svc.Create(context.Background(), input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, model.User{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, id, got.ID)
			assert.Equal(t, email, got.Email)
			assert.Equal(t, "Andi Pratama", got.Name)
			assert.Equal(t, createdAt, got.CreatedAt)
		})
	}
}

func TestService_SendWelcomeEmail(t *testing.T) {
	ev := model.UserCreatedEvent{UserID: "01928f6a-0000-7000-8000-000000000001", Name: "Andi Pratama", Email: "andi@example.com"}
	key := "welcome-email:" + ev.UserID
	anything := gomock.Any()

	tests := []struct {
		name    string
		setup   func(d deps)
		wantErr error
	}{
		{
			name: "first delivery sends the email",
			setup: func(d deps) {
				d.idem.EXPECT().Acquire(anything, key, 24*time.Hour).Return(true, nil)
				d.mailer.EXPECT().SendWelcomeEmail(anything, ev.Email, ev.Name).Return(nil)
			},
		},
		{
			name: "duplicate delivery is skipped",
			setup: func(d deps) {
				d.idem.EXPECT().Acquire(anything, key, 24*time.Hour).Return(false, nil)
			},
		},
		{
			name: "idempotency store error is returned so the message is retried",
			setup: func(d deps) {
				d.idem.EXPECT().Acquire(anything, key, anything).Return(false, errBoom)
			},
			wantErr: errBoom,
		},
		{
			name: "mailer error releases the key so a retry can send again",
			setup: func(d deps) {
				d.idem.EXPECT().Acquire(anything, key, anything).Return(true, nil)
				d.mailer.EXPECT().SendWelcomeEmail(anything, ev.Email, ev.Name).Return(errBoom)
				d.idem.EXPECT().Release(anything, key).Return(nil)
			},
			wantErr: errBoom,
		},
		{
			name: "release error still returns the mailer error",
			setup: func(d deps) {
				d.idem.EXPECT().Acquire(anything, key, anything).Return(true, nil)
				d.mailer.EXPECT().SendWelcomeEmail(anything, anything, anything).Return(errBoom)
				d.idem.EXPECT().Release(anything, key).Return(errors.New("redis down"))
			},
			wantErr: errBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, d := newService(t)
			tt.setup(d)

			err := svc.SendWelcomeEmail(context.Background(), ev)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
