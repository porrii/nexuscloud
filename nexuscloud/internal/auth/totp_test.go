package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateAndValidateTOTP(t *testing.T) {
	secret, url, err := GenerateTOTP("NexusCloud", "ivan")
	if err != nil {
		t.Fatalf("GenerateTOTP falló: %v", err)
	}
	if secret == "" || url == "" {
		t.Fatal("secret/url no deberían estar vacíos")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generando código de prueba: %v", err)
	}
	if !ValidateTOTP(code, secret) {
		t.Error("ValidateTOTP debería aceptar un código válido recién generado")
	}
	if ValidateTOTP("000000", secret) {
		t.Error("ValidateTOTP no debería aceptar un código arbitrario")
	}
}
