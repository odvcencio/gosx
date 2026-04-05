package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/gosx/server"
	goredis "github.com/redis/go-redis/v9"
)

var releaseLockScript = goredis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// ISRStore is a Redis-backed implementation of server.ISRStore.
type ISRStore struct {
	client goredis.UniversalClient
	opts   Options
}

var _ server.ISRStore = (*ISRStore)(nil)

// NewISRStore creates a Redis-backed ISR store.
func NewISRStore(client goredis.UniversalClient, opts Options) *ISRStore {
	return &ISRStore{
		client: client,
		opts:   opts.normalized(),
	}
}

// NewISRAdapter is a compatibility alias for NewISRStore.
func NewISRAdapter(client goredis.UniversalClient) *ISRStore {
	return NewISRStore(client, Options{})
}

// StatArtifact returns the modification time for a stored ISR artifact.
func (s *ISRStore) StatArtifact(staticDir, pagePath, file string) (server.ISRArtifactInfo, error) {
	if s == nil || s.client == nil {
		return server.ISRArtifactInfo{}, errors.New("redis client is nil")
	}
	value, err := s.client.Get(context.Background(), s.opts.isrArtifactMetaKey(staticDir, pagePath, file)).Result()
	if errors.Is(err, goredis.Nil) {
		return server.ISRArtifactInfo{}, server.ErrISRArtifactNotFound
	}
	if err != nil {
		return server.ISRArtifactInfo{}, err
	}
	modTime, err := parseUnixNano(value)
	if err != nil {
		return server.ISRArtifactInfo{}, err
	}
	return server.ISRArtifactInfo{ModTime: modTime}, nil
}

// ReadArtifact returns the stored artifact body and modification time.
func (s *ISRStore) ReadArtifact(staticDir, pagePath, file string) (server.ISRArtifact, error) {
	if s == nil || s.client == nil {
		return server.ISRArtifact{}, errors.New("redis client is nil")
	}
	bodyKey := s.opts.isrArtifactBodyKey(staticDir, pagePath, file)
	metaKey := s.opts.isrArtifactMetaKey(staticDir, pagePath, file)
	values, err := s.client.MGet(context.Background(), bodyKey, metaKey).Result()
	if err != nil {
		return server.ISRArtifact{}, err
	}
	if len(values) != 2 || values[0] == nil || values[1] == nil {
		return server.ISRArtifact{}, server.ErrISRArtifactNotFound
	}
	body, ok := values[0].(string)
	if !ok {
		return server.ISRArtifact{}, server.ErrISRArtifactNotFound
	}
	modValue, ok := values[1].(string)
	if !ok {
		return server.ISRArtifact{}, server.ErrISRArtifactNotFound
	}
	modTime, err := parseUnixNano(modValue)
	if err != nil {
		return server.ISRArtifact{}, err
	}
	return server.ISRArtifact{
		Body:    []byte(body),
		ModTime: modTime,
	}, nil
}

// WriteArtifact stores an ISR artifact body in Redis.
func (s *ISRStore) WriteArtifact(staticDir, pagePath, file string, body []byte) (server.ISRArtifactInfo, error) {
	if s == nil || s.client == nil {
		return server.ISRArtifactInfo{}, errors.New("redis client is nil")
	}
	modTime := time.Now().UTC()
	bodyKey := s.opts.isrArtifactBodyKey(staticDir, pagePath, file)
	metaKey := s.opts.isrArtifactMetaKey(staticDir, pagePath, file)
	pipe := s.client.TxPipeline()
	pipe.Set(context.Background(), bodyKey, body, s.opts.ArtifactTTL)
	pipe.Set(context.Background(), metaKey, strconv.FormatInt(modTime.UnixNano(), 10), s.opts.ArtifactTTL)
	if _, err := pipe.Exec(context.Background()); err != nil {
		return server.ISRArtifactInfo{}, err
	}
	return server.ISRArtifactInfo{ModTime: modTime}, nil
}

// LoadState returns freshness state for a page, initializing it when absent.
func (s *ISRStore) LoadState(bundleRoot, pagePath string, fallbackGeneratedAt time.Time) (server.ISRPageState, error) {
	if s == nil || s.client == nil {
		return server.ISRPageState{}, errors.New("redis client is nil")
	}
	key := s.opts.isrStateKey(bundleRoot, pagePath)
	payload, err := s.client.Get(context.Background(), key).Bytes()
	if err == nil {
		return decodeState(payload)
	}
	if !errors.Is(err, goredis.Nil) {
		return server.ISRPageState{}, err
	}
	if fallbackGeneratedAt.IsZero() {
		fallbackGeneratedAt = time.Now().UTC()
	}
	state := server.ISRPageState{
		GeneratedAt: fallbackGeneratedAt.UTC(),
		TagVersions: map[string]uint64{},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return server.ISRPageState{}, err
	}
	created, err := s.client.SetNX(context.Background(), key, encoded, s.opts.StateTTL).Result()
	if err != nil {
		return server.ISRPageState{}, err
	}
	if created {
		return state, nil
	}
	payload, err = s.client.Get(context.Background(), key).Bytes()
	if err != nil {
		return server.ISRPageState{}, err
	}
	return decodeState(payload)
}

// SaveState persists freshness state for a page.
func (s *ISRStore) SaveState(bundleRoot, pagePath string, state server.ISRPageState) error {
	if s == nil || s.client == nil {
		return errors.New("redis client is nil")
	}
	encoded, err := json.Marshal(cloneState(state))
	if err != nil {
		return err
	}
	return s.client.Set(context.Background(), s.opts.isrStateKey(bundleRoot, pagePath), encoded, s.opts.StateTTL).Err()
}

// AcquireRefresh acquires a distributed refresh lease for the page.
func (s *ISRStore) AcquireRefresh(bundleRoot, pagePath string) (server.ISRRefreshLease, bool, error) {
	if s == nil || s.client == nil {
		return nil, false, errors.New("redis client is nil")
	}
	token, err := randomToken()
	if err != nil {
		return nil, false, err
	}
	key := s.opts.isrLockKey(bundleRoot, pagePath)
	acquired, err := s.client.SetNX(context.Background(), key, token, s.opts.LockTTL).Result()
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return &refreshLease{
		client: s.client,
		key:    key,
		token:  token,
	}, true, nil
}

type refreshLease struct {
	client goredis.UniversalClient
	key    string
	token  string
}

func (l *refreshLease) Release() error {
	if l == nil || l.client == nil || strings.TrimSpace(l.key) == "" || l.token == "" {
		return nil
	}
	return releaseLockScript.Run(context.Background(), l.client, []string{l.key}, l.token).Err()
}

func decodeState(payload []byte) (server.ISRPageState, error) {
	var state server.ISRPageState
	if err := json.Unmarshal(payload, &state); err != nil {
		return server.ISRPageState{}, err
	}
	return cloneState(state), nil
}

func cloneState(state server.ISRPageState) server.ISRPageState {
	cloned := server.ISRPageState{
		GeneratedAt: state.GeneratedAt.UTC(),
		PathVersion: state.PathVersion,
		TagVersions: make(map[string]uint64, len(state.TagVersions)),
	}
	for tag, version := range state.TagVersions {
		cloned.TagVersions[tag] = version
	}
	return cloned
}

func parseUnixNano(value string) (time.Time, error) {
	nanos, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nanos).UTC(), nil
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
