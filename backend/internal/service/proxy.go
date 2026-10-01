package service

import (
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	FallbackModeNone   = "none"
	FallbackModeProxy  = "proxy"
	FallbackModeDirect = "direct"
)

type Proxy struct {
	ID             int64
	WorkspaceID    int64
	DeletedAt      *time.Time
	Name           string
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      *time.Time
	FallbackMode   string
	BackupProxyID  *int64
	ExpiryWarnDays int
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

// IsExpired 报告代理是否已过期（基于 expires_at，与 status 无关）。
func (p *Proxy) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

type ProxyWithAccountCount struct {
	Proxy
	AccountCount   int64
	LatencyMs      *int64
	LatencyStatus  string
	LatencyMessage string
	IPAddress      string
	Country        string
	CountryCode    string
	Region         string
	City           string
	QualityStatus  string
	QualityScore   *int
	QualityGrade   string
	QualitySummary string
	QualityChecked *int64
}

type ProxyAccountSummary struct {
	ParentAccountID *int64 `json:"-"`
	ID              int64
	Name            string
	Platform        string
	Type            string
	Notes           *string
}

// ProxyUsageTotals 代理名下账号在原始用量账本保留窗口内的真实请求观测。
// 请求数只能来自 usage_logs：账本只保留窗口内的原始行，因此返回值带上窗口
// 起点，调用方不得把「窗口内累计」当作历史全量。
type ProxyUsageTotals struct {
	Requests      int64
	LastRequestAt *time.Time
	WindowStart   time.Time
	WindowEnd     time.Time
}

// ProxyStats 代理的真实观测汇总：账号归属、账本请求量，以及最近一次真实
// 网络探测/质量检查的结果。未探测过的字段保持 nil，不用 0 或 100% 冒充观测。
type ProxyStats struct {
	ProxyID        int64
	TotalAccounts  int64
	ActiveAccounts int64
	Usage          *ProxyUsageTotals

	// 以下字段来自代理延迟/质量缓存（真实探测或质量检查写入）。
	AverageLatencyMs *int64
	MeasuredAt       *time.Time
	ProbeSuccess     *bool
	QualityStatus    string
	QualityScore     *int
	QualityGrade     string
	QualitySummary   string
	QualityCheckedAt *int64
}
