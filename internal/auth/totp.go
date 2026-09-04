package auth

import (
	"fmt"

	"github.com/pquerna/otp/totp"
)

// GenerateTOTP crea un nuevo secreto TOTP (RFC 6238, §25) para accountName.
// otpauthURL es la URL otpauth:// que una app autenticadora puede
// interpretar como código QR.
func GenerateTOTP(issuer, accountName string) (secret string, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: accountName})
	if err != nil {
		return "", "", fmt.Errorf("generando secreto TOTP: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTP comprueba un código de 6 dígitos contra el secreto guardado.
func ValidateTOTP(passcode, secret string) bool {
	return totp.Validate(passcode, secret)
}
