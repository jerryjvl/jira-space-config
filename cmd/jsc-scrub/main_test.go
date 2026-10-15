package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScrub runs the command over a small recording: the site, the cloud ID
// and every form of account ID (plain, in a URL, the older 24-digit form)
// are replaced, the same account always by the same fake.
func TestScrub(t *testing.T) {
	dir, accounts := t.TempDir(), filepath.Join(t.TempDir(), "accounts.txt")
	real := "557058:2f972df4-413b-4225-8b93-878f69b980e9"
	recording := `{"cloudId": "0e98014c-0183-4713-8545-8c618884bbdb",
  "self": "https://acme.atlassian.net/rest/api/3/user?accountId=557058%3A2f972df4-413b-4225-8b93-878f69b980e9",
  "accountId": "` + real + `", "displayName": "Real Person", "emailAddress": "real@acme.com",
  "lead": "5b10a2844c20165700ede21f", "note": "see acme"}`
	if err := os.WriteFile(filepath.Join(dir, "r.json"), []byte(recording), 0o600); err != nil {
		t.Fatal(err)
	}
	args, set := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = args, set })
	os.Args = []string{"jsc-scrub", "--site", "https://acme.atlassian.net", "--accounts", accounts, dir}
	flag.CommandLine = flag.NewFlagSet("jsc-scrub", flag.ContinueOnError)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, leak := range []string{"acme", "0e98014c", "2f972df4", "5b10a2844c20165700ede21f", "Real Person", "real@acme.com"} {
		if strings.Contains(got, leak) {
			t.Errorf("still contains %q:\n%s", leak, got)
		}
	}
	fake := "000000:00000000-0000-0000-0000-000000000001"
	for _, want := range []string{`"accountId": "` + fake + `"`, "accountId=000000%3A00000000-0000-0000-0000-000000000001",
		fakeCloudID, "example.atlassian.net"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}
