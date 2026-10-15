package jira

import "strings"

// ID is an ID Jira gives as a string in some answers and a number in others
// (a field scheme's: a number in the list, a string when read alone).
type ID string

// UnmarshalJSON takes the ID as a string or a number.
func (id *ID) UnmarshalJSON(b []byte) error {
	*id = ID(strings.Trim(string(b), `"`))
	return nil
}
