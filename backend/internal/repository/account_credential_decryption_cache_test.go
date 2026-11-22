//go:build unit

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

type countingCredentialDecryptor struct {
	service.SecretEncryptor
	decrypts atomic.Int32
}

func (e *countingCredentialDecryptor) Decrypt(ciphertext string) (string, error) {
	e.decrypts.Add(1)
	return e.SecretEncryptor.Decrypt(ciphertext)
}

func credentialCacheCiphertext(t *testing.T, encryptor service.SecretEncryptor, metadata service.CredentialSecret, token string) string {
	t.Helper()
	payload := fmt.Sprintf(`{"workspace_id":%d,"account_id":%d,"reference":%q,"version":%d,"credentials":{"access_token":%q,"nested":{"safe":true}}}`,
		metadata.WorkspaceID, metadata.AccountID, metadata.Reference, metadata.Version, token)
	ciphertext, err := encryptor.Encrypt(payload)
	require.NoError(t, err)
	return ciphertext
}

func TestCredentialDecryptionCacheConcurrentCopiesAndCiphertextInvalidation(t *testing.T) {
	base := aesEncryptor(t)
	encryptor := &countingCredentialDecryptor{SecretEncryptor: base}
	metadata := service.CredentialSecret{
		AccountID:   73,
		WorkspaceID: 9,
		Reference:   "cache-reference",
		Version:     4,
		UpdatedAt:   time.Now().UTC(),
	}
	repo := &accountRepository{
		credentialEncryptor:       encryptor,
		credentialDecryptionCache: newCredentialDecryptionCache(),
	}
	ciphertext := credentialCacheCiphertext(t, base, metadata, "first-token")

	first, err := repo.resolveStoredCredentialSecret(context.Background(), metadata, ciphertext)
	require.NoError(t, err)
	first["access_token"] = "mutated-by-first-caller"
	first["nested"].(map[string]any)["safe"] = false

	const readers = 48
	start := make(chan struct{})
	errCh := make(chan error, readers)
	var readersDone sync.WaitGroup
	readersDone.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer readersDone.Done()
			<-start
			credentials, err := repo.resolveStoredCredentialSecret(context.Background(), metadata, ciphertext)
			if err != nil {
				errCh <- err
				return
			}
			if credentials["access_token"] != "first-token" || !credentials["nested"].(map[string]any)["safe"].(bool) {
				errCh <- fmt.Errorf("cache leaked another caller's map: %#v", credentials)
				return
			}
			credentials["access_token"] = "private"
			credentials["nested"].(map[string]any)["safe"] = false
		}()
	}
	close(start)
	readersDone.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, encryptor.decrypts.Load(), "all unchanged reads must use the cached decrypted copy")

	rotatedCiphertext := credentialCacheCiphertext(t, base, metadata, "rotated-token")
	rotated, err := repo.resolveStoredCredentialSecret(context.Background(), metadata, rotatedCiphertext)
	require.NoError(t, err)
	require.Equal(t, "rotated-token", rotated["access_token"], "ciphertext change must invalidate immediately")
	require.EqualValues(t, 2, encryptor.decrypts.Load())
}
