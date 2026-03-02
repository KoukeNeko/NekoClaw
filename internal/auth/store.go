package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuthServiceName = "nekoclaw-auth"
	defaultAuthDirName     = ".nekoclaw/auth"
)

var ErrCredentialNotFound = errors.New("credential not found")
var ErrProfileNotFound = errors.New("profile not found")
var ErrKeyringUnavailable = errors.New("keyring unavailable")

type ProfileMetadata struct {
	ProfileID      string    `json:"profile_id"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	Email          string    `json:"email,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Endpoint       string    `json:"endpoint,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	DisabledUntil  time.Time `json:"disabled_until,omitempty"`
	DisabledReason string    `json:"disabled_reason,omitempty"`
}

type Credential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type StoreOptions struct {
	BaseDir string
	Keyring CredentialKeyring
}

type Store struct {
	mu            sync.Mutex
	metadataPath  string
	keyring       CredentialKeyring
	fallback      *encryptedCredentialFile
}

type metadataFile struct {
	Profiles map[string]ProfileMetadata `json:"profiles"`
}

type CredentialKeyring interface {
	Available() bool
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

func NewStore(opts StoreOptions) (*Store, error) {
	baseDir := strings.TrimSpace(opts.BaseDir)
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Join(home, defaultAuthDirName)
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, err
	}

	keyring := opts.Keyring
	if keyring == nil {
		keyring = newPlatformKeyring(defaultAuthServiceName)
	}
	fallback, err := newEncryptedCredentialFile(baseDir)
	if err != nil {
		return nil, err
	}

	store := &Store{
		metadataPath: filepath.Join(baseDir, "profiles.json"),
		keyring:      keyring,
		fallback:     fallback,
	}
	if err := store.ensureMetadataFile(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) ListProfiles(provider string) ([]ProfileMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.readMetadataLocked()
	if err != nil {
		return nil, err
	}
	provider = strings.TrimSpace(provider)
	profiles := make([]ProfileMetadata, 0, len(file.Profiles))
	for _, meta := range file.Profiles {
		if provider != "" && meta.Provider != provider {
			continue
		}
		profiles = append(profiles, meta)
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Provider != profiles[j].Provider {
			return profiles[i].Provider < profiles[j].Provider
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
	return profiles, nil
}

func (s *Store) UpsertProfile(meta ProfileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta.Provider = strings.TrimSpace(meta.Provider)
	meta.ProfileID = strings.TrimSpace(meta.ProfileID)
	if meta.Provider == "" || meta.ProfileID == "" {
		return fmt.Errorf("provider and profile_id are required")
	}

	file, err := s.readMetadataLocked()
	if err != nil {
		return err
	}
	key := profileMapKey(meta.Provider, meta.ProfileID)
	now := time.Now()
	if existing, ok := file.Profiles[key]; ok {
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = existing.CreatedAt
		}
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	file.Profiles[key] = meta
	return s.writeMetadataLocked(file)
}

func (s *Store) UpdateProfileState(provider, profileID string, update ProfileMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider = strings.TrimSpace(provider)
	profileID = strings.TrimSpace(profileID)
	if provider == "" || profileID == "" {
		return fmt.Errorf("provider and profile_id are required")
	}
	file, err := s.readMetadataLocked()
	if err != nil {
		return err
	}
	key := profileMapKey(provider, profileID)
	meta, ok := file.Profiles[key]
	if !ok {
		return ErrProfileNotFound
	}
	meta.CooldownUntil = update.CooldownUntil
	meta.DisabledUntil = update.DisabledUntil
	meta.DisabledReason = update.DisabledReason
	meta.UpdatedAt = time.Now()
	file.Profiles[key] = meta
	return s.writeMetadataLocked(file)
}

func (s *Store) GetProfile(provider, profileID string) (ProfileMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.readMetadataLocked()
	if err != nil {
		return ProfileMetadata{}, err
	}
	meta, ok := file.Profiles[profileMapKey(provider, profileID)]
	if !ok {
		return ProfileMetadata{}, ErrProfileNotFound
	}
	return meta, nil
}

func (s *Store) SaveCredential(provider, profileID string, credential Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider = strings.TrimSpace(provider)
	profileID = strings.TrimSpace(profileID)
	if provider == "" || profileID == "" {
		return fmt.Errorf("provider and profile_id are required")
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return fmt.Errorf("access token is required")
	}

	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	secretKey := secretStorageKey(provider, profileID)

	if s.keyring != nil && s.keyring.Available() {
		if err := s.keyring.Set(secretKey, string(raw)); err == nil {
			return nil
		}
	}
	return s.fallback.Set(secretKey, raw)
}

func (s *Store) LoadCredential(provider, profileID string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secretKey := secretStorageKey(provider, profileID)
	if s.keyring != nil && s.keyring.Available() {
		if value, err := s.keyring.Get(secretKey); err == nil {
			var credential Credential
			if err := json.Unmarshal([]byte(value), &credential); err == nil {
				return credential, nil
			}
		}
	}
	value, err := s.fallback.Get(secretKey)
	if err != nil {
		return Credential{}, ErrCredentialNotFound
	}
	var credential Credential
	if err := json.Unmarshal(value, &credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (s *Store) DeleteCredential(provider, profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secretKey := secretStorageKey(provider, profileID)
	if s.keyring != nil && s.keyring.Available() {
		_ = s.keyring.Delete(secretKey)
	}
	_ = s.fallback.Delete(secretKey)
	return nil
}

func (s *Store) ensureMetadataFile() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := os.Stat(s.metadataPath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seed := metadataFile{Profiles: map[string]ProfileMetadata{}}
	return s.writeMetadataLocked(seed)
}

func (s *Store) readMetadataLocked() (metadataFile, error) {
	content, err := os.ReadFile(s.metadataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metadataFile{Profiles: map[string]ProfileMetadata{}}, nil
		}
		return metadataFile{}, err
	}
	var file metadataFile
	if len(content) == 0 {
		file.Profiles = map[string]ProfileMetadata{}
		return file, nil
	}
	if err := json.Unmarshal(content, &file); err != nil {
		return metadataFile{}, err
	}
	if file.Profiles == nil {
		file.Profiles = map[string]ProfileMetadata{}
	}
	return file, nil
}

func (s *Store) writeMetadataLocked(file metadataFile) error {
	if file.Profiles == nil {
		file.Profiles = map[string]ProfileMetadata{}
	}
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.metadataPath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.metadataPath)
}

func profileMapKey(provider, profileID string) string {
	return strings.TrimSpace(provider) + ":" + strings.TrimSpace(profileID)
}

func secretStorageKey(provider, profileID string) string {
	return "nekoclaw.auth." + profileMapKey(provider, profileID)
}

type encryptedCredentialFile struct {
	filePath string
	keyPath  string
}

type encryptedMap struct {
	Items map[string]string `json:"items"`
}

func newEncryptedCredentialFile(baseDir string) (*encryptedCredentialFile, error) {
	file := &encryptedCredentialFile{
		filePath: filepath.Join(baseDir, "credentials.enc.json"),
		keyPath:  filepath.Join(baseDir, "master.key"),
	}
	if _, err := file.loadMasterKey(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(file.filePath); err != nil && errors.Is(err, os.ErrNotExist) {
		seed := encryptedMap{Items: map[string]string{}}
		payload, _ := json.Marshal(seed)
		if err := os.WriteFile(file.filePath, payload, 0o600); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func (f *encryptedCredentialFile) Set(key string, value []byte) error {
	master, err := f.loadMasterKey()
	if err != nil {
		return err
	}
	items, err := f.readMap()
	if err != nil {
		return err
	}
	cipherText, err := encrypt(master, value)
	if err != nil {
		return err
	}
	items[key] = cipherText
	return f.writeMap(items)
}

func (f *encryptedCredentialFile) Get(key string) ([]byte, error) {
	master, err := f.loadMasterKey()
	if err != nil {
		return nil, err
	}
	items, err := f.readMap()
	if err != nil {
		return nil, err
	}
	cipherText, ok := items[key]
	if !ok {
		return nil, ErrCredentialNotFound
	}
	return decrypt(master, cipherText)
}

func (f *encryptedCredentialFile) Delete(key string) error {
	items, err := f.readMap()
	if err != nil {
		return err
	}
	delete(items, key)
	return f.writeMap(items)
}

func (f *encryptedCredentialFile) readMap() (map[string]string, error) {
	content, err := os.ReadFile(f.filePath)
	if err != nil {
		return nil, err
	}
	var payload encryptedMap
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, err
	}
	if payload.Items == nil {
		payload.Items = map[string]string{}
	}
	return payload.Items, nil
}

func (f *encryptedCredentialFile) writeMap(items map[string]string) error {
	payload, err := json.Marshal(encryptedMap{Items: items})
	if err != nil {
		return err
	}
	tmp := f.filePath + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.filePath)
}

func (f *encryptedCredentialFile) loadMasterKey() ([]byte, error) {
	if keyData, err := os.ReadFile(f.keyPath); err == nil {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
		if err != nil {
			return nil, err
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("invalid master key length")
		}
		return decoded, nil
	}
	if err := os.MkdirAll(filepath.Dir(f.keyPath), 0o700); err != nil {
		return nil, err
	}
	master := make([]byte, 32)
	if _, err := rand.Read(master); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(master)
	if err := os.WriteFile(f.keyPath, []byte(encoded), 0o600); err != nil {
		return nil, err
	}
	return master, nil
}

func encrypt(key []byte, plain []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	cipherText := gcm.Seal(nil, nonce, plain, nil)
	packed := append(nonce, cipherText...)
	return base64.StdEncoding.EncodeToString(packed), nil
}

func decrypt(key []byte, encoded string) ([]byte, error) {
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(blob) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := blob[:nonceSize]
	cipherText := blob[nonceSize:]
	return gcm.Open(nil, nonce, cipherText, nil)
}

type platformKeyring struct {
	service string
	impl    keyringImpl
}

type keyringImpl interface {
	Available() bool
	Set(service, key, value string) error
	Get(service, key string) (string, error)
	Delete(service, key string) error
}

func newPlatformKeyring(service string) CredentialKeyring {
	var impl keyringImpl
	switch runtime.GOOS {
	case "darwin":
		impl = macOSKeyring{}
	case "linux":
		impl = linuxSecretToolKeyring{}
	default:
		impl = unsupportedKeyring{}
	}
	return &platformKeyring{service: service, impl: impl}
}

func (k *platformKeyring) Available() bool {
	return k.impl != nil && k.impl.Available()
}

func (k *platformKeyring) Set(key, value string) error {
	if !k.Available() {
		return ErrKeyringUnavailable
	}
	return k.impl.Set(k.service, key, value)
}

func (k *platformKeyring) Get(key string) (string, error) {
	if !k.Available() {
		return "", ErrKeyringUnavailable
	}
	return k.impl.Get(k.service, key)
}

func (k *platformKeyring) Delete(key string) error {
	if !k.Available() {
		return ErrKeyringUnavailable
	}
	return k.impl.Delete(k.service, key)
}

type macOSKeyring struct{}

func (macOSKeyring) Available() bool {
	_, err := exec.LookPath("security")
	return err == nil
}

func (macOSKeyring) Set(service, key, value string) error {
	cmd := exec.Command("security", "add-generic-password", "-a", key, "-s", service, "-w", value, "-U")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mac keychain set failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (macOSKeyring) Get(service, key string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-a", key, "-s", service, "-w")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mac keychain get failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (macOSKeyring) Delete(service, key string) error {
	cmd := exec.Command("security", "delete-generic-password", "-a", key, "-s", service)
	_, _ = cmd.CombinedOutput()
	return nil
}

type linuxSecretToolKeyring struct{}

func (linuxSecretToolKeyring) Available() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

func (linuxSecretToolKeyring) Set(service, key, value string) error {
	cmd := exec.Command("secret-tool", "store", "--label", "NekoClaw OAuth", "service", service, "account", key)
	cmd.Stdin = strings.NewReader(value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("linux keyring set failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (linuxSecretToolKeyring) Get(service, key string) (string, error) {
	cmd := exec.Command("secret-tool", "lookup", "service", service, "account", key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("linux keyring get failed: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (linuxSecretToolKeyring) Delete(service, key string) error {
	cmd := exec.Command("secret-tool", "clear", "service", service, "account", key)
	_, _ = cmd.CombinedOutput()
	return nil
}

type unsupportedKeyring struct{}

func (unsupportedKeyring) Available() bool                                 { return false }
func (unsupportedKeyring) Set(_, _, _ string) error                        { return ErrKeyringUnavailable }
func (unsupportedKeyring) Get(_, _ string) (string, error)                 { return "", ErrKeyringUnavailable }
func (unsupportedKeyring) Delete(_, _ string) error                         { return ErrKeyringUnavailable }
