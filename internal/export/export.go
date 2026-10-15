// Package export builds a spec from a space's configuration in Jira, in
// canonical form, such that a dry run of it against the same space finds
// nothing to do. It calls the readers in
// internal/reader in order and assembles what they return; it only reads.
package export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Options configure an export.
type Options struct {
	ToolVersion string
	Now         func() time.Time // time.Now if nil; tests fix it
}

// Space exports the configuration of one company-managed space.
func Space(ctx context.Context, c *jira.Client, spaceKey string, opts Options) (*spec.Spec, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	info, err := c.TenantInfo(ctx)
	if err != nil {
		return nil, err
	}
	s := &spec.Spec{APIVersion: spec.APIVersion, Kind: spec.Kind, Binding: &spec.SiteBinding{
		Site:        c.Site().String(),
		CloudID:     info.CloudID,
		WrittenAt:   opts.Now().UTC().Format(time.RFC3339),
		ToolVersion: opts.ToolVersion,
	}}
	if err := assemble(reader.New(ctx, c), spaceKey, s); err != nil {
		return nil, err
	}
	s.Metadata = spec.Metadata{
		Name:        strings.ToLower(spaceKey) + "-export",
		Description: spec.Some(fmt.Sprintf("Exported from %s, space %s.", c.Site(), spaceKey)),
	}
	return s, nil
}

// assemble reads the space into s. The order of the reads is the order
// notes and custom fields appear in the spec.
func assemble(r *reader.Reader, spaceKey string, s *spec.Spec) error {
	p, err := r.Space(spaceKey)
	if err != nil {
		return err
	}
	its, err := r.IssueTypeScheme(p)
	if err != nil {
		return err
	}
	s.IssueTypes = its.Definitions
	s.Space = &spec.Space{Key: p.Key, ID: p.ID, Details: p.Details, IssueTypes: its.Space,
		Binding: &spec.SpaceBinding{IssueTypeScheme: its.Binding}}
	types := s.Space.IssueTypes // per issue type, in the order the readers return

	if s.Statuses, err = r.SpaceStatuses(p); err != nil {
		return err
	}
	if s.Resolutions, err = r.Resolutions(); err != nil {
		return err
	}
	prios, err := r.PriorityScheme(p)
	if err != nil {
		return err
	}
	s.Priorities = prios.Definitions
	s.Space.Priorities = spec.Section[spec.SpacePriorities]{Set: true, V: prios.Space}

	screens, err := r.ScreenScheme(p, its)
	if err != nil {
		return err
	}
	s.Screens = screens.Definitions
	s.Space.Binding.IssueTypeScreenScheme = screens.Binding
	for i := range types {
		types[i].Screens = screens.Screens[i]
		types[i].Binding = &spec.IssueTypeBinding{ScreenScheme: screens.ScreenSchemes[i]}
	}

	fields, err := r.Fields(p, its)
	if err != nil {
		return err
	}
	s.Binding.FieldBackend = fields.Backend
	s.Space.Fields = spec.Section[[]spec.SpaceField]{Set: true, V: fields.Fields}
	s.Space.Binding.FieldScheme = fields.FieldScheme
	s.Space.Binding.FieldConfigurationScheme = fields.FieldConfigurationScheme
	for i, ref := range fields.FieldConfigurations {
		types[i].Binding.FieldConfiguration = ref
	}

	// Contexts are exported for the custom fields the space has: those on its
	// screens and in its fields, not those only workflow rules or permissions mention.
	if s.Space.FieldContexts, err = r.FieldContexts(p, its, r.CustomFields()); err != nil {
		return err
	}

	assignment, err := r.WorkflowScheme(p, its)
	if err != nil {
		return err
	}
	workflows, err := r.Workflows(assignment.Names)
	if err != nil {
		return err
	}
	s.Workflows = workflows.Workflows
	s.Space.Binding.WorkflowScheme = assignment.Binding
	for i, name := range assignment.Workflows {
		types[i].Workflow = spec.Some(name)
	}
	for _, sc := range workflows.Screens {
		if !hasScreen(s.Screens, sc.ID) {
			s.Screens = append(s.Screens, sc) // a transition screen that is not one of the space's own
		}
	}

	roles, err := r.Roles(p)
	if err != nil {
		return err
	}
	s.Roles, s.Space.RoleMembers = roles.Definitions, roles.Members
	perms, err := r.PermissionScheme(p)
	if err != nil {
		return err
	}
	s.Space.Permissions = spec.Section[spec.Permissions]{Set: true, V: *perms}
	notifications, err := r.NotificationScheme(p)
	if err != nil {
		return err
	}
	if notifications == nil {
		s.Space.Notifications = spec.Section[spec.Notifications]{Set: true, Null: true}
	} else {
		s.Space.Notifications = spec.Section[spec.Notifications]{Set: true, V: *notifications}
	}
	security, err := r.IssueSecurity(p)
	if err != nil {
		return err
	}
	if security == nil {
		s.Space.IssueSecurity = spec.Section[spec.IssueSecurity]{Set: true, Null: true}
	} else {
		s.Space.IssueSecurity = spec.Section[spec.IssueSecurity]{Set: true, V: *security}
	}
	if s.Space.Components, err = r.Components(p); err != nil {
		return err
	}
	if s.Space.Versions, err = r.Versions(p); err != nil {
		return err
	}
	if s.Space.Properties, err = r.Properties(p); err != nil {
		return err
	}
	if s.Filters, s.Boards, err = r.BoardsAndFilters(p); err != nil {
		return err
	}
	if s.AutomationRules, err = r.AutomationRules(p); err != nil {
		return err
	}

	s.CustomFields = r.CustomFields()
	s.Notes = r.Notes()
	if prefix, ok := managedPrefix(spec.ManagedNames(s)); ok {
		s.Space.ManagedPrefix = prefix
	} else {
		s.Notes = append(s.Notes, "The space has objects the tool manages whose names share no prefix, so "+
			"space.managed_prefix is not set; set it, and change the mode of objects without it to reuse.")
	}
	return nil
}

// managedPrefix works out the prefix the managed objects were named with.
// A scheme apply generated gives it exactly ("JSC3" from "JSC3 Field
// Scheme"); otherwise it is the leading words all the names share. It is
// false when the names share no prefix.
func managedPrefix(names []string) (string, bool) {
	if len(names) == 0 {
		return "", true
	}
	sharedBy := func(prefix string) bool {
		for _, n := range names {
			if !strings.HasPrefix(n, prefix+" ") {
				return false
			}
		}
		return prefix != ""
	}
	for _, n := range names {
		for _, kind := range spec.GeneratedSchemes {
			if p, ok := strings.CutSuffix(n, " "+kind); ok && sharedBy(p) {
				return p, true
			}
		}
	}
	words := strings.Fields(names[0])
	for i := len(words) - 1; i > 0; i-- {
		if p := strings.Join(words[:i], " "); sharedBy(p) {
			return p, true
		}
	}
	return "", false
}

func hasScreen(list []spec.Screen, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}
