package portal

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func testOpts() Options {
	return Options{BcryptCost: bcrypt.MinCost, SecureCookies: false}
}

func TestIngestSeesServerAddedByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	serve, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	cli, err := Open(dir, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := cli.CreateAccount("sahabify", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := cli.AddServer(acct.ID, "remote", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"host":"remote-host","nodes":[{"id":"space-volume-data","type":"volume","name":"data","health":"unknown","metadata":{"kind":"volume","size_state":"measured"}}]}`)
	meta, err := serve.Ingest(token, body)
	if err != nil {
		t.Fatalf("running server rejected a token written by another process: %v", err)
	}
	if meta.Status != "ready" || meta.Volumes != 1 {
		t.Fatalf("meta = %+v", meta)
	}
	if _, err := serve.Ingest("not-the-token", body); err != ErrToken {
		t.Fatalf("bad token err = %v", err)
	}
}

func TestEmailAliasDoesNotCollapseLookalikes(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateAccount("sahabify", "password-long-enough"); err != nil {
		t.Fatal(err)
	}
	if err := p.AddAlias("sahabify", "Sahabify@Gmail.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Authenticate("sahabify@gmail.com", "password-long-enough"); err != nil {
		t.Fatalf("email login: %v", err)
	}
	if _, err := p.Authenticate("sahabify", "password-long-enough"); err != nil {
		t.Fatalf("slug login: %v", err)
	}
	if _, err := p.CreateAccount("sahabify@gmail.com", "another-password-ok"); err != ErrExists {
		t.Fatalf("duplicate email err = %v", err)
	}
	if _, err := p.CreateAccount("saha.bify@gmail.com", "another-password-ok"); err != nil {
		t.Fatalf("dotted local part must stay distinct: %v", err)
	}
	if _, err := p.CreateAccount("sahabify+ops@gmail.com", "another-password-ok"); err != nil {
		t.Fatalf("plus tag must stay distinct: %v", err)
	}
	if _, err := p.Authenticate("saha.bify@gmail.com", "password-long-enough"); err != ErrInvalid {
		t.Fatal("dotted address authenticated as the other account")
	}
}

func TestCanonicalEmail(t *testing.T) {
	got, err := canonicalEmail("  Sahabify@Gmail.com ")
	if err != nil || got != "sahabify@gmail.com" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := canonicalEmail("not an email"); err == nil {
		t.Fatal("accepted spaces")
	}
	if _, err := canonicalEmail("a@b"); err == nil {
		t.Fatal("accepted domain without a dot")
	}
}
