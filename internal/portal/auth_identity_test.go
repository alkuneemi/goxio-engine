package portal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSlugEmailLoginAndPastedPassword(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateAccount("Sahabify", "password-long-enough"); err != nil {
		t.Fatal(err)
	}
	// The address is not an alias yet. Signup must not take it or join the tenant.
	if _, err := p.CreateAccount("sahabify@gmail.com", "another-password-ok"); err != ErrExists {
		t.Fatalf("signup err = %v", err)
	}
	if _, err := p.FindAccount("sahabify@gmail.com"); err != ErrNotFound {
		t.Fatal("failed signup still claimed the email")
	}
	if _, err := p.Authenticate("sahabify@gmail.com", "another-password-ok"); err != ErrInvalid {
		t.Fatal("wrong password authenticated")
	}
	if _, err := p.FindAccount("sahabify@gmail.com"); err != ErrNotFound {
		t.Fatal("wrong password wrote an alias")
	}
	if _, err := p.Authenticate("sahabify", "password-long-enough\n"); err != ErrInvalid {
		t.Fatal("a trailing newline matched the stored password")
	}
	if _, err := p.Authenticate("sahabify", "password-long-enough "); err != ErrInvalid {
		t.Fatal("a trailing space matched the stored password")
	}
	sess, err := p.Authenticate("  Sahabify@Gmail.com  ", "password-long-enough")
	if err != nil {
		t.Fatalf("email login: %v", err)
	}
	if _, err := p.LookupSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	got, err := p.FindAccount("SAHABIFY")
	if err != nil || len(got.Aliases) != 1 || got.Aliases[0] != "sahabify@gmail.com" {
		t.Fatalf("account = %+v err %v", got.Aliases, err)
	}
	if _, err := p.Authenticate("saha.bify@gmail.com", "password-long-enough"); err != ErrInvalid {
		t.Fatal("dotted address entered the slug account")
	}
	if _, err := p.Authenticate("sahabify+ops@gmail.com", "password-long-enough"); err != ErrInvalid {
		t.Fatal("plus-tag entered the slug account")
	}
	if _, err := p.Authenticate("sahabify", "password-long- enough"); err != ErrInvalid {
		t.Fatal("internal space was ignored")
	}
	if _, err := p.CreateAccount("saha.bify@gmail.com", "another-password-ok"); err != nil {
		t.Fatalf("dotted address must be its own tenant: %v", err)
	}
}

func TestLegacyMixedCaseStillSignsIn(t *testing.T) {
	dir := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("password-long-enough "), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	acct := Account{
		ID: "acct_legacycase0001", Login: "Sahabify", PasswordHash: string(hash),
		CreatedAt: time.Now().UTC(), Aliases: []string{"Sahabify@Gmail.com"},
	}
	if err := os.MkdirAll(filepath.Join(dir, "accounts"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(acct, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "accounts", acct.ID+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := p.Authenticate("SAHABIFY", "password-long-enough ")
	if err != nil {
		t.Fatalf("legacy exact password: %v", err)
	}
	if _, err := p.Authenticate("sahabify@gmail.com", "password-long-enough"); err != ErrInvalid {
		t.Fatal("trimmed password matched a hash that includes the trailing space")
	}
	p2, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.LookupSession(sess.ID); err != nil {
		t.Fatalf("session did not survive reopen: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "accounts", acct.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored Account
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Login != "sahabify" || stored.PasswordHash != string(hash) {
		t.Fatalf("migration changed identity or hash: %+v", stored)
	}
	if len(stored.Aliases) != 1 || stored.Aliases[0] != "sahabify@gmail.com" {
		t.Fatalf("aliases = %v", stored.Aliases)
	}
}

func TestConcurrentSignupOneAccount(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = a.CreateAccount("new-tenant", "password-long-enough")
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = b.CreateAccount("NEW-tenant", "password-long-enough")
	}()
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		if err != ErrExists {
			t.Fatalf("err = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d (%v)", wins, errs)
	}
	p, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate("new-tenant", "password-long-enough"); err != nil {
		t.Fatal(err)
	}
}
