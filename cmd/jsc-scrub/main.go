// Command jsc-scrub is a development tool: it removes a real site's details
// from a recording made by jsc-record, in place, so it can go into the
// repository as test fixtures (docs/api/README.md):
//
//   - the site's host and bare name become example.atlassian.net and example;
//   - the cloud ID becomes all zeros;
//   - every account ID becomes a numbered fake, the same one each time: the
//     mapping is kept in --accounts, a file that holds real IDs and so must
//     stay outside the repository;
//   - users' display names become "Example User", their email addresses
//     user@example.com, and their avatars are removed (space and issue type
//     avatars are kept).
//
// It scrubs every .json and .yaml file in the directory (a recorded spec
// too), and refuses to finish if a file still contains the site's name.
//
//	go run ./cmd/jsc-scrub --site https://example.atlassian.net --accounts ACCOUNTMAP DIR
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

var (
	// Account IDs: the current form anywhere, and the older 24-digit form
	// only where it stands as an account (after user:, or as a whole string).
	accountPattern = regexp.MustCompile(`\b\d{6}:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	// encodedAccount is an account ID in a URL, its colon written %3A.
	encodedAccount = regexp.MustCompile(`(?i)\b(\d{6})%3A([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)
	oldAccount     = regexp.MustCompile(`(user:|")([0-9a-f]{24})("|\b)`)
	cloudIDPattern = regexp.MustCompile(`"cloudId"\s*:\s*"([0-9a-f-]{36})"`)
)

const fakeCloudID = "00000000-0000-0000-0000-000000000000"

func run() error {
	site := flag.String("site", "", "the recorded site, e.g. https://name.atlassian.net (required)")
	accountsFile := flag.String("accounts", "", "the account ID mapping, outside the repository (required)")
	flag.Parse()
	if *site == "" || *accountsFile == "" || flag.NArg() != 1 {
		return errors.New("usage: jsc-scrub --site URL --accounts FILE DIR")
	}
	dir := flag.Arg(0)
	host := strings.TrimSuffix(strings.TrimPrefix(*site, "https://"), "/")
	name, _, _ := strings.Cut(host, ".")
	if name == "" || name == "example" {
		return fmt.Errorf("--site %q: give the real site the recording came from", *site)
	}
	accounts, err := loadAccounts(*accountsFile)
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	cloudID := ""
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // G304: the recording the developer named
		if err != nil {
			return err
		}
		if m := cloudIDPattern.FindSubmatch(data); m != nil {
			cloudID = string(m[1])
		}
	}
	scrubbed := 0
	for _, f := range files {
		if ext := filepath.Ext(f); ext != ".json" && ext != ".yaml" {
			continue
		}
		data, err := os.ReadFile(f) //nolint:gosec // G304: the recording the developer named
		if err != nil {
			return err
		}
		out := data
		if filepath.Ext(f) == ".json" {
			if out, err = scrubUsers(out); err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
		}
		out = bytes.ReplaceAll(out, []byte(host), []byte("example.atlassian.net"))
		if cloudID != "" {
			out = bytes.ReplaceAll(out, []byte(cloudID), []byte(fakeCloudID))
		}
		out = accountPattern.ReplaceAllFunc(out, func(id []byte) []byte { return []byte(accounts.fake(string(id))) })
		out = encodedAccount.ReplaceAllFunc(out, func(m []byte) []byte {
			p := encodedAccount.FindSubmatch(m)
			return []byte(strings.Replace(accounts.fake(string(p[1])+":"+strings.ToLower(string(p[2]))), ":", "%3A", 1))
		})
		out = oldAccount.ReplaceAllFunc(out, func(m []byte) []byte {
			p := oldAccount.FindSubmatch(m)
			return []byte(string(p[1]) + accounts.fake(string(p[2])) + string(p[3]))
		})
		out = regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(name)+`\b`).ReplaceAll(out, []byte("example"))
		if bytes.Contains(bytes.ToLower(out), []byte(strings.ToLower(name))) {
			return fmt.Errorf("%s still contains %q; scrub it by hand", f, name)
		}
		if !bytes.Equal(out, data) {
			if err := os.WriteFile(f, out, 0o600); err != nil { //nolint:gosec // G703: the recording the developer named
				return err
			}
			scrubbed++
		}
	}
	if err := accounts.save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Scrubbed %d of %d files; %d account IDs mapped in all.\n", scrubbed, len(files), len(accounts.fakes))
	return nil
}

// scrubUsers replaces every display name and email address in a JSON
// document (role members carry a user's name beside the user object), and
// drops the avatars of user objects (those with an accountId).
func scrubUsers(data []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if _, ok := t["displayName"]; ok {
				t["displayName"] = "Example User"
			}
			if _, ok := t["emailAddress"]; ok {
				t["emailAddress"] = "user@example.com"
			}
			if _, isUser := t["accountId"]; isUser {
				delete(t, "avatarUrls")
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// accountMap maps real account IDs to numbered fakes, kept between runs so
// every recording uses the same fake for the same account.
type accountMap struct {
	file  string
	fakes map[string]string
	order []string
}

func loadAccounts(file string) (*accountMap, error) {
	m := &accountMap{file: file, fakes: map[string]string{}}
	f, err := os.Open(file) //nolint:gosec // G304: the mapping the developer named
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		real, fake, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if ok {
			m.fakes[real] = fake
			m.order = append(m.order, real)
		}
	}
	return m, sc.Err()
}

func (m *accountMap) fake(real string) string {
	if strings.HasPrefix(real, "000000:00000000-0000-0000-0000-") {
		return real // already a fake
	}
	if f, ok := m.fakes[real]; ok {
		return f
	}
	f := fmt.Sprintf("000000:00000000-0000-0000-0000-%012d", len(m.fakes)+1)
	m.fakes[real] = f
	m.order = append(m.order, real)
	return f
}

func (m *accountMap) save() error {
	var b strings.Builder
	for _, real := range m.order {
		fmt.Fprintf(&b, "%s %s\n", real, m.fakes[real])
	}
	return os.WriteFile(m.file, []byte(b.String()), 0o600)
}
