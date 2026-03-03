package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	NodeID string `json:"node_id"`

	DisplayName string `json:"display_name"`

	Services []string `json:"services"`

	jwt.RegisteredClaims
}

type Manager struct {
	secret    []byte
	expiryHrs int
	issuer    string
}

func NewManager(secret string, expiryHrs int, nodeID string) *Manager {
	return &Manager{
		secret:    []byte(secret),
		expiryHrs: expiryHrs,
		issuer:    "lan-suite:" + nodeID,
	}
}

func (m *Manager) Issue(nodeID, displayName string, services []string) (string, error) {
	now := time.Now()
	expiry := now.Add(time.Duration(m.expiryHrs) * time.Hour)

	claims := &Claims{
		NodeID:      nodeID,
		DisplayName: displayName,
		Services:    services,
		RegisteredClaims: jwt.RegisteredClaims{

			Subject: nodeID,

			Issuer: m.issuer,

			IssuedAt: jwt.NewNumericDate(now),

			ExpiresAt: jwt.NewNumericDate(expiry),

			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return signed, nil
}

func (m *Manager) Verify(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},

		func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.secret, nil
		},
	)

	if err != nil {

		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, fmt.Errorf("token expired")
		case errors.Is(err, jwt.ErrTokenNotValidYet):
			return nil, fmt.Errorf("token not yet valid")
		case errors.Is(err, jwt.ErrTokenMalformed):
			return nil, fmt.Errorf("malformed token")
		default:
			return nil, fmt.Errorf("invalid token")
		}
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
