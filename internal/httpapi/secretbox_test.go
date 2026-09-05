package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"filebox/internal/store"
)

func newSecretTestServer(t *testing.T, encryptionKey []byte) (*Server, *store.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		db.Close()
		t.Fatal(err)
	}
	server := NewServer(db, Config{DataDir: db.DataDir, JWTSecret: []byte("legacy-jwt-secret-for-tests"), EncryptionKey: encryptionKey})
	t.Cleanup(func() { db.Close() })
	return server, db
}

func legacyEncryptSecretForTest(t *testing.T, jwtSecret []byte, plaintext string) string {
	t.Helper()
	key := sha256.Sum256(jwtSecret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil))
}

func TestSensitiveSecretRoundTripUsesIndependentKey(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	server, _ := newSecretTestServer(t, key)

	for _, encrypt := range []func(string) (string, error){server.encryptTOTPSecret, server.encryptSyncSecret} {
		ciphertext, err := encrypt("sensitive-value")
		if err != nil {
			t.Fatal(err)
		}
		framed, err := base64.RawURLEncoding.DecodeString(ciphertext)
		if err != nil || len(framed) < len(sensitiveSecretEnvelope) || string(framed[:len(sensitiveSecretEnvelope)]) != string(sensitiveSecretEnvelope) {
			t.Fatalf("ciphertext is not versioned static-key envelope: %q", ciphertext)
		}
		plaintext, legacy, err := server.decryptSensitiveSecret(ciphertext)
		if err != nil || legacy || plaintext != "sensitive-value" {
			t.Fatalf("decrypt independent-key value = %q, legacy=%t, err=%v", plaintext, legacy, err)
		}
	}
}

func TestSensitiveSecretFallsBackToLegacyJWTDerivedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	server, _ := newSecretTestServer(t, key)
	legacyCiphertext := legacyEncryptSecretForTest(t, server.config.JWTSecret, "legacy-value")

	plaintext, legacy, err := server.decryptSensitiveSecret(legacyCiphertext)
	if err != nil || !legacy || plaintext != "legacy-value" {
		t.Fatalf("decrypt legacy value = %q, legacy=%t, err=%v", plaintext, legacy, err)
	}
}

func TestSensitiveSecretWithoutIndependentKeyRetainsLegacyBehavior(t *testing.T) {
	server, _ := newSecretTestServer(t, nil)
	ciphertext, err := server.encryptTOTPSecret("legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, legacy, err := server.decryptSensitiveSecret(ciphertext)
	if err != nil || !legacy || plaintext != "legacy-value" {
		t.Fatalf("no-key round trip = %q, legacy=%t, err=%v", plaintext, legacy, err)
	}
}

func TestTOTPSecretLazyMigrationPreservesReplayState(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(255 - index)
	}
	server, db := newSecretTestServer(t, key)
	user, err := db.GetUserByUsername("admin")
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacyEncryptSecretForTest(t, server.config.JWTSecret, "JBSWY3DPEHPK3PXP")
	if err := db.SetTOTP(context.Background(), user.ID, legacy, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE users SET last_used_totp = '12345' WHERE id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	user, err = db.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.decryptTOTPSecretForUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	migrated, err := db.GetUser(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.TOTPSecret == legacy || migrated.LastUsedTOTP != "12345" || !migrated.TOTPEnabled {
		t.Fatalf("lazy migration changed unexpected state: secret same=%t counter=%q enabled=%t", migrated.TOTPSecret == legacy, migrated.LastUsedTOTP, migrated.TOTPEnabled)
	}
	if plaintext, err := server.decryptTOTPSecret(migrated.TOTPSecret); err != nil || plaintext != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("migrated TOTP decrypt = %q, err=%v", plaintext, err)
	}
}

func TestSyncCredentialsLazyMigrationPersistsBothValues(t *testing.T) {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 33)
	}
	server, db := newSecretTestServer(t, key)
	legacySecret := legacyEncryptSecretForTest(t, server.config.JWTSecret, "remote-password")
	legacyPassphrase := legacyEncryptSecretForTest(t, server.config.JWTSecret, "key-passphrase")
	item, err := db.CreateRemoteSystem(context.Background(), store.RemoteSystem{UserID: 1, Name: "remote", Kind: "sftp", Host: "example.test", Port: 22, Username: "user", AuthType: "key", AuthSecret: legacySecret, AuthPassphrase: legacyPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	secret, passphrase, err := server.decryptSyncCredentials(context.Background(), item)
	if err != nil || secret != "remote-password" || passphrase != "key-passphrase" {
		t.Fatalf("sync credentials = %q/%q, err=%v", secret, passphrase, err)
	}
	migrated, err := db.GetRemoteSystem(context.Background(), item.ID, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.AuthSecret == legacySecret || migrated.AuthPassphrase == legacyPassphrase {
		t.Fatal("sync credentials were not persisted with the independent key")
	}
	if secret, _, err := server.decryptSensitiveSecret(migrated.AuthSecret); err != nil || secret != "remote-password" {
		t.Fatalf("migrated sync secret decrypt = %q, err=%v", secret, err)
	}
}

func TestVersionedSecretWrongKeyFailsWithoutLegacyFallback(t *testing.T) {
	firstKey := make([]byte, 32)
	secondKey := make([]byte, 32)
	for index := range firstKey {
		firstKey[index] = byte(index + 1)
		secondKey[index] = byte(index + 101)
	}
	writer, _ := newSecretTestServer(t, firstKey)
	reader, _ := newSecretTestServer(t, secondKey)
	ciphertext, err := writer.encryptSyncSecret("value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.decryptSyncSecret(ciphertext); err == nil {
		t.Fatal("wrong independent key unexpectedly decrypted a versioned secret")
	}
	unsupported := append([]byte(nil), sensitiveSecretEnvelope...)
	unsupported[4] = 2
	unsupportedCiphertext := base64.RawURLEncoding.EncodeToString(unsupported)
	if _, err := reader.decryptSyncSecret(unsupportedCiphertext); err == nil {
		t.Fatal("unsupported envelope version unexpectedly fell back to legacy decryption")
	}
}
