package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("test1234")
	if err != nil {
		t.Fatalf("HashPassword gave error: %v", err)
	}

	if !CheckPassword("test1234", hash) {
		t.Errorf("Correct password gave error")
	}

	if CheckPassword("WrongPassword", hash) {
		t.Errorf("Wrong password has been accepted")
	}
}
