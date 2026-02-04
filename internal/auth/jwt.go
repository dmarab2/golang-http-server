package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	newJWTToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
		Subject:   userID.String(),
	})
	byteSecret, err := base64.StdEncoding.DecodeString(tokenSecret)
	if err != nil {
		return "", err
	}
	completeJWT, err := newJWTToken.SignedString(byteSecret)
	if err != nil {
		return "", err
	}
	return completeJWT, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (any, error) {
		return base64.StdEncoding.DecodeString(tokenSecret)
	})
	if err != nil {
		return uuid.UUID{}, err
	} else if claims, ok := token.Claims.(*jwt.RegisteredClaims); ok {
		uuidConversion, err := uuid.Parse(claims.Subject)
		if err != nil {
			return uuid.UUID{}, err
		}
		return uuidConversion, nil
	} else {
		return uuid.UUID{}, fmt.Errorf("Unknown claim type")
	}
}

func GetBearerToken(headers http.Header) (string, error) {
	authorizationHeader := headers.Get("Authorization")
	if authorizationHeader == "" {
		return "", fmt.Errorf("There is no authorization in this request!")
	}
	headerList := strings.Fields(authorizationHeader)
	tokenString := headerList[1]
	return tokenString, nil

}
