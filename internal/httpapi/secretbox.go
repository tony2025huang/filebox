package httpapi

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// sensitiveSecretEnvelope is the versioned framing for values encrypted with
// the independent static key. The header is also authenticated as AAD.
var sensitiveSecretMagic = []byte{'F', 'B', 'S', 'E'}
var sensitiveSecretEnvelope = []byte{'F', 'B', 'S', 'E', 1, 1}

var errInvalidEncryptedSecret = errors.New("invalid encrypted secret")

func (s *Server) encryptSensitiveSecret(secret string) (string, error) {
	if len(s.config.EncryptionKey) != 0 && len(s.config.EncryptionKey) != 32 {
		return "", errors.New("invalid static encryption key")
	}
	key := sha256.Sum256(s.config.JWTSecret)
	framed := false
	if len(s.config.EncryptionKey) == 32 {
		copy(key[:], s.config.EncryptionKey)
		framed = true
	}
	gcm, err := newSecretGCM(key[:])
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	if !framed {
		return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(secret), nil)), nil
	}
	sealed := gcm.Seal(nil, nonce, []byte(secret), sensitiveSecretEnvelope)
	framedBytes := make([]byte, 0, len(sensitiveSecretEnvelope)+len(nonce)+len(sealed))
	framedBytes = append(framedBytes, sensitiveSecretEnvelope...)
	framedBytes = append(framedBytes, nonce...)
	framedBytes = append(framedBytes, sealed...)
	return base64.RawURLEncoding.EncodeToString(framedBytes), nil
}

// decryptSensitiveSecret returns legacy=true when the value used the old
// unframed SHA256(JWTSecret)-derived AES-GCM format.
func (s *Server) decryptSensitiveSecret(value string) (plaintext string, legacy bool, err error) {
	if len(s.config.EncryptionKey) != 0 && len(s.config.EncryptionKey) != 32 {
		return "", false, errInvalidEncryptedSecret
	}
	framed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", false, errInvalidEncryptedSecret
	}
	if len(framed) >= len(sensitiveSecretMagic) && bytes.Equal(framed[:len(sensitiveSecretMagic)], sensitiveSecretMagic) {
		if len(framed) < len(sensitiveSecretEnvelope) || !bytes.Equal(framed[:len(sensitiveSecretEnvelope)], sensitiveSecretEnvelope) {
			return "", false, errInvalidEncryptedSecret
		}
		if len(s.config.EncryptionKey) != 32 {
			return "", false, errInvalidEncryptedSecret
		}
		gcm, err := newSecretGCM(s.config.EncryptionKey)
		if err != nil || len(framed) < len(sensitiveSecretEnvelope)+gcm.NonceSize()+gcm.Overhead() {
			return "", false, errInvalidEncryptedSecret
		}
		start := len(sensitiveSecretEnvelope)
		plainBytes, err := gcm.Open(nil, framed[start:start+gcm.NonceSize()], framed[start+gcm.NonceSize():], sensitiveSecretEnvelope)
		if err != nil {
			return "", false, errInvalidEncryptedSecret
		}
		return string(plainBytes), false, nil
	}

	// Legacy values have no frame and therefore cannot be confused with a
	// versioned value. They remain decryptable with the JWT-derived key only.
	key := sha256.Sum256(s.config.JWTSecret)
	gcm, err := newSecretGCM(key[:])
	if err != nil || len(framed) < gcm.NonceSize()+gcm.Overhead() {
		return "", true, errInvalidEncryptedSecret
	}
	plainBytes, err := gcm.Open(nil, framed[:gcm.NonceSize()], framed[gcm.NonceSize():], nil)
	if err != nil {
		return "", true, errInvalidEncryptedSecret
	}
	return string(plainBytes), true, nil
}

func newSecretGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
