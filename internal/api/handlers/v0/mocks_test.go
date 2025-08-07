package v0_test

import (
	"context"

	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/mock"
)

// MockRegistryService is a mock implementation of the RegistryService interface
type MockRegistryService struct {
	mock.Mock
}

func (m *MockRegistryService) List(cursor string, limit int) ([]model.Server, string, error) {
	args := m.Called(cursor, limit)
	return args.Get(0).([]model.Server), args.String(1), args.Error(2)
}

func (m *MockRegistryService) GetByID(id string) (*model.ServerDetail, error) {
	args := m.Called(id)
	return args.Get(0).(*model.ServerDetail), args.Error(1)
}

func (m *MockRegistryService) Publish(serverDetail *model.ServerDetail) error {
	args := m.Called(serverDetail)
	return args.Error(0)
}

// MockAuthService is a mock implementation of the auth.Service interface
type MockAuthService struct {
	mock.Mock
}

func (m *MockAuthService) StartAuthFlow(
	ctx context.Context, method model.AuthMethod, repoRef string,
) (map[string]string, string, error) {
	args := m.Called(ctx, method, repoRef)
	return args.Get(0).(map[string]string), args.String(1), args.Error(2)
}

func (m *MockAuthService) CheckAuthStatus(ctx context.Context, statusToken string) (string, error) {
	args := m.Called(ctx, statusToken)
	return args.String(0), args.Error(1)
}

func (m *MockAuthService) ValidateAuth(ctx context.Context, authentication model.Authentication) (bool, error) {
	args := m.Called(ctx, authentication)
	return args.Bool(0), args.Error(1)
}
