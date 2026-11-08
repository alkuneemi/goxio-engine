package portal

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func mustAuthorized(t *testing.T, pub ssh.PublicKey, comment string) string {
	t.Helper()
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

func TestParseSSHPublicKeyAcceptsAllowedTypes(t *testing.T) {
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPub, err := ssh.NewPublicKey(edPriv.Public())
	if err != nil {
		t.Fatal(err)
	}

	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaPub, err := ssh.NewPublicKey(&rsaPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPub, err := ssh.NewPublicKey(&ecPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		raw  string
		algo string
	}{
		{name: "ed25519", raw: mustAuthorized(t, edPub, "box-key"), algo: ssh.KeyAlgoED25519},
		{name: "rsa", raw: mustAuthorized(t, rsaPub, ""), algo: ssh.KeyAlgoRSA},
		{name: "ecdsa", raw: mustAuthorized(t, ecPub, "ecdsa-note"), algo: ssh.KeyAlgoECDSA256},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalized, fp, err := ParseSSHPublicKey("\n  " + tc.raw + "\n")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(normalized))
			if err != nil {
				t.Fatalf("normalized unreadable: %v", err)
			}
			if pub.Type() != tc.algo {
				t.Fatalf("type %s want %s", pub.Type(), tc.algo)
			}
			if !strings.HasPrefix(fp, "SHA256:") || len(fp) < 20 {
				t.Fatalf("fingerprint %q", fp)
			}
			if fp != ssh.FingerprintSHA256(pub) {
				t.Fatalf("fingerprint mismatch %s vs %s", fp, ssh.FingerprintSHA256(pub))
			}
		})
	}
}

func TestParseSSHPublicKeyRejectsPrivateAndJunk(t *testing.T) {
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPub, err := ssh.NewPublicKey(edPriv.Public())
	if err != nil {
		t.Fatal(err)
	}
	good := mustAuthorized(t, edPub, "ok")

	cases := []struct {
		name string
		raw  string
		want error
	}{
		{name: "openssh-private", raw: "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", want: ErrSSHPrivateKey},
		{name: "rsa-private", raw: "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAK8=\n-----END RSA PRIVATE KEY-----", want: ErrSSHPrivateKey},
		{name: "empty", raw: "   \n# comment only\n", want: ErrSSHInvalid},
		{name: "garbage", raw: "not-a-key at all", want: ErrSSHInvalid},
		{name: "two-keys", raw: good + "\n" + good, want: ErrSSHInvalid},
		{name: "cert", raw: strings.Replace(good, "ssh-ed25519", "ssh-ed25519-cert-v01@openssh.com", 1), want: ErrSSHInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ParseSSHPublicKey(tc.raw); err != tc.want {
				t.Fatalf("err = %v want %v", err, tc.want)
			}
		})
	}
}

func TestSetReplaceAndDeleteServerSSHPublicKey(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("sahabify", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	srv, _, err := p.AddServer(acct.ID, "remote", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}

	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	firstPub, err := ssh.NewPublicKey(edPriv.Public())
	if err != nil {
		t.Fatal(err)
	}
	first := mustAuthorized(t, firstPub, "first")

	view, err := p.SetServerSSHPublicKey(acct.ID, srv.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if view.SSHPublicKey == "" || !strings.HasPrefix(view.SSHFingerprint, "SHA256:") {
		t.Fatalf("view = %+v", view)
	}
	if strings.Contains(view.SSHPublicKey, "PRIVATE") {
		t.Fatal("view leaked private key material")
	}

	_, edPriv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secondPub, err := ssh.NewPublicKey(edPriv2.Public())
	if err != nil {
		t.Fatal(err)
	}
	second := mustAuthorized(t, secondPub, "second")
	replaced, err := p.SetServerSSHPublicKey(acct.ID, srv.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.SSHFingerprint == view.SSHFingerprint {
		t.Fatal("replace kept the old fingerprint")
	}
	if !strings.Contains(replaced.SSHPublicKey, "second") {
		t.Fatalf("comment lost: %s", replaced.SSHPublicKey)
	}

	list, err := p.ListServers(acct.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v err %v", list, err)
	}
	if list[0].SSHFingerprint != replaced.SSHFingerprint {
		t.Fatalf("list fingerprint %s want %s", list[0].SSHFingerprint, replaced.SSHFingerprint)
	}

	cleared, err := p.DeleteServerSSHPublicKey(acct.ID, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.SSHPublicKey != "" || cleared.SSHFingerprint != "" {
		t.Fatalf("delete left key: %+v", cleared)
	}

	if _, err := p.SetServerSSHPublicKey(acct.ID, srv.ID, "-----BEGIN OPENSSH PRIVATE KEY-----\nxxxx\n-----END OPENSSH PRIVATE KEY-----"); err != ErrSSHPrivateKey {
		t.Fatalf("private key err = %v", err)
	}
	if _, err := p.SetServerSSHPublicKey("acct_missing", srv.ID, first); err != ErrNotFound {
		t.Fatalf("missing account err = %v", err)
	}
}
