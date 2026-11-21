package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReleaseModeSuppressesRouteRegistrationDebug(t *testing.T) {
	previous := gin.Mode()
	t.Cleanup(func() { gin.SetMode(previous) })
	gin.SetMode(gin.DebugMode)
	original := gin.DefaultWriter
	t.Cleanup(func() { gin.DefaultWriter = original })
	var out bytes.Buffer
	gin.DefaultWriter = &out
	configureGinMode(gin.ReleaseMode)
	gin.New().GET("/mode-check", func(c *gin.Context) { c.Status(204) })
	if strings.Contains(out.String(), "[GIN-debug]") {
		t.Fatalf("release mode printed route diagnostics: %s", out.String())
	}
}

func TestSetupReleaseModeBeforeRouteRegistration(t *testing.T) {
	previous := gin.Mode()
	t.Cleanup(func() { gin.SetMode(previous) })
	gin.SetMode(gin.DebugMode)
	configureGinMode(" RELEASE ")
	if gin.Mode() != gin.ReleaseMode {
		t.Fatalf("setup mode = %s", gin.Mode())
	}
}
