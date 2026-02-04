package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testUserID = uuid.MustParse("123e4567-e89b-12d3-a456-426655440000")
var expiredDuration, _ = time.ParseDuration("-10h")
var validDuration, _ = time.ParseDuration("10h")
var testSecretKey = "4FeINserbkQvFg2DgKX7OFNWsLdkND1HMHoIvpLvezM="

func TestMakeJWT(t *testing.T) {
	testToken, err := MakeJWT(testUserID, testSecretKey, validDuration)
	if err != nil {
		t.Errorf("Got an error, this should have been valid.")
	}
	_, errTwo := ValidateJWT(testToken, testSecretKey)
	if errTwo != nil {
		t.Errorf("Got an error, this should have been valid.")
	}

}

func TestExpiredDuration(t *testing.T) {
	testToken, err := MakeJWT(testUserID, testSecretKey, expiredDuration)
	if err != nil {
		t.Errorf("Got an error, this shouldn't fail yet.")
	}
	_, errTwo := ValidateJWT(testToken, testSecretKey)
	if errTwo == nil {
		t.Errorf("Got an error, this should have failed.")
	}

}

func TestGetBearerToken(t *testing.T) {
	var testHeader = make(http.Header)
	testHeader.Add("Authorization", "BEARER test_token_here")
	resultString, err := GetBearerToken(testHeader)
	if err != nil {
		t.Errorf("Error, should have returned a valid string")
	}
	if resultString != "test_token_here" {
		t.Errorf("Error, the result string is not what it should be")
	}
}
