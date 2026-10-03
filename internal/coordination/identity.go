package coordination

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func randomIdentifier(prefix string) (string, error) {
	suffix, err := randomBytes(9)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", prefix, suffix), nil
}

// randomHyphenIdentifier returns an identifier using only lowercase hex digits
// (plus the prefix/hyphens), which matches the domain id pattern for resources
// whose id alphabet excludes underscores, such as region lockdowns.
func randomHyphenIdentifier(prefix string) (string, error) {
	value := make([]byte, 10)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%x", prefix, value), nil
}

func randomToken() (string, error) {
	return randomBytes(24)
}

func randomBytes(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
