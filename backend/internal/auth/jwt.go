package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/zeotap/ims/internal/models"
)

var ErrInvalidToken = errors.New("invalid or expired token")

type Claims struct {
	UserID string     `json:"uid"`
	Role   models.Role `json:"role"`
	jwt.RegisteredClaims
}

type Issuer struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewIssuer(secret string, accessTTL, refreshTTL time.Duration) *Issuer {
	return &Issuer{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (i *Issuer) Issue(userID uuid.UUID, role models.Role) (*models.TokenPair, error) {
	access, err := i.sign(userID.String(), role, i.accessTTL, "access")
	if err != nil {
		return nil, err
	}
	refresh, err := i.sign(userID.String(), role, i.refreshTTL, "refresh")
	if err != nil {
		return nil, err
	}
	return &models.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (i *Issuer) sign(userID string, role models.Role, ttl time.Duration, typ string) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			ID:        uuid.New().String(),
			Issuer:    "ims",
			Audience:  jwt.ClaimStrings{typ},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(i.secret)
}

// VerifyRefresh validates a token and asserts it has audience "refresh".
func (i *Issuer) VerifyRefresh(tokenStr string) (*Claims, error) {
	claims, err := i.Verify(tokenStr)
	if err != nil {
		return nil, err
	}
	for _, a := range claims.Audience {
		if a == "refresh" {
			return claims, nil
		}
	}
	return nil, ErrInvalidToken
}

func (i *Issuer) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
