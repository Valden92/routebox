package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

func (s *Store) SaveEncrypted(passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase required")
	}
	s.mu.RLock()
	plain, err := json.Marshal(s.settings)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	blob, err := encrypt(plain, passphrase)
	if err != nil {
		return err
	}
	out, err := json.Marshal(blob)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.settings.Encrypted = true
	s.mu.Unlock()
	encPath := s.path + ".enc"
	if err := os.WriteFile(encPath, out, 0o600); err != nil {
		return err
	}
	_ = os.Remove(s.path)
	s.path = encPath
	return nil
}

func (s *Store) LoadEncrypted(passphrase string) error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var blob encryptedBlob
	if err := json.Unmarshal(b, &blob); err != nil {
		return err
	}
	plain, err := decrypt(blob, passphrase)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var st Settings
	if err := json.Unmarshal(plain, &st); err != nil {
		return err
	}
	st.DataDir = s.settings.DataDir
	s.settings = st
	return nil
}

func (s *Store) TryLoad() error {
	if _, err := os.Stat(s.path + ".enc"); err == nil {
		s.path = s.path + ".enc"
		return os.ErrNotExist // caller must use unlock endpoint
	}
	return s.Load()
}

func deriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, keyIter, keyLen, sha256.New)
}

func encrypt(plain []byte, passphrase string) (*encryptedBlob, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := deriveKey(passphrase, salt)
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

func decrypt(blob encryptedBlob, passphrase string) ([]byte, error) {
	key := deriveKey(passphrase, blob.Salt)
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

func (s *Store) Unlock(passphrase string) error {
	if err := s.LoadEncrypted(passphrase); err != nil {
		return err
	}
	return nil
}

func (s *Store) Lock(passphrase string) error {
	return s.SaveEncrypted(passphrase)
}
