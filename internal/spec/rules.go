package spec

import "slices"

// Rule lists on a transition.
const (
	ListConditions    = "conditions"
	ListValidators    = "validators"
	ListPostFunctions = "post_functions"
	ListTriggers      = "triggers"
)

// RuleOpaque is the type of a rule kept exactly as Jira returns it.
const RuleOpaque = "rule"

// Conversions between a typed parameter and Jira's string parameter.
const (
	ConvPlain    = ""          // the string as is
	ConvCSV      = "csv"       // a list, comma-separated in Jira
	ConvBool     = "bool"      // true/false
	ConvJSONList = "json-list" // a list, as a JSON array in a string
	ConvEnum     = "enum"      // one of Values (Jira value → typed value)
)

// What an ID-bearing parameter refers to; IDs are shown as names.
const (
	RefNone   = ""
	RefField  = "field"
	RefStatus = "status"
	RefRole   = "role"
	RefGroup  = "group"
	RefUser   = "user"
)

// ParamSpec describes one typed rule parameter and its Jira counterpart.
type ParamSpec struct {
	Key      string
	Kind     ParamKind
	Required bool
	Enum     []string

	Jira   string            // Jira's parameter name
	Conv   string            // Conv* constant
	Ref    string            // Ref* constant: IDs in Jira, names in the spec
	Values map[string]string // ConvEnum: Jira value → typed value
}

// RuleSpec describes one typed rule: where it may appear, which Jira rule it
// maps to, and its parameters in canonical order. Together with Fixed, the
// rule key identifies the typed rule uniquely, which makes typed ↔ Jira a
// bijection: a rule read and written back is unchanged.
type RuleSpec struct {
	List    string
	Type    string
	RuleKey string
	// Fixed are Jira parameters with set values that tell typed rules
	// sharing a rule key apart (the ruleType of system:validate-field-value,
	// the empty value of a cleared field).
	Fixed  map[string]string
	Params []ParamSpec
	// AnyOf requires at least one of these parameters.
	AnyOf []string
}

var comparators = []string{">", ">=", "=", "<=", "<", "!="}

func str(key, jira string, required bool) ParamSpec {
	return ParamSpec{Key: key, Kind: KindString, Required: required, Jira: jira}
}
func ref(key, jira, to string, required bool) ParamSpec {
	return ParamSpec{Key: key, Kind: KindString, Required: required, Jira: jira, Ref: to}
}
func refs(key, jira, to string, required bool) ParamSpec {
	return ParamSpec{Key: key, Kind: KindList, Required: required, Jira: jira, Conv: ConvCSV, Ref: to}
}
func boolean(key, jira string) ParamSpec {
	return ParamSpec{Key: key, Kind: KindBool, Jira: jira, Conv: ConvBool}
}
func enum(key, jira string, required bool, values map[string]string) ParamSpec {
	p := ParamSpec{Key: key, Kind: KindString, Required: required, Jira: jira, Conv: ConvEnum, Values: values}
	for _, v := range values {
		p.Enum = append(p.Enum, v)
	}
	slices.Sort(p.Enum)
	return p
}
func comparator(key, jira string) ParamSpec {
	values := map[string]string{}
	for _, c := range comparators {
		values[c] = c
	}
	return enum(key, jira, true, values)
}

