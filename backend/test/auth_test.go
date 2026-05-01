package test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/auth"
	"github.com/zeotap/ims/internal/models"
)

func newTestIssuer() *auth.Issuer {
	return auth.NewIssuer("test_secret_key", 15*time.Minute, 7*24*time.Hour)
}

func TestJWT_IssueAndVerify(t *testing.T) {
	issuer := newTestIssuer()
	id := uuid.New()

	pair, err := issuer.Issue(id, models.RoleResponder)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty token pair")
	}

	claims, err := issuer.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.UserID != id.String() {
		t.Fatalf("user ID mismatch: got %s want %s", claims.UserID, id.String())
	}
	if claims.Role != models.RoleResponder {
		t.Fatalf("role mismatch: got %s want %s", claims.Role, models.RoleResponder)
	}
}

func TestJWT_RejectsTamperedToken(t *testing.T) {
	issuer := newTestIssuer()
	pair, _ := issuer.Issue(uuid.New(), models.RoleProducer)

	tampered := pair.AccessToken + "x"
	if _, err := issuer.Verify(tampered); err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestJWT_RejectsExpiredToken(t *testing.T) {
	issuer := auth.NewIssuer("test_secret_key", -1*time.Second, 7*24*time.Hour)
	pair, _ := issuer.Issue(uuid.New(), models.RoleAdmin)

	if _, err := issuer.Verify(pair.AccessToken); err == nil {
		t.Fatal("expected error for expired token")
	}
}
