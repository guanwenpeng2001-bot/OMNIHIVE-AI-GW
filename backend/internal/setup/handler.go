package setup

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/sysutil"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/logredact"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// installMutex prevents concurrent installation attempts (TOCTOU protection)
var installMutex sync.Mutex

// RegisterRoutes registers setup wizard routes
func RegisterRoutes(r *gin.Engine) {
	if NeedsSetup() {
		if err := initializeBootstrapToken(); err != nil {
			slog.Error("cannot initialize setup bootstrap token", "error", err)
		}
	}
	setup := r.Group("/setup")
	{
		// Status endpoint is always accessible (read-only)
		setup.GET("/status", getStatus)

		// All modification endpoints are protected by setupGuard
		protected := setup.Group("")
		protected.Use(setupGuard())
		{
			protected.POST("/test-db", testDatabase)
			protected.POST("/test-redis", testRedis)
			protected.POST("/install", install)
		}
	}
}

// SetupStatus represents the current setup state
type SetupStatus struct {
	NeedsSetup bool   `json:"needs_setup"`
	Step       string `json:"step"`
}

// getStatus returns the current setup status
func getStatus(c *gin.Context) {
	response.Success(c, SetupStatus{
		NeedsSetup: NeedsSetup(),
		Step:       "welcome",
	})
}

// setupGuard middleware ensures setup endpoints are only accessible during setup mode
func setupGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !NeedsSetup() {
			response.Error(c, http.StatusForbidden, "Setup is not allowed: system is already installed")
			c.Abort()
			return
		}
		if !consumeBootstrapToken(c) {
			return
		}
		c.Next()
	}
}

// validateHostname checks if a hostname/IP is safe (no injection characters)
func validateHostname(host string) bool {
	// Allow only alphanumeric, dots, hyphens, and colons (for IPv6)
	validHost := regexp.MustCompile(`^[a-zA-Z0-9.\-:]+$`)
	return validHost.MatchString(host) && len(host) <= 253
}

// validateDBName checks if database name is safe
func validateDBName(name string) bool {
	// Allow only alphanumeric and underscores, starting with letter
	validName := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	return validName.MatchString(name) && len(name) <= 63
}

// validateUsername checks if username is safe
func validateUsername(name string) bool {
	// Allow only alphanumeric and underscores
	validName := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	return validName.MatchString(name) && len(name) <= 63
}

// validateEmail checks if email format is valid
func validateEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil && len(email) <= 254
}

// validatePassword checks password strength
func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}
	return nil
}

// validatePort checks if port is in valid range
func validatePort(port int) bool {
	return port > 0 && port <= 65535
}

// validateSSLMode checks if SSL mode is valid
func validateSSLMode(mode string) bool {
	validModes := map[string]bool{
		"disable": true, "require": true, "verify-ca": true, "verify-full": true,
	}
	return validModes[mode]
}

// TestDatabaseRequest represents database test request
type TestDatabaseRequest struct {
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	User     string `json:"user" binding:"required"`
	Password string `json:"password"`
	DBName   string `json:"dbname" binding:"required"`
	SSLMode  string `json:"sslmode"`
}

const maxSetupRequestBytes = 64 << 10

func bindSetupRequest(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSetupRequestBytes)
	data, err := io.ReadAll(c.Request.Body)
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		response.Error(c, http.StatusRequestEntityTooLarge, "Setup request is too large")
		return false
	}
	if err == nil {
		err = json.Unmarshal(data, dst)
	}
	if err == nil {
		err = binding.Validator.ValidateStruct(dst)
	}
	if err != nil {
		slog.Warn("setup request validation failed", "error", logredact.RedactDiagnostic(err.Error()))
		response.Error(c, http.StatusBadRequest, "Invalid request")
		return false
	}
	return true
}

// testDatabase tests database connection
func testDatabase(c *gin.Context) {
	var req TestDatabaseRequest
	if !bindSetupRequest(c, &req) {
		return
	}

	// Security: Validate all inputs to prevent injection attacks
	if !validateHostname(req.Host) {
		response.Error(c, http.StatusBadRequest, "Invalid hostname format")
		return
	}
	if !validatePort(req.Port) {
		response.Error(c, http.StatusBadRequest, "Invalid port number")
		return
	}
	if !validateUsername(req.User) {
		response.Error(c, http.StatusBadRequest, "Invalid username format")
		return
	}
	if !validateDBName(req.DBName) {
		response.Error(c, http.StatusBadRequest, "Invalid database name format")
		return
	}

	if req.SSLMode == "" {
		req.SSLMode = "disable"
	}
	if !validateSSLMode(req.SSLMode) {
		response.Error(c, http.StatusBadRequest, "Invalid SSL mode")
		return
	}

	cfg := &DatabaseConfig{
		Host:     req.Host,
		Port:     req.Port,
		User:     req.User,
		Password: req.Password,
		DBName:   req.DBName,
		SSLMode:  req.SSLMode,
	}

	if err := TestDatabaseConnection(cfg); err != nil {
		response.Error(c, http.StatusBadRequest, "Connection failed")
		return
	}

	response.Success(c, gin.H{"message": "Connection successful"})
}

