package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/zeotap/ims/internal/models"
)

type ctxKey string

const (
	CtxUserID ctxKey = "userID"
	CtxRole   ctxKey = "role"
)

// Middleware validates the Bearer token and injects claims into context.
func Middleware(issuer *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r)
			if token == "" {
				http.Error(w, `{"error":"missing authorization token"}`, http.StatusUnauthorized)
				return
			}
			claims, err := issuer.Verify(token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxRole, claims.Role)
			// Also set X-User-ID header for the rate limiter.
			r.Header.Set("X-User-ID", claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole rejects requests whose role does not match one of the allowed roles.
func RequireRole(allowed ...models.Role) func(http.Handler) http.Handler {
	set := make(map[models.Role]bool, len(allowed))
	for _, r := range allowed {
		set[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(CtxRole).(models.Role)
			if !set[role] && role != models.RoleAdmin {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return h[7:]
	}
	// Also accept token in cookie for browser SSE.
	if c, err := r.Cookie("access_token"); err == nil {
		return c.Value
	}
	return ""
}

// UserIDFromCtx retrieves the user ID string from context.
func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(CtxUserID).(string)
	return v
}

// RoleFromCtx retrieves the role from context.
func RoleFromCtx(ctx context.Context) models.Role {
	v, _ := ctx.Value(CtxRole).(models.Role)
	return v
}
