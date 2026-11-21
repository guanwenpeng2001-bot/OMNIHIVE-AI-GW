package handler

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"strconv"
	"sync"
	"time"
)

var (
	ErrMCPGrantNotFound = service.ErrMCPGrantNotFound
	timeNow             = time.Now
)

type memoryMCPGrantStore struct {
	mu      sync.Mutex
	grants  map[int64][]MCPGrantDTO
	clients map[string]string
}

func newMemoryMCPGrantStore() *memoryMCPGrantStore {
	return &memoryMCPGrantStore{grants: map[int64][]MCPGrantDTO{}, clients: map[string]string{}}
}

func (store *memoryMCPGrantStore) List(_ context.Context, userID int64) ([]MCPGrantDTO, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]MCPGrantDTO(nil), store.grants[userID]...), nil
}

func (store *memoryMCPGrantStore) Create(_ context.Context, userID int64, grant MCPGrantDTO) (MCPGrantDTO, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.grants[userID] = append(store.grants[userID], grant)
	return grant, nil
}

func (store *memoryMCPGrantStore) Revoke(_ context.Context, userID int64, id string) (MCPGrantDTO, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.grants[userID] {
		if store.grants[userID][index].ID == id {
			store.grants[userID][index].Status = "revoked"
			return store.grants[userID][index], nil
		}
	}
	return MCPGrantDTO{}, ErrMCPGrantNotFound
}

func (store *memoryMCPGrantStore) Active(_ context.Context, userID int64, id string) (MCPGrantDTO, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, grant := range store.grants[userID] {
		if grant.ID == id && grant.Status == "active" && grant.ExpiresAt.After(timeNow()) {
			return grant, true, nil
		}
	}
	return MCPGrantDTO{}, false, nil
}

func (store *memoryMCPGrantStore) RegisterClient(_ context.Context, userID int64, clientID, name, redirectURI string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.clients[clientKey(userID, clientID)] = redirectURI
	return nil
}

func (store *memoryMCPGrantStore) ClientMatches(_ context.Context, userID int64, clientID, redirectURI string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	registered, ok := store.clients[clientKey(userID, clientID)]
	return ok && registered == redirectURI, nil
}

func clientKey(userID int64, clientID string) string {
	return strconv.FormatInt(userID, 10) + ":" + clientID
}