// ruleRegistry is every typed rule, from the Atlassian rule catalogue in the
// description of GET /rest/api/3/workflows/capabilities (see
// docs/api/inventory.md, section 7).
var ruleRegistry = []RuleSpec{
	// Conditions.
	{List: ListConditions, Type: "restrict_to", RuleKey: "system:restrict-issue-transition",
		Params: []ParamSpec{refs("roles", "roleIds", RefRole, false), refs("groups", "groupIds", RefGroup, false),
			{Key: "permissions", Kind: KindList, Jira: "permissionKeys", Conv: ConvCSV},
			refs("group_fields", "groupCustomFields", RefField, false), refs("user_fields", "allowUserCustomFields", RefField, false),
			refs("deny_user_fields", "denyUserCustomFields", RefField, false),
			// Account IDs, or allow-assignee / allow-reporter; Jira documents these
			// for team-managed spaces, but returns the (empty) parameter everywhere.
			refs("accounts", "accountIds", RefUser, false)},
		AnyOf: []string{"roles", "groups", "permissions", "group_fields", "user_fields", "deny_user_fields", "accounts"}},
	{List: ListConditions, Type: "field_value", RuleKey: "system:check-field-value",
		Params: []ParamSpec{ref("field", "fieldId", RefField, true),
			{Key: "values", Kind: KindList, Required: true, Jira: "fieldValue", Conv: ConvJSONList},
			comparator("comparator", "comparator"),
			enum("comparison", "comparisonType", true, map[string]string{"STRING": "string", "NUMBER": "number",
				"DATE": "date", "DATE_WITHOUT_TIME": "date_without_time", "OPTIONID": "option"})}},
	{List: ListConditions, Type: "previous_status", RuleKey: "system:previous-status-condition",
		Params: []ParamSpec{ref("status", "previousStatusIds", RefStatus, true), boolean("not", "not"),
			boolean("most_recent_only", "mostRecentStatusOnly"), boolean("include_current", "includeCurrentStatus"),
			boolean("ignore_loops", "ignoreLoopTransitions")}},
	{List: ListConditions, Type: "parent_child_blocking", RuleKey: "system:parent-or-child-blocking-condition",
		Fixed:  map[string]string{"blocker": "CHILD"},
		Params: []ParamSpec{refs("statuses", "statusIds", RefStatus, true)}},
	{List: ListConditions, Type: "separation_of_duties", RuleKey: "system:separation-of-duties",
		Params: []ParamSpec{ref("from_status", "fromStatusId", RefStatus, false), ref("to_status", "toStatusId", RefStatus, false)},
		AnyOf:  []string{"from_status", "to_status"}},
	{List: ListConditions, Type: "block_all", RuleKey: "system:restrict-from-all-users",
		Params: []ParamSpec{enum("mode", "restrictMode", true, map[string]string{"users": "users", "usersAndAPI": "users_and_api"})}},
	{List: ListConditions, Type: "block_in_progress_approval", RuleKey: "system:block-in-progress-approval"},

	// Validators.
	{List: ListValidators, Type: "permission", RuleKey: "system:check-permission-validator",
		Params: []ParamSpec{str("permission", "permissionKey", true)}},
	{List: ListValidators, Type: "parent_child_blocking", RuleKey: "system:parent-or-child-blocking-validator",
		Fixed:  map[string]string{"blocker": "PARENT"},
		Params: []ParamSpec{refs("statuses", "statusIds", RefStatus, true)}},
	{List: ListValidators, Type: "previous_status", RuleKey: "system:previous-status-validator",
		Params: []ParamSpec{ref("status", "previousStatusIds", RefStatus, true), boolean("most_recent_only", "mostRecentStatusOnly")}},
	{List: ListValidators, Type: "fields_required", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "fieldRequired"},
		Params: []ParamSpec{refs("fields", "fieldsRequired", RefField, true), boolean("ignore_context", "ignoreContext"),
			str("error_message", "errorMessage", false)}},
	{List: ListValidators, Type: "field_changed", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "fieldChanged"},
		Params: []ParamSpec{ref("field", "fieldKey", RefField, true), refs("exempt_groups", "groupsExemptFromValidation", RefGroup, false),
			str("error_message", "errorMessage", false)}},
	{List: ListValidators, Type: "field_single_value", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "fieldHasSingleValue"},
		Params: []ParamSpec{ref("field", "fieldKey", RefField, true), boolean("exclude_subtasks", "excludeSubtasks")}},
	{List: ListValidators, Type: "field_matches", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "fieldMatchesRegularExpression"},
		Params: []ParamSpec{ref("field", "fieldKey", RefField, true), str("regexp", "regexp", true), str("error_message", "errorMessage", false)}},
	{List: ListValidators, Type: "date_compare", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "dateFieldComparison"},
		Params: []ParamSpec{ref("field", "date1FieldKey", RefField, true), comparator("comparator", "conditionSelected"),
			ref("other_field", "date2FieldKey", RefField, true), boolean("include_time", "includeTime")}},
	{List: ListValidators, Type: "date_window", RuleKey: "system:validate-field-value", Fixed: map[string]string{"ruleType": "windowDateComparison"},
		Params: []ParamSpec{ref("field", "date1FieldKey", RefField, true), ref("other_field", "date2FieldKey", RefField, true),
			str("days", "numberOfDays", true)}},
	{List: ListValidators, Type: "forms_attached", RuleKey: "system:proforma-forms-attached"},
	{List: ListValidators, Type: "forms_submitted", RuleKey: "system:proforma-forms-submitted"},

	// Post functions. clear_field must come before set_field: both are
	// system:update-field, and clear_field is the one with fixed values.
	{List: ListPostFunctions, Type: "assign", RuleKey: "system:change-assignee",
		Params: []ParamSpec{enum("to", "type", true, map[string]string{"to-selected-user": "selected_user", "to-unassigned": "unassigned",
			"to-current-user": "current_user", "to-default-user": "default_user"}), ref("user", "accountId", RefUser, false)}},
	{List: ListPostFunctions, Type: "copy_field", RuleKey: "system:copy-value-from-other-field",
		Params: []ParamSpec{ref("from", "sourceFieldKey", RefField, true), ref("to", "targetFieldKey", RefField, true),
			enum("source", "issueSource", false, map[string]string{"SAME": "same", "PARENT": "parent"})}},
	{List: ListPostFunctions, Type: "clear_field", RuleKey: "system:update-field", Fixed: map[string]string{"value": "", "mode": ""},
		Params: []ParamSpec{ref("field", "field", RefField, true)}},
	{List: ListPostFunctions, Type: "set_field", RuleKey: "system:update-field",
		Params: []ParamSpec{ref("field", "field", RefField, true), str("value", "value", true),
			enum("mode", "mode", false, map[string]string{"replace": "replace", "append": "append"})}},
	{List: ListPostFunctions, Type: "trigger_webhook", RuleKey: "system:trigger-webhook",
		Params: []ParamSpec{str("webhook", "webhookId", true)}},
	{List: ListPostFunctions, Type: "trigger_agent", RuleKey: "system:trigger-agent",
		Params: []ParamSpec{ref("agent", "agentId", RefUser, true), str("prompt", "promptValue", false)}},
}