// TestRedisRequest represents Redis test request
type TestRedisRequest struct {
	Host      string `json:"host" binding:"required"`
	Port      int    `json:"port" binding:"required"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	DB        int    `json:"db"`
	EnableTLS bool   `json:"enable_tls"`
}

// testRedis tests Redis connection
func testRedis(c *gin.Context) {
	var req TestRedisRequest
	if !bindSetupRequest(c, &req) {
		return
	}

	// Security: Validate inputs
	if !validateHostname(req.Host) {
		response.Error(c, http.StatusBadRequest, "Invalid hostname format")
		return
	}
	if !validatePort(req.Port) {
		response.Error(c, http.StatusBadRequest, "Invalid port number")
		return
	}
	if req.DB < 0 || req.DB > 15 {
		response.Error(c, http.StatusBadRequest, "Invalid Redis database number (0-15)")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) > 128 {
		response.Error(c, http.StatusBadRequest, "Invalid Redis username")
		return
	}

	cfg := &RedisConfig{
		Host:      req.Host,
		Port:      req.Port,
		Username:  req.Username,
		Password:  req.Password,
		DB:        req.DB,
		EnableTLS: req.EnableTLS,
	}

	if err := TestRedisConnection(cfg); err != nil {
		response.Error(c, http.StatusBadRequest, "Connection failed")
		return
	}

	response.Success(c, gin.H{"message": "Connection successful"})
}

// InstallRequest represents installation request
type InstallRequest struct {
	Database DatabaseConfig `json:"database" binding:"required"`
	Redis    RedisConfig    `json:"redis" binding:"required"`
	Admin    AdminConfig    `json:"admin" binding:"required"`
	Server   ServerConfig   `json:"server"`
}

// install performs the installation
func install(c *gin.Context) {
	var req InstallRequest
	if !bindSetupRequest(c, &req) {
		return
	}

	req.Admin.Email = strings.TrimSpace(req.Admin.Email)
	req.Database.Host = strings.TrimSpace(req.Database.Host)
	req.Database.User = strings.TrimSpace(req.Database.User)
	req.Database.DBName = strings.TrimSpace(req.Database.DBName)
	req.Redis.Host = strings.TrimSpace(req.Redis.Host)
	req.Redis.Username = strings.TrimSpace(req.Redis.Username)

	// ========== COMPREHENSIVE INPUT VALIDATION ==========
	// Database validation
	if !validateHostname(req.Database.Host) {
		response.Error(c, http.StatusBadRequest, "Invalid database hostname")
		return
	}
	if !validatePort(req.Database.Port) {
		response.Error(c, http.StatusBadRequest, "Invalid database port")
		return
	}
	if !validateUsername(req.Database.User) {
		response.Error(c, http.StatusBadRequest, "Invalid database username")
		return
	}
	if !validateDBName(req.Database.DBName) {
		response.Error(c, http.StatusBadRequest, "Invalid database name")
		return
	}

	// Redis validation
	if !validateHostname(req.Redis.Host) {
		response.Error(c, http.StatusBadRequest, "Invalid Redis hostname")
		return
	}
	if !validatePort(req.Redis.Port) {
		response.Error(c, http.StatusBadRequest, "Invalid Redis port")
		return
	}
	if req.Redis.DB < 0 || req.Redis.DB > 15 {
		response.Error(c, http.StatusBadRequest, "Invalid Redis database number")
		return
	}
	if len(req.Redis.Username) > 128 {
		response.Error(c, http.StatusBadRequest, "Invalid Redis username")
		return
	}

	// Admin validation
	if !validateEmail(req.Admin.Email) {
		response.Error(c, http.StatusBadRequest, "Invalid admin email format")
		return
	}
	if err := validatePassword(req.Admin.Password); err != nil {
		slog.Warn("setup administrator password validation failed", "error", logredact.RedactDiagnostic(err.Error()))
		response.Error(c, http.StatusBadRequest, "Invalid administrator password")
		return
	}

	// Server validation
	if req.Server.Port != 0 && !validatePort(req.Server.Port) {
		response.Error(c, http.StatusBadRequest, "Invalid server port")
		return
	}

	// ========== SET DEFAULTS ==========
	if req.Database.SSLMode == "" {
		req.Database.SSLMode = "disable"
	}
	if !validateSSLMode(req.Database.SSLMode) {
		response.Error(c, http.StatusBadRequest, "Invalid SSL mode")
		return
	}
	if req.Server.Host == "" {
		req.Server.Host = "0.0.0.0"
	}
	if req.Server.Port == 0 {
		req.Server.Port = 8080
	}
	if req.Server.Mode == "" {
		req.Server.Mode = "release"
	}
	// Validate server mode
	if req.Server.Mode != "release" && req.Server.Mode != "debug" {
		response.Error(c, http.StatusBadRequest, "Invalid server mode (must be 'release' or 'debug')")
		return
	}

	cfg := &SetupConfig{
		Database: req.Database,
		Redis:    req.Redis,
		Admin:    req.Admin,
		Server:   req.Server,
		JWT: JWTConfig{
			ExpireHour: 24,
		},
	}

	installMutex.Lock()
	defer installMutex.Unlock()
	if !NeedsSetup() {
		response.Error(c, http.StatusForbidden, "Setup is not allowed: system is already installed")
		return
	}

	if err := Install(cfg); err != nil {
		response.Error(c, http.StatusInternalServerError, "Installation failed")
		return
	}

	// Schedule service restart in background after sending response
	// This ensures the client receives the success response before the service restarts
	go func() {
		// Wait a moment to ensure the response is sent
		time.Sleep(500 * time.Millisecond)
		sysutil.RestartServiceAsync()
	}()

	response.Success(c, gin.H{
		"message": "Installation completed successfully. Service will restart automatically.",
		"restart": true,
	})
}

// The first credential is delivered only through the local data directory.
// A successful rotation renews the idle window within the total setup window.
const (
	bootstrapIdleWindow = 2 * time.Hour
	bootstrapMaxWindow  = 12 * time.Hour
)

var bootstrapState struct {
	sync.Mutex
	digest    [32]byte
	expiresAt time.Time
	deadline  time.Time
	path      string
}

func initializeBootstrapToken() error {
	bootstrapState.Lock()
	defer bootstrapState.Unlock()
	token, err := newBootstrapToken()
	if err != nil {
		return err
	}
	tokenPath := filepath.Join(GetDataDir(), "setup-bootstrap-token")
	if err := writeBootstrapToken(tokenPath, token); err != nil {
		return err
	}
	bootstrapState.digest = sha256.Sum256([]byte(token))
	now := time.Now()
	bootstrapState.expiresAt = now.Add(bootstrapIdleWindow)
	bootstrapState.deadline = now.Add(bootstrapMaxWindow)
	bootstrapState.path = tokenPath
	slog.Info("setup bootstrap token written to local file", "path", tokenPath)
	return nil
}

func newBootstrapToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func writeBootstrapToken(path, token string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".setup-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.WriteString(f, token+"\n")
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func consumeBootstrapToken(c *gin.Context) bool {
	bootstrapState.Lock()
	defer bootstrapState.Unlock()
	supplied := strings.TrimSpace(c.GetHeader("X-Setup-Bootstrap-Token"))
	digest := sha256.Sum256([]byte(supplied))
	now := time.Now()
	if supplied == "" || subtle.ConstantTimeCompare(digest[:], bootstrapState.digest[:]) != 1 {
		response.Error(c, http.StatusUnauthorized, "Valid local setup bootstrap token required")
		c.Abort()
		return false
	}
	if !now.Before(bootstrapState.expiresAt) {
		response.Error(c, http.StatusUnauthorized, "Local setup bootstrap token expired. Restart setup to issue a new token.")
		c.Abort()
		return false
	}
	nextToken, err := newBootstrapToken()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Setup authorization unavailable")
		c.Abort()
		return false
	}
	if err := writeBootstrapToken(bootstrapState.path, nextToken); err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Setup authorization unavailable")
		c.Abort()
		return false
	}
	bootstrapState.digest = sha256.Sum256([]byte(nextToken))
	bootstrapState.expiresAt = now.Add(bootstrapIdleWindow)
	if bootstrapState.deadline.Before(bootstrapState.expiresAt) {
		bootstrapState.expiresAt = bootstrapState.deadline
	}
	c.Header("X-Setup-Bootstrap-Token", nextToken)
	c.Header("Cache-Control", "no-store")
	return true
}
