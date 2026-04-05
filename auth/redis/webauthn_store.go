package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/gosx/auth"
	goredis "github.com/redis/go-redis/v9"
)

// WebAuthnStore is a Redis-backed implementation of auth.WebAuthnStore.
type WebAuthnStore struct {
	client goredis.UniversalClient
	opts   Options
}

var _ auth.WebAuthnStore = (*WebAuthnStore)(nil)

// NewWebAuthnStore creates a Redis-backed durable WebAuthn credential store.
func NewWebAuthnStore(client goredis.UniversalClient, opts Options) *WebAuthnStore {
	return &WebAuthnStore{
		client: client,
		opts:   opts.normalized(),
	}
}

// NewWebAuthnAdapter is a compatibility alias for NewWebAuthnStore.
func NewWebAuthnAdapter(client goredis.UniversalClient) *WebAuthnStore {
	return NewWebAuthnStore(client, Options{})
}

// SaveCredential stores or replaces a credential.
func (s *WebAuthnStore) SaveCredential(credential auth.WebAuthnCredential) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("webauthn store is nil")
	}
	credential.ID = normalizeCredentialID(credential.ID)
	if credential.ID == "" {
		return auth.ErrWebAuthnCredentialNotFound
	}
	key := s.opts.webAuthnCredentialKey(credential.ID)
	ctx := context.Background()

	var previous auth.WebAuthnCredential
	payload, err := s.client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, goredis.Nil):
	case err != nil:
		return err
	default:
		previous, err = decodeCredential(payload)
		if err != nil {
			return err
		}
	}

	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, key, encoded, 0)
	if strings.TrimSpace(previous.User.ID) != "" && previous.User.ID != credential.User.ID {
		pipe.SRem(ctx, s.opts.webAuthnUserKey(previous.User.ID), credential.ID)
	}
	if strings.TrimSpace(credential.User.ID) != "" {
		pipe.SAdd(ctx, s.opts.webAuthnUserKey(credential.User.ID), credential.ID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// Credential loads a credential by ID.
func (s *WebAuthnStore) Credential(id string) (auth.WebAuthnCredential, error) {
	if s == nil || s.client == nil {
		return auth.WebAuthnCredential{}, auth.ErrWebAuthnCredentialNotFound
	}
	id = normalizeCredentialID(id)
	if id == "" {
		return auth.WebAuthnCredential{}, auth.ErrWebAuthnCredentialNotFound
	}
	payload, err := s.client.Get(context.Background(), s.opts.webAuthnCredentialKey(id)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return auth.WebAuthnCredential{}, auth.ErrWebAuthnCredentialNotFound
	}
	if err != nil {
		return auth.WebAuthnCredential{}, err
	}
	return decodeCredential(payload)
}

// Credentials loads all credentials for the provided user ID.
func (s *WebAuthnStore) Credentials(userID string) ([]auth.WebAuthnCredential, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	ids, err := s.client.SMembers(context.Background(), s.opts.webAuthnUserKey(userID)).Result()
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, s.opts.webAuthnCredentialKey(id))
	}
	values, err := s.client.MGet(context.Background(), keys...).Result()
	if err != nil {
		return nil, err
	}
	credentials := make([]auth.WebAuthnCredential, 0, len(values))
	for _, value := range values {
		payload, ok := value.(string)
		if !ok || payload == "" {
			continue
		}
		credential, err := decodeCredential([]byte(payload))
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

// UpdateCounter updates the signature counter for a credential.
func (s *WebAuthnStore) UpdateCounter(id string, signCount uint32, usedAt time.Time) error {
	if s == nil || s.client == nil {
		return auth.ErrWebAuthnCredentialNotFound
	}
	id = normalizeCredentialID(id)
	if id == "" {
		return auth.ErrWebAuthnCredentialNotFound
	}
	key := s.opts.webAuthnCredentialKey(id)
	ctx := context.Background()

	for attempt := 0; attempt < 4; attempt++ {
		err := s.client.Watch(ctx, func(tx *goredis.Tx) error {
			payload, err := tx.Get(ctx, key).Bytes()
			if errors.Is(err, goredis.Nil) {
				return auth.ErrWebAuthnCredentialNotFound
			}
			if err != nil {
				return err
			}
			credential, err := decodeCredential(payload)
			if err != nil {
				return err
			}
			credential.SignCount = signCount
			if !usedAt.IsZero() {
				credential.LastUsedAt = usedAt.UTC()
			} else {
				credential.LastUsedAt = time.Time{}
			}
			encoded, err := json.Marshal(credential)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
				pipe.Set(ctx, key, encoded, 0)
				return nil
			})
			return err
		}, key)
		if err == nil {
			return nil
		}
		if errors.Is(err, goredis.TxFailedErr) {
			continue
		}
		return err
	}
	return goredis.TxFailedErr
}

func decodeCredential(payload []byte) (auth.WebAuthnCredential, error) {
	var credential auth.WebAuthnCredential
	if err := json.Unmarshal(payload, &credential); err != nil {
		return auth.WebAuthnCredential{}, err
	}
	credential.ID = normalizeCredentialID(credential.ID)
	if credential.ID == "" {
		return auth.WebAuthnCredential{}, auth.ErrWebAuthnCredentialNotFound
	}
	return credential, nil
}
