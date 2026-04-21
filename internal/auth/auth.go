package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	params := argon2id.DefaultParams
	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", err
	}

	return hash, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {
	ok, _, err := argon2id.CheckHash(password, hash)
	if err != nil {
		return false, err
	}

	return ok, nil
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy-access",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn).UTC()),
		Subject:   userID.String(),
	})

	signedToken, err := token.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {

	claims := jwt.RegisteredClaims{
		Issuer: "chirpy-access",
	}

	var emptyUUID uuid.UUID

	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenSecret), nil
	})

	if err != nil {
		return emptyUUID, err
	}

	uuidString, err := token.Claims.GetSubject()
	if err != nil {
		return emptyUUID, err
	}

	uuid, err := uuid.Parse(uuidString)
	if err != nil {
		return emptyUUID, err
	}

	return uuid, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	authorizationInfo := headers.Get("Authorization")
	if !strings.Contains(authorizationInfo, "Bearer") {
		return "", errors.New("missing authorization information")
	}

	bearerTokenString := strings.Replace(authorizationInfo, "Bearer", "", 1)
	bearerTokenString = strings.TrimSpace(bearerTokenString)

	return bearerTokenString, nil
}

func MakeRefreshToken() string {
	data := make([]byte, 32)
	rand.Read(data) // 32 bytes (256 bits) of random data
	return hex.EncodeToString(data)
}
