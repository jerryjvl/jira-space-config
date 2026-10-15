// Command jsc-sandbox is a development tool for the test site: it creates
// the empty company-managed spaces that live apply runs are tested in. The
// tool itself never creates spaces; this command is not part of it, and is only for a site with no real data.
//
//	go run ./cmd/jsc-sandbox --site https://example.atlassian.net create-space KEY "Name"
//	go run ./cmd/jsc-sandbox --site https://example.atlassian.net update-status ID "Name" CATEGORY "Description"
//
// update-status repairs a test status (CATEGORY is TODO, IN_PROGRESS or DONE).
//
//	go run ./cmd/jsc-sandbox --site https://example.atlassian.net delete-field-scheme ID
//
//	go run ./cmd/jsc-sandbox --site https://example.atlassian.net delete-space KEY "Name"
//
// delete-space deletes a test space for good (not to the trash), so its key is
// free again. Name must be the space's current name, as a check that it is the
// one meant.
//
// delete-field-scheme removes a field scheme the tool made (its description has
// the marker), left behind by a failed test run; it refuses any other.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
	"github.com/jerryjvl/jira-space-config/internal/jira"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	site := flag.String("site", "", "the test site (required)")
	flag.Parse()
	switch {
	case *site != "" && flag.NArg() == 3 && flag.Arg(0) == "create-space":
	case *site != "" && flag.NArg() == 5 && flag.Arg(0) == "update-status":
	case *site != "" && flag.NArg() == 2 && flag.Arg(0) == "delete-field-scheme":
	case *site != "" && flag.NArg() == 3 && flag.Arg(0) == "delete-space":
	default:
		return errors.New(`usage: jsc-sandbox --site URL create-space KEY "Name" | update-status ID "Name" CATEGORY "Description" | delete-field-scheme ID | delete-space KEY "Name"`)
	}
	key, name := flag.Arg(1), flag.Arg(2)
	parsed, err := jira.ParseSite(*site)
	if err != nil {
		return err
	}
	creds, err := credentials.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx := context.Background()
	opts := jira.Options{Site: parsed, Credentials: creds, UserAgent: "jsc-sandbox"}
	w, err := jira.NewWriter(ctx, opts, key, "")
	if err != nil {
		return err
	}
	if flag.Arg(0) == "delete-space" {
		var p struct {
			Name string `json:"name"`
		}
		if err := w.Get(ctx, "/rest/api/3/project/"+key, nil, &p); err != nil {
			return err
		}
		if p.Name != name {
			return fmt.Errorf("space %s is named %q, not %q; not deleting it", key, p.Name, name)
		}
		w.Allow(http.MethodDelete, "/rest/api/3/project/{projectIdOrKey}")
		if err := w.Send(ctx, http.MethodDelete, "/rest/api/3/project/"+key, url.Values{"enableUndo": {"false"}}, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Deleted space %s (%q) for good; %d requests.\n", key, name, w.Requests())
		return nil
	}
	if flag.Arg(0) == "delete-field-scheme" {
		var scheme struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := w.Get(ctx, "/rest/api/3/config/fieldschemes/"+key, nil, &scheme); err != nil {
			return err
		}
		if !strings.Contains(scheme.Description, "[managed-by: jira-space-config]") {
			return fmt.Errorf("field scheme %s (%q) was not made by the tool; not deleting it", key, scheme.Name)
		}
		w.Allow(http.MethodDelete, "/rest/api/3/config/fieldschemes/{id}")
		if err := w.Send(ctx, http.MethodDelete, "/rest/api/3/config/fieldschemes/"+key, nil, nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Deleted field scheme %s (%q); %d requests.\n", key, scheme.Name, w.Requests())
		return nil
	}
	if flag.Arg(0) == "update-status" {
		w.Allow(http.MethodPut, "/rest/api/3/statuses")
		body := map[string]any{"statuses": []map[string]string{{"id": key, "name": name, "statusCategory": flag.Arg(3), "description": flag.Arg(4)}}}
		if err := w.Send(ctx, http.MethodPut, "/rest/api/3/statuses", nil, body, nil); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Updated status %s; %d requests.\n", key, w.Requests())
		return nil
	}
	var me struct {
		AccountID string `json:"accountId"`
	}
	if err := w.Get(ctx, "/rest/api/3/myself", nil, &me); err != nil {
		return err
	}
	w.Allow(http.MethodPost, "/rest/api/3/project")
	var created struct {
		ID  int    `json:"id"`
		Key string `json:"key"`
	}
	body := map[string]string{"key": key, "name": name, "leadAccountId": me.AccountID, "projectTypeKey": "software",
		"projectTemplateKey": "com.pyxis.greenhopper.jira:gh-simplified-kanban-classic"}
	if err := w.Send(ctx, http.MethodPost, "/rest/api/3/project", nil, body, &created); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Created company-managed space %s (id %d); %d requests.\n", created.Key, created.ID, w.Requests())
	return nil
}
