package auth

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthReciprocality(t *testing.T) {
	password := "testing1234"
	hashed, err := HashPassword(password)
	if err != nil {
		t.Errorf("Error calling HashPassword: %v", err)
	}

	ok, err := CheckPasswordHash(password, hashed)
	if err != nil {
		t.Errorf("Error calling CheckPasswordHash: %v", err)
	}

	if !ok {
		t.Errorf("CheckPasswordHash(%s, %s) failed.  Expected OK", password, hashed)
	}

}

const goodSecret = "abcdefg"
const badSecret = "zyxwvut"

func TestJWTReciprocality(t *testing.T) {
	duration, err := time.ParseDuration("1h")
	if err != nil {
		t.Errorf("Failed to parse duration: %v", err)
	}

	test_uuid := uuid.New()

	tokenString, err := MakeJWT(test_uuid, goodSecret, duration)
	if err != nil {
		t.Errorf("Failed to generate JWT Token: %v", err)
	}

	recovered_uuid, err := ValidateJWT(tokenString, goodSecret)
	if err != nil {
		t.Errorf("Failed to validate JWT Token: %v", err)
	}

	if recovered_uuid != test_uuid {
		t.Errorf("Bad UUID {%v}, expected {%v}", recovered_uuid, test_uuid)
	}
}

func TestJWTFailWithBadSecret(t *testing.T) {
	duration, err := time.ParseDuration("1h")
	if err != nil {
		t.Errorf("Failed to parse duration: %v", err)
	}

	test_uuid := uuid.New()

	tokenString, err := MakeJWT(test_uuid, goodSecret, duration)
	if err != nil {
		t.Errorf("Failed to generate JWT Token: %v", err)
	}

	_, err = ValidateJWT(tokenString, badSecret)
	if err == nil {
		fmt.Printf("Validated with bad secret.\n")
		t.Errorf("Validation should have failed, but succeeded")
	}

	fmt.Printf("Fail with bad secret\n")
}

func TestJWTFailExpired(t *testing.T) {
	sleep_duration, err := time.ParseDuration("1s")
	duration, err := time.ParseDuration("10ms")
	if err != nil {
		t.Errorf("Failed to parse duration: %v", err)
	}

	test_uuid := uuid.New()

	tokenString, err := MakeJWT(test_uuid, goodSecret, duration)
	if err != nil {
		t.Errorf("Failed to generate JWT Token: %v", err)
	}

	time.Sleep(sleep_duration)

	recovered_uuid, err := ValidateJWT(tokenString, goodSecret)
	if err == nil {
		t.Errorf("Validation on expired token succeeded")
	}

	fmt.Printf("Fail with expired token, UUID {%v}\n", recovered_uuid)
}
