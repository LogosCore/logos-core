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
	b, _ := json.Marshal(consumerCursor{Bytes: c.Bytes, Name: c.OperationName, Key: c.key()})
	return base64.RawURLEncoding.EncodeToString(b)
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
	var from *consumerCursor
	if after != "" {
		raw, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil {
			return ConsumerPage{}, ErrBadCursor
		}
		var c consumerCursor
		if err := json.Unmarshal(raw, &c); err != nil {
			return ConsumerPage{}, ErrBadCursor
		}
		from = &c
	}
	q := strings.ToLower(strings.TrimSpace(search))
	page := ConsumerPage{Consumers: []Consumer{}}
	for _, c := range s.consumers[bucket] {
		if q != "" && !strings.Contains(strings.ToLower(c.OperationName), q) && !strings.Contains(c.Kind, q) {
			continue
		}
		page.Total++
		if from != nil && !follows(c, *from) {
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

// follows reports whether c comes after cursor in a bucket's order
// (compareConsumers).
func follows(c Consumer, cursor consumerCursor) bool {
	switch {
	case c.Bytes != cursor.Bytes:
		return c.Bytes < cursor.Bytes
	case c.OperationName != cursor.Name:
		return c.OperationName > cursor.Name
	}
	return c.key() > cursor.Key
}
