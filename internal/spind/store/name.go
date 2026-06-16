package store

import "regexp"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ValidName(name string) bool {
	return validName.MatchString(name) && name != "." && name != ".."
}
