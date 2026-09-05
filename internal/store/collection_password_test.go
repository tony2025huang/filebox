package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestUploadCollectionStoresOnlyBcryptPasswordHash(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureAdmin("admin", "admin123", 1024); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("collection-secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateUploadCollection(context.Background(), UploadCollection{
		CreatedBy: 1, Name: "protected", Token: "collection-password-test",
		PasswordHash: string(hash), ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.DB.QueryRow("SELECT password_hash FROM upload_collections WHERE id = ?", created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "collection-secret" || bcrypt.CompareHashAndPassword([]byte(stored), []byte("collection-secret")) != nil {
		t.Fatalf("stored collection password is not a matching bcrypt hash: %q", stored)
	}
	loaded, err := db.GetUploadCollectionByToken(context.Background(), created.Token)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || containsAny(string(encoded), "collection-secret", "passwordHash") {
		t.Fatalf("collection JSON leaked password data: %s", encoded)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
