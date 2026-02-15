package auth

import (
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
	byteSecret := []byte(tokenSecret)
	completeJWT, err := newJWTToken.SignedString(byteSecret)
	if err != nil {
		return "", err
	}
	return completeJWT, nil
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenSecret), nil
	})
	if err != nil {
		fmt.Printf("Failed after validation")
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
	fmt.Printf("The token string received from GetBearerToken is %v\n", tokenString)
	return tokenString, nil
}
