// Package reader reads a space's configuration from Jira as spec-model
// values, one reader per kind of object. Export
// calls the readers in order and assembles a spec; apply calls the same
// readers between its writes, so what apply compares with is exactly what
// export would write.
//
// Readers take their inputs explicitly (the space, its issue types) and
// return values with their names resolved, bindings and IDs. Lookups a
// reader needs from the site (fields, resolutions, role types, events,
// groups, screens) are read once per Reader and shared.
//
// A Reader also keeps two logs in the order things were met, because the
// spec presents them in that order: notes (what could not be exported as it
// is) and the custom fields that readers named.
//
// It only reads: the client it is given cannot write.
package reader

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Marker identifies objects created by this tool. It is added to their
// description, and export takes it off again and writes mode: managed.
const Marker = "[managed-by: jira-space-config]"

// Reader reads from one site for one run. It is not safe for concurrent use.
type Reader struct {
	ctx context.Context
	c   *jira.Client

	notes []string

	// Site lookups, read on first use.
	siteFields      map[string]siteField // by field ID
	namedFields     map[string]bool      // fields whose display name has been noted
	customOrder     []string             // custom field IDs in the order readers named them
	resolutions     []spec.Resolution
	resolutionsRead bool
	roleTypes       []roleType
	roleTypesRead   bool
	eventNames      map[string]string
	groupNames      map[string]string
	screens         map[string]spec.Screen // by ID

	// statusNames are the names of statuses by ID: stored names from
	// SpaceStatuses, completed by the statuses workflows mention.
	statusNames map[string]string

	// lookupErr is the first failed name lookup. Lookups happen deep in
	// conversions that return names, so the reader that made one returns it
	// when it finishes, rather than going on with IDs it could have named.
	lookupErr error
}

// New returns a Reader for one run. ctx bounds every request it makes.
func New(ctx context.Context, c *jira.Client) *Reader {
	return &Reader{ctx: ctx, c: c, statusNames: map[string]string{}, screens: map[string]spec.Screen{}}
}

// Notes returns everything noted so far, in order: things the readers could
// not express in the format, or that the operator should know.
func (r *Reader) Notes() []string { return r.notes }

func (r *Reader) note(format string, args ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, args...))
}

func (r *Reader) get(path string, query url.Values, out any) error {
	return r.c.Get(r.ctx, path, query, out)
}

// lookupFailed records a failed name lookup; see Reader.lookupErr.
func (r *Reader) lookupFailed(err error) {
	if r.lookupErr == nil {
		r.lookupErr = err
	}
}

// done ends a reader: a failed lookup during it fails it.
func (r *Reader) done() error {
	err := r.lookupErr
	r.lookupErr = nil
	return err
}

// optText exports a description: empty means "not set", so it is omitted
// rather than written as an empty string.
func optText(s string) spec.Opt[string] {
	if s == "" {
		return spec.Opt[string]{}
	}
	return spec.Some(s)
}

// nullable exports an optional attribute: empty is written as null, so the
// export states that it is unset.
func nullable(s string) spec.Opt[string] {
	if s == "" {
		return spec.Opt[string]{Set: true, Null: true}
	}
	return spec.Some(s)
}

// ownership splits the marker off a description: objects carrying it were
// created by this tool and are exported as managed, everything else as reuse.
func ownership(description string) (spec.Opt[string], spec.Mode) {
	if clean, found := strings.CutSuffix(strings.TrimSpace(description), Marker); found {
		return optText(strings.TrimSpace(clean)), spec.ModeManaged
	}
	return optText(description), spec.ModeReuse
}

// globalOwnership is ownership for global objects: one this tool created is
// exported as ensure (created if missing), never managed, because global
// objects are never updated.
func globalOwnership(description string) (spec.Opt[string], spec.Mode) {
	d, mode := ownership(description)
	if mode == spec.ModeManaged {
		mode = spec.ModeEnsure
	}
	return d, mode
}

func containerRef(id, name, description string) *spec.ContainerRef {
	clean, mode := ownership(description)
	return &spec.ContainerRef{Name: name, ID: id, Mode: mode, Description: clean}
}

// sitePath strips the site from a URL Jira returns (icons and avatars), so
// the spec does not depend on the site it came from.
func sitePath(u string) string {
	if parsed, err := url.Parse(u); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return u
}

// avatarID extracts the avatar ID from an avatar URL (…/avatar/{id}).
func avatarID(u string) string {
	if m := avatarPattern.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

var avatarPattern = regexp.MustCompile(`/avatar/(\d+)`)

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKV(m map[string]string) []spec.KV {
	out := make([]spec.KV, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, spec.KV{Key: k, Value: m[k]})
	}
	return out
}
