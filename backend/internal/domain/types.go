package domain

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	EndpointChat       = "chat"
	EndpointResponses  = "responses"
	EndpointEmbeddings = "embeddings"
	EndpointImages     = "images"
	EndpointMessages   = "messages"
	EndpointModels     = "models"

	BackendStatusNormal   = "normal"
	BackendStatusAbnormal = "abnormal"
	BackendStatusDisabled = "disabled"

	BackendProtocolOpenAI    = "openai"
	BackendProtocolAnthropic = "anthropic"
	BackendProtocolBoth      = "both"

	BackendTypeNewAPI  = "new-api"
	BackendTypeSub2API = "sub2api"
)

type ClientKey struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	TokenHash     string    `json:"-"`
	Token         string    `json:"token,omitempty"`
	TokenPrefix   string    `json:"token_prefix"`
	AllowedModels string    `json:"allowed_models"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type BackendAPIKey struct {
	ID           string            `json:"id,omitempty"`
	APIKey       string            `json:"api_key"`
	Name         string            `json:"name"`
	Group        string            `json:"group"`
	Models       []string          `json:"models"`
	ModelMapping map[string]string `json:"model_mapping"`
	UsedQuota    int64             `json:"used_quota"`
}

func (k *BackendAPIKey) UnmarshalJSON(data []byte) error {
	var value struct {
		ID           string            `json:"id"`
		Key          string            `json:"key"`
		APIKey       string            `json:"api_key"`
		Name         string            `json:"name"`
		Group        string            `json:"group"`
		Models       []string          `json:"models"`
		ModelMapping map[string]string `json:"model_mapping"`
		UsedQuota    int64             `json:"used_quota"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	k.ID = value.ID
	k.APIKey = value.Key
	if strings.TrimSpace(k.APIKey) == "" {
		k.APIKey = value.APIKey
	}
	k.Name = value.Name
	k.Group = value.Group
	k.Models = value.Models
	k.ModelMapping = value.ModelMapping
	k.UsedQuota = value.UsedQuota
	return nil
}

type Backend struct {
	ID                     int64             `json:"id"`
	Name                   string            `json:"name"`
	Protocol               string            `json:"protocol"`
	BackendType            string            `json:"backend_type"`
	BaseURL                string            `json:"base_url"`
	APIKeys                []BackendAPIKey   `json:"api_keys"`
	ConsoleURL             string            `json:"console_url"`
	Tags                   []string          `json:"tags"`
	ConsoleUsername        string            `json:"console_username"`
	ConsolePassword        string            `json:"console_password,omitempty"`
	NewAPIRefresh          string            `json:"new_api_refresh,omitempty"`
	ConsoleAuthorization   string            `json:"console_authorization,omitempty"`
	ConsoleCheckinPath     string            `json:"console_checkin_path,omitempty"`
	ConsoleCheckinWorkflow string            `json:"console_checkin_workflow_id,omitempty"`
	ChannelURL             string            `json:"channel_url,omitempty"`
	ConsoleCookie          string            `json:"console_cookie,omitempty"`
	ConsoleHeaders         map[string]string `json:"console_headers,omitempty"`
	ConsoleAccountJSON     string            `json:"console_account_json"`
	ConsolePricingJSON     string            `json:"console_pricing_json"`
	Notes                  string            `json:"notes"`
	ProxyID                int64             `json:"proxy_id"`
	Proxy                  *SocksProxy       `json:"proxy,omitempty"`
	Status                 string            `json:"status"`
	ConsecutiveFailures    int               `json:"consecutive_failures"`
	RecoverAt              *time.Time        `json:"recover_at,omitempty"`
	Weight                 int               `json:"weight"`
	// Legacy routing fields are retained internally for old database rows and
	// direct service callers. API responses use APIKeys as the canonical shape.
	APIKey       string            `json:"-"`
	Models       []string          `json:"-"`
	ModelMapping map[string]string `json:"-"`
	Endpoints    []string          `json:"-"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type SocksProxy struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Username  string    `json:"username"`
	Password  string    `json:"password,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AuditEvent struct {
	ID           int64     `json:"id"`
	Level        string    `json:"level"`
	Type         string    `json:"type"`
	Category     string    `json:"category"`
	Severity     string    `json:"severity"`
	Actor        string    `json:"actor"`
	ResourceType string    `json:"resource_type"`
	ResourceID   int64     `json:"resource_id"`
	Message      string    `json:"message"`
	ClientName   string    `json:"client_name"`
	Model        string    `json:"model"`
	Endpoint     string    `json:"endpoint"`
	BackendName  string    `json:"backend_name"`
	CreatedAt    time.Time `json:"created_at"`
}

type UsageLog struct {
	ID                  int64     `json:"id"`
	RequestID           string    `json:"request_id"`
	ClientID            int64     `json:"client_id"`
	ClientName          string    `json:"client_name"`
	ClientTokenPrefix   string    `json:"client_token_prefix"`
	Method              string    `json:"method"`
	Path                string    `json:"path"`
	Query               string    `json:"query"`
	Endpoint            string    `json:"endpoint"`
	Model               string    `json:"model"`
	BackendID           int64     `json:"backend_id"`
	BackendName         string    `json:"backend_name"`
	ProxyID             int64     `json:"proxy_id"`
	ProxyName           string    `json:"proxy_name"`
	Attempts            int       `json:"attempts"`
	StatusCode          int       `json:"status_code"`
	StatusFamily        string    `json:"status_family"`
	DurationMS          int64     `json:"duration_ms"`
	ErrorMessage        string    `json:"error_message"`
	ClientIP            string    `json:"client_ip"`
	UserAgent           string    `json:"user_agent"`
	TraceID             string    `json:"trace_id"`
	RequestBytes        int64     `json:"request_bytes"`
	ResponseBytes       int64     `json:"response_bytes"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	InputCacheTokens    int64     `json:"input_cache_tokens"`
	RequestHeadersJSON  string    `json:"request_headers_json"`
	RequestBodyPreview  string    `json:"request_body_preview"`
	ResponseHeadersJSON string    `json:"response_headers_json"`
	ResponseBodyPreview string    `json:"response_body_preview"`
	PreviewTruncated    bool      `json:"preview_truncated"`
	IsStream            bool      `json:"is_stream"`
	CreatedAt           time.Time `json:"created_at"`
}

func NormalizeBackendProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BackendProtocolAnthropic, "claude":
		return BackendProtocolAnthropic
	case BackendProtocolBoth, "dual", "openai+anthropic", "anthropic+openai":
		return BackendProtocolBoth
	default:
		return BackendProtocolOpenAI
	}
}

func NormalizeBackendType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case BackendTypeNewAPI, "newapi", "new_api":
		return BackendTypeNewAPI
	case BackendTypeSub2API, "sub-2-api", "sub_2_api":
		return BackendTypeSub2API
	default:
		return BackendTypeNewAPI
	}
}

func IsBackendFailureStatus(status int) bool {
	return status > 0 && (status < http.StatusOK || status >= http.StatusMultipleChoices)
}