// opaqueRule is accepted in every list.
var opaqueRule = RuleSpec{Type: RuleOpaque, Params: []ParamSpec{
	{Key: "key", Kind: KindString, Required: true},
	{Key: "parameters", Kind: KindMap},
}}

// extraParam holds parameters of a known rule that the typed form does not
// name, so that typed → Jira → typed loses nothing.
var extraParam = ParamSpec{Key: "extra", Kind: KindMap}

// lookupRule finds the typed rule for a list, or the opaque rule.
func lookupRule(list, typ string) (RuleSpec, bool) {
	if typ == RuleOpaque {
		return opaqueRule, true
	}
	for _, r := range ruleRegistry {
		if r.List == list && r.Type == typ {
			r.Params = append(slices.Clone(r.Params), extraParam)
			return r, true
		}
	}
	return RuleSpec{}, false
}

// RulesFor returns the typed rules for a Jira rule key in a list, in
// registry order (more specific first). Used by export.
func RulesFor(list, ruleKey string) []RuleSpec {
	var out []RuleSpec
	for _, r := range ruleRegistry {
		if r.List == list && r.RuleKey == ruleKey {
			out = append(out, r)
		}
	}
	return out
}

// RuleKeys returns every Jira rule key the registry maps.
func RuleKeys() []string {
	var keys []string
	for _, r := range ruleRegistry {
		if !slices.Contains(keys, r.RuleKey) {
			keys = append(keys, r.RuleKey)
		}
	}
	return keys
}

// ruleTypes lists the typed rules allowed in a list, for error messages.
func ruleTypes(list string) []string {
	var types []string
	for _, r := range ruleRegistry {
		if r.List == list {
			types = append(types, r.Type)
		}
	}
	return append(types, RuleOpaque)
}

// RuleSpecFor returns the typed rule of a type in a list, for writing a
// rule back in Jira's form.
func RuleSpecFor(list, typ string) (RuleSpec, bool) { return lookupRule(list, typ) }
