package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHashAndCheck(t *testing.T) {
	plain := "supersecret"
	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, plain) {
		t.Error("CheckPassword should return true for correct password")
	}
	if CheckPassword(hash, "wrongpassword") {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestIssueAndValidateToken(t *testing.T) {
	secret := "testsecret"
	token, err := IssueToken("alice", "edit", secret)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	claims, err := ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Sub != "alice" {
		t.Errorf("Sub = %q, want %q", claims.Sub, "alice")
	}
	if claims.Role != "edit" {
		t.Errorf("Role = %q, want %q", claims.Role, "edit")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	secret := "testsecret"
	// Build a token that is already expired.
	past := time.Now().Add(-2 * time.Hour)
	claims := Claims{
		Sub:  "bob",
		Role: "view",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "bob",
			ExpiresAt: jwt.NewNumericDate(past),
			IssuedAt:  jwt.NewNumericDate(past.Add(-time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	_, err = ValidateToken(signed, secret)
	if err == nil {
		t.Error("ValidateToken should reject an expired token")
	}
}

func TestIssueRefreshTokenUniqueness(t *testing.T) {
	raw1, hash1, err := IssueRefreshToken()
	if err != nil {
		t.Fatalf("IssueRefreshToken #1: %v", err)
	}
	raw2, hash2, err := IssueRefreshToken()
	if err != nil {
		t.Fatalf("IssueRefreshToken #2: %v", err)
	}
	if raw1 == raw2 {
		t.Error("raw tokens should be different")
	}
	if hash1 == hash2 {
		t.Error("hashes should be different")
	}
	if raw1 == hash1 {
		t.Error("raw token and its hash should differ")
	}
}
