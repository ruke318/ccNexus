package storage

import "time"

type Endpoint struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	APIUrl      string    `json:"apiUrl"`
	APIKey      string    `json:"apiKey"`
	AuthType    string    `json:"authType"`
	CodexPoolID int64     `json:"codexPoolId"`
	Enabled     bool      `json:"enabled"`
	Transformer string    `json:"transformer"`
	Model       string    `json:"model"`
	Remark      string    `json:"remark"`
	ClientType  string    `json:"clientType"`
	ProxyURL    string    `json:"proxyUrl"`
	SortOrder   int       `json:"sortOrder"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type DailyStat struct {
	ID           int64
	EndpointName string
	Date         string
	Requests     int
	Errors       int
	InputTokens  int
	OutputTokens int
	DeviceID     string
	CreatedAt    time.Time
}

type EndpointStats struct {
	Requests     int
	Errors       int
	InputTokens  int64
	OutputTokens int64
}

type CodexSlot struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	StateDir      string    `json:"stateDir"`
	AccountID     string    `json:"accountId"`
	Status        string    `json:"status"`
	Enabled       bool      `json:"enabled"`
	CooldownUntil time.Time `json:"cooldownUntil"`
	LastCheckedAt time.Time `json:"lastCheckedAt"`
	LastUsedAt    time.Time `json:"lastUsedAt"`
	LastError     string    `json:"lastError"`
	Remark        string    `json:"remark"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type CodexPool struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Strategy          string    `json:"strategy"`
	Enabled           bool      `json:"enabled"`
	Cooldown429Sec    int       `json:"cooldown429Sec"`
	Cooldown5xxSec    int       `json:"cooldown5xxSec"`
	AuthExpiredPolicy string    `json:"authExpiredPolicy"`
	SlotIDs           []int64   `json:"slotIds"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Storage interface {
	// Endpoints
	GetEndpoints() ([]Endpoint, error)
	SaveEndpoint(ep *Endpoint) error
	UpdateEndpoint(ep *Endpoint) error
	DeleteEndpoint(id int64) error

	// Stats
	RecordDailyStat(stat *DailyStat) error
	GetDailyStats(endpointName, startDate, endDate string) ([]DailyStat, error)
	GetAllStats() (map[string][]DailyStat, error)
	GetTotalStats() (int, map[string]*EndpointStats, error)
	GetEndpointTotalStats(endpointName string) (*EndpointStats, error)

	// Config
	GetConfig(key string) (string, error)
	SetConfig(key, value string) error

	// Close
	Close() error
}
