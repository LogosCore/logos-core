package models

import (
	"fmt"
	"io"
	"strings"
)

// WikiDocumentStatus is a page's lifecycle state.
//
// The zero value is the empty string and means stable: pages created before
// this field existed, and pages whose author never set a status, are
// stable by default. Use Or() rather than comparing directly.
type WikiDocumentStatus string

const (
	WikiDocumentStatusDraft      WikiDocumentStatus = "draft"
	WikiDocumentStatusStable     WikiDocumentStatus = "stable"
	WikiDocumentStatusDeprecated WikiDocumentStatus = "deprecated"
)

// Or resolves the zero value to stable.
func (s WikiDocumentStatus) Or() WikiDocumentStatus {
	if s == "" {
		return WikiDocumentStatusStable
	}
	return s
}

// Valid reports whether the status is one this build knows.
func (s WikiDocumentStatus) Valid() bool {
	switch s.Or() {
	case WikiDocumentStatusDraft, WikiDocumentStatusStable, WikiDocumentStatusDeprecated:
		return true
	default:
		return false
	}
}

// MarshalGQL writes the status as an uppercase quoted string for GraphQL.
func (s WikiDocumentStatus) MarshalGQL(w io.Writer) {
	fmt.Fprintf(w, "%q", strings.ToUpper(string(s.Or())))
}

// UnmarshalGQL reads the status from a GraphQL uppercase string.
func (s *WikiDocumentStatus) UnmarshalGQL(v interface{}) error {
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("WikiDocumentStatus must be a string")
	}
	parsed := WikiDocumentStatus(strings.ToLower(str))
	if !parsed.Valid() {
		return fmt.Errorf("invalid WikiDocumentStatus: %s", str)
	}
	*s = parsed.Or()
	return nil
}
