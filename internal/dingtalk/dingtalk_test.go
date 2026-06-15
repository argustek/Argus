package dingtalk

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMin(t *testing.T) {
	assert.Equal(t, 1, min(1, 2))
	assert.Equal(t, 1, min(2, 1))
	assert.Equal(t, 5, min(5, 5))
	assert.Equal(t, -3, min(-3, 0))
}

func TestSetLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	SetLogDir(tmpDir)
	assert.Equal(t, tmpDir, logDir)
}

func TestLogToFile(t *testing.T) {
	tmpDir := t.TempDir()
	SetLogDir(tmpDir)

	logToFile("test message")

	logPath := filepath.Join(tmpDir, "dingtalk.log")
	_, err := os.Stat(logPath)
	assert.NoError(t, err, "log file should exist")

	data, err := os.ReadFile(logPath)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "test message")
}

func TestStreamConfigDefault(t *testing.T) {
	cfg := &StreamConfig{
		Enabled:      false,
		ClientID:     "",
		ClientSecret: "",
	}
	assert.False(t, cfg.Enabled)
	assert.Empty(t, cfg.ClientID)
}

func TestStreamConfigSetAndGet(t *testing.T) {
	cfg := &StreamConfig{
		Enabled:      true,
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	}
	SetStreamConfig(cfg)

	retrieved := getStreamConfig()
	assert.NotNil(t, retrieved)
	assert.True(t, retrieved.Enabled)
	assert.Equal(t, "test-id", retrieved.ClientID)
}

func TestGetLastSenderID_Default(t *testing.T) {
	id := GetLastSenderID()
	_ = id
}

func TestAccessTokenCache(t *testing.T) {
	tokenCache = &accessTokenCache{
		token:     "cached-token",
		expiresAt: time.Now().Add(1 * time.Hour),
	}

	assert.NotNil(t, tokenCache)
	assert.Equal(t, "cached-token", tokenCache.token)
	assert.True(t, time.Now().Before(tokenCache.expiresAt))
}

func TestAccessTokenCacheExpired(t *testing.T) {
	tokenCache = &accessTokenCache{
		token:     "expired-token",
		expiresAt: time.Now().Add(-1 * time.Hour),
	}

	assert.NotNil(t, tokenCache)
	assert.True(t, time.Now().After(tokenCache.expiresAt))
}

func TestInitStreamDisabled(t *testing.T) {
	cfg := StreamConfig{
		Enabled: false,
	}
	handler := func(content string, sender string) {}

	InitStream(cfg, handler)

	assert.Nil(t, streamClient)
}

func TestStopStreamWithoutStart(t *testing.T) {
	streamClient = nil
	streamCancel = nil
	StopStream()
}

func TestGetStreamConfigNil(t *testing.T) {
	streamConfig = nil
	cfg := getStreamConfig()
	assert.Nil(t, cfg)
}
