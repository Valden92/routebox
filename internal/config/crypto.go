package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

const (
	saltSize   = 16
	nonceSize  = 12
	keyIter    = 200_000
	keyLen     = 32
	encMagic   = "VPNR"
	encVersion = 1
)

type encryptedBlob struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}

// SaveEncrypted enables encryption at rest; the current session stays unlocked.
func (s *Store) SaveEncrypted(passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.encrypted && len(s.key) == 0 {
		return ErrLocked
	}
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	key := deriveKey(passphrase, salt)
	candidate := cloneSettings(s.settings)
	candidate.Encrypted = true
	plain, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	blob, err := encrypt(plain, key, salt)
	if err != nil {
		return err
	}
	out, err := json.Marshal(blob)
	if err != nil {
		return err
	}
	if err := writeAtomic(s.path+".enc", out); err != nil {
		return err
	}
	// The encrypted file is committed. Keep subsequent saves encrypted even if
	// removing an old plaintext copy fails; report that failure to the caller.
	s.settings, s.encrypted, s.key, s.salt = candidate, true, key, salt
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("encrypted settings saved, but removing plaintext failed: %w", err)
	}
	return nil
}

func (s *Store) LoadEncrypted(passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.encrypted || len(s.key) != 0 {
		return errors.New("config is not locked")
	}
	b, err := os.ReadFile(s.path + ".enc")
	if err != nil {
		return err
	}
	var blob encryptedBlob
	if err = json.Unmarshal(b, &blob); err != nil {
		return err
	}
	if err = validateBlob(blob); err != nil {
		return err
	}
	key := deriveKey(passphrase, blob.Salt)
	plain, err := decrypt(blob, key)
	if err != nil {
		return err
	}
	st, err := s.decodeSettings(plain, true)
	if err != nil {
		return err
	}
	s.settings, s.key, s.salt = st, key, blob.Salt
	return nil
}

func (s *Store) TryLoad() error { return s.Load() }

func deriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, keyIter, keyLen, sha256.New)
}

func encrypt(plain, key, salt []byte) (*encryptedBlob, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return &encryptedBlob{
		Version: encVersion,
		Salt:    salt,
		Nonce:   nonce,
		Data:    gcm.Seal(nil, nonce, plain, []byte(encMagic)),
	}, nil
}

func decrypt(blob encryptedBlob, key []byte) ([]byte, error) {
	if err := validateBlob(blob); err != nil {
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
	plain, err := gcm.Open(nil, blob.Nonce, blob.Data, []byte(encMagic))
	if err != nil {
		return nil, errors.New("invalid passphrase or corrupted config")
	}
	return plain, nil
}

func validateBlob(blob encryptedBlob) error {
	if blob.Version != encVersion {
		return errors.New("unsupported encrypted config version")
	}
	if len(blob.Salt) != saltSize {
		return errors.New("invalid encrypted config salt")
	}
	if len(blob.Nonce) != nonceSize {
		return errors.New("invalid encrypted config nonce")
	}
	if len(blob.Data) < 16 {
		return errors.New("invalid encrypted config data")
	}
	return nil
}

func (s *Store) Unlock(passphrase string) error {
	if err := s.LoadEncrypted(passphrase); err != nil {
		return err
	}
	return nil
}

func (s *Store) Lock(passphrase string) error {
	return s.SaveEncrypted(passphrase)
}
