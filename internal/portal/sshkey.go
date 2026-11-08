package portal

import (
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"
)

var (
	ErrSSHPrivateKey  = errors.New("private ssh keys are not accepted")
	ErrSSHInvalid     = errors.New("invalid ssh public key")
	ErrSSHUnsupported = errors.New("unsupported ssh public key")
)

const maxSSHPublicKeyBytes = 8192

// ParseSSHPublicKey accepts one OpenSSH public key of type ssh-ed25519,
// ssh-rsa, or ecdsa-sha2-nistp256/384/521. Private keys and certificates
// are rejected. The fingerprint is the SHA256 form used by ssh-keygen -l.
func ParseSSHPublicKey(raw string) (normalized, fingerprint string, err error) {
	if looksLikeSSHPrivateKey(raw) {
		return "", "", ErrSSHPrivateKey
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxSSHPublicKeyBytes {
		return "", "", ErrSSHInvalid
	}
	var line string
	for _, candidate := range strings.Split(raw, "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		if line != "" {
			return "", "", ErrSSHInvalid
		}
		line = candidate
	}
	if line == "" || looksLikeSSHPrivateKey(line) {
		return "", "", ErrSSHInvalid
	}
	pub, comment, _, rest, parseErr := ssh.ParseAuthorizedKey([]byte(line))
	if parseErr != nil || pub == nil {
		return "", "", ErrSSHInvalid
	}
	if leftover := strings.TrimSpace(string(rest)); leftover != "" && !strings.HasPrefix(leftover, "#") {
		return "", "", ErrSSHInvalid
	}
	if !allowedSSHPublicKey(pub.Type()) {
		return "", "", ErrSSHUnsupported
	}
	normalized = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment = strings.TrimSpace(comment); comment != "" {
		normalized = normalized + " " + comment
	}
	return normalized, ssh.FingerprintSHA256(pub), nil
}

func allowedSSHPublicKey(algo string) bool {
	switch algo {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return true
	default:
		return false
	}
}

func looksLikeSSHPrivateKey(raw string) bool {
	upper := strings.ToUpper(raw)
	markers := []string{
		"BEGIN OPENSSH PRIVATE KEY",
		"BEGIN RSA PRIVATE KEY",
		"BEGIN EC PRIVATE KEY",
		"BEGIN DSA PRIVATE KEY",
		"BEGIN PRIVATE KEY",
		"BEGIN ENCRYPTED PRIVATE KEY",
		"BEGIN OPENSSH PRIVATE",
	}
	for _, m := range markers {
		if strings.Contains(upper, m) {
			return true
		}
	}
	return false
}
