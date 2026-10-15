package spec

// HolderType is how Jira spells one of the format's holder kinds in each
// place a holder is used. "" means that place has no such kind. It is the
// one table export and apply both use: export reads Jira's spelling into the
// kind, apply writes the kind as Jira's spelling.
type HolderType struct {
	Permission   string // in a permission scheme
	Notification string // as a notification recipient
	Security     string // as an issue security level member
	Value        bool   // the kind takes a value (role:<name>); application_role may omit it
}

// HolderTypes are the holder kinds of the format, by name.
var HolderTypes = map[string]HolderType{
	"role":             {"projectRole", "ProjectRole", "projectrole", true},
	"group":            {"group", "Group", "group", true},
	"user":             {"user", "User", "user", true},
	"reporter":         {"reporter", "Reporter", "reporter", false},
	"current_assignee": {"assignee", "CurrentAssignee", "assignee", false},
	"space_lead":       {"projectLead", "ProjectLead", "", false}, // Jira refuses projectLead in a security level
	"application_role": {"applicationRole", "", "applicationRole", true},
	"user_field":       {"userCustomField", "UserCustomField", "", true},
	"group_field":      {"groupCustomField", "GroupCustomField", "", true},
	"anyone":           {"anyone", "", "", false},
	"current_user":     {"", "CurrentUser", "", false},
	"component_lead":   {"", "ComponentLead", "", false},
	"all_watchers":     {"", "AllWatchers", "", false},
	"email":            {"", "EmailAddress", "", true},
}

// The places a holder is used, for HolderType.In.
const (
	PlacePermission = iota
	PlaceNotification
	PlaceSecurity
)

// In is Jira's spelling of the kind in a place, "" if the place has none.
func (t HolderType) In(place int) string {
	return [...]string{t.Permission, t.Notification, t.Security}[place]
}
