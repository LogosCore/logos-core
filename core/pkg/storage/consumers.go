package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// ErrBadCursor is a consumers cursor that does not read.
var ErrBadCursor = errors.New("invalid cursor")

// consumerCursor is a consumer's place in its bucket's list (the largest
// first): its bytes, operation's name and key, so a page follows the one
// before it whatever the snapshot between them added or dropped.
type consumerCursor struct {
	Bytes int64  `json:"b"`
	Name  string `json:"n"`
	Key   string `json:"k"`
}

// ConsumerCursor is c's place in its bucket's list.
func ConsumerCursor(c Consumer) string {
	return encodeCursor(consumerCursor{Bytes: c.Bytes, Name: c.OperationName, Key: c.key()})
}

// encodeCursor is a place in a list as a cursor says it.
func encodeCursor(c consumerCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// parseCursor reads a cursor: nil for none (""), ErrBadCursor for one that
// does not read.
func parseCursor(after string) (*consumerCursor, error) {
	if after == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil {
		return nil, ErrBadCursor
	}
	var c consumerCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, ErrBadCursor
	}
	return &c, nil
}

// ConsumerPage is a page of a bucket's consumers.
type ConsumerPage struct {
	Consumers []Consumer
	// Total is how many match; More whether more follow the page.
	Total int
	More  bool
}

// Consumers is a page of what core keeps in bucket, the largest first: at
// most first after the cursor after (none: from the start); search keeps
// those whose operation's name or kind holds it.
func (s Snapshot) Consumers(bucket, search, after string, first int) (ConsumerPage, error) {
	from, err := parseCursor(after)
	if err != nil {
		return ConsumerPage{}, err
	}
	q := strings.ToLower(strings.TrimSpace(search))
	page := ConsumerPage{Consumers: []Consumer{}}
	for _, c := range s.consumers[bucket] {
		if q != "" && !strings.Contains(strings.ToLower(c.OperationName), q) && !strings.Contains(c.Kind, q) {
			continue
		}
		page.Total++
		if from != nil && !follows(c.Bytes, c.OperationName, c.key(), *from) {
			continue
		}
		if len(page.Consumers) == first {
			page.More = true
			continue
		}
		page.Consumers = append(page.Consumers, c)
	}
	return page, nil
}

// follows reports whether what has bytes, name and key comes after cursor
// in a list the most bytes first, then by name and key (compareConsumers,
// compareCleanups).
func follows(bytes int64, name, key string, cursor consumerCursor) bool {
	switch {
	case bytes != cursor.Bytes:
		return bytes < cursor.Bytes
	case name != cursor.Name:
		return name > cursor.Name
	}
	return key > cursor.Key
}
