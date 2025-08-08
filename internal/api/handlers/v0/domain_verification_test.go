package v0_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockRegistryServiceForDomainVerification is a mock implementation of the RegistryService interface for domain verification tests
type MockRegistryServiceForDomainVerification struct {
	mock.Mock
}

func (m *MockRegistryServiceForDomainVerification) List(cursor string, limit int) ([]model.Server, string, error) {
	args := m.Mock.Called(cursor, limit)
	return args.Get(0).([]model.Server), args.String(1), args.Error(2)
}

func (m *MockRegistryServiceForDomainVerification) GetByID(id string) (*model.ServerDetail, error) {
	args := m.Mock.Called(id)
	return args.Get(0).(*model.ServerDetail), args.Error(1)
}

func (m *MockRegistryServiceForDomainVerification) Publish(serverDetail *model.ServerDetail) error {
	args := m.Mock.Called(serverDetail)
	return args.Error(0)
}

func (m *MockRegistryServiceForDomainVerification) ClaimDomain(domain string) (*model.VerificationToken, error) {
	args := m.Mock.Called(domain)
	return args.Get(0).(*model.VerificationToken), args.Error(1)
}

func (m *MockRegistryServiceForDomainVerification) GetDomainVerificationStatus(domain string) (*model.VerificationTokens, error) {
	args := m.Mock.Called(domain)
	return args.Get(0).(*model.VerificationTokens), args.Error(1)
}

func TestClaimDomainHandler(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		requestBody    interface{}
		setupMocks     func(*MockRegistryServiceForDomainVerification)
		expectedStatus int
		checkResponse  func(t *testing.T, response *v0.DomainClaimResponse)
	}{
		{
			name:   "successful domain claim",
			method: http.MethodPost,
			requestBody: v0.DomainClaimRequest{
				Domain: "example.com",
			},
			setupMocks: func(registry *MockRegistryServiceForDomainVerification) {
				registry.On("ClaimDomain", "example.com").Return(&model.VerificationToken{
					Token:     "test-token-123",
					CreatedAt: time.Now(),
				}, nil)
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, response *v0.DomainClaimResponse) {
				assert.Equal(t, "example.com", response.Domain)
				assert.Equal(t, "example.com", response.NormalizedDomain)
				assert.Equal(t, "test-token-123", response.Token)
				assert.NotEmpty(t, response.CreatedAt)
			},
		},
		{
			name:           "method not allowed",
			method:         http.MethodGet,
			requestBody:    nil,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:   "missing domain",
			method: http.MethodPost,
			requestBody: v0.DomainClaimRequest{
				Domain: "",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRegistry := new(MockRegistryServiceForDomainVerification)

			if tt.setupMocks != nil {
				tt.setupMocks(mockRegistry)
			}

			handler := v0.ClaimDomainHandler(mockRegistry)

			var reqBody []byte
			if tt.requestBody != nil {
				var err error
				reqBody, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(tt.method, "/v0/domains/claim", bytes.NewReader(reqBody))

			w := httptest.NewRecorder()
			handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkResponse != nil && w.Code == http.StatusCreated {
				var response v0.DomainClaimResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.checkResponse(t, &response)
			}

			mockRegistry.AssertExpectations(t)
		})
	}
}

func TestGetDomainStatusHandler(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		queryParam     string
		setupMocks     func(*MockRegistryServiceForDomainVerification)
		expectedStatus int
		checkResponse  func(t *testing.T, response *v0.DomainStatusResponse)
	}{
		{
			name:       "domain with verified token",
			method:     http.MethodGet,
			queryParam: "domain=verified.com",
			setupMocks: func(registry *MockRegistryServiceForDomainVerification) {
				verifiedAt := time.Now()
				registry.On("GetDomainVerificationStatus", "verified.com").Return(&model.VerificationTokens{
					VerifiedToken: &model.VerificationToken{
						Token:          "verified-token",
						CreatedAt:      time.Now(),
						LastVerifiedAt: &verifiedAt,
					},
					PendingTokens: []model.VerificationToken{},
				}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, response *v0.DomainStatusResponse) {
				assert.Equal(t, "verified.com", response.Domain)
				assert.Equal(t, "verified", response.Status)
			},
		},
		{
			name:       "domain with pending tokens only",
			method:     http.MethodGet,
			queryParam: "domain=pending.com",
			setupMocks: func(registry *MockRegistryServiceForDomainVerification) {
				registry.On("GetDomainVerificationStatus", "pending.com").Return(&model.VerificationTokens{
					VerifiedToken: nil,
					PendingTokens: []model.VerificationToken{
						{
							Token:     "pending-token-1",
							CreatedAt: time.Now(),
						},
						{
							Token:     "pending-token-2",
							CreatedAt: time.Now(),
						},
					},
				}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, response *v0.DomainStatusResponse) {
				assert.Equal(t, "pending.com", response.Domain)
				assert.Equal(t, "unverified", response.Status)
			},
		},
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			queryParam:     "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "missing domain parameter",
			method:         http.MethodGet,
			queryParam:     "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRegistry := new(MockRegistryServiceForDomainVerification)

			if tt.setupMocks != nil {
				tt.setupMocks(mockRegistry)
			}

			handler := v0.GetDomainStatusHandler(mockRegistry)

			url := "/v0/domains/status"
			if tt.queryParam != "" {
				url = url + "?" + tt.queryParam
			}

			req := httptest.NewRequest(tt.method, url, nil)

			w := httptest.NewRecorder()
			handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkResponse != nil && w.Code == http.StatusOK {
				var response v0.DomainStatusResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.checkResponse(t, &response)
			}

			mockRegistry.AssertExpectations(t)
		})
	}
}
