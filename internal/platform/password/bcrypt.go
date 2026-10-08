// Package password hashes user passwords.
package password

import "golang.org/x/crypto/bcrypt"

// Bcrypt hashes passwords with bcrypt. Inputs longer than 72 bytes are
// rejected by bcrypt, so validate the max length before calling Hash.
type Bcrypt struct {
	Cost int
}

// NewBcrypt returns a hasher using bcrypt.DefaultCost.
func NewBcrypt() Bcrypt { return Bcrypt{Cost: bcrypt.DefaultCost} }

func (b Bcrypt) Hash(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), b.Cost)
	return string(h), err
}
