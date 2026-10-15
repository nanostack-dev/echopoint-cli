package commands

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// keysetRow is one row of a keysetTable: key is the sort value, which rows may
// share, and id the unique tiebreak. item is the row as the API shows it.
type keysetRow struct {
	key  int
	id   uuid.UUID
	item any
}

// keysetTable answers a POST search like Echopoint does: rows ordered by key
// DESC then id ASC, a page of `limit` rows, and an opaque cursor that holds the
// last row's (key, id) and continues strictly after it, so rows that tie on key
// across a page boundary are neither skipped nor repeated, and rows added
// between two pages change nothing of the pages still to come. A cursor sent
// with a non-zero offset, or one the table did not issue, is 400 INVALID_CURSOR.
type keysetTable struct {
	mu   sync.Mutex
	rows []keysetRow
	// pagination records the pagination object of every request.
	pagination []map[string]any
	// afterPage runs after the n-th (1-based) page was answered, with the table unlocked.
	afterPage func(n int, table *keysetTable)
}

func newKeysetTable(rows ...keysetRow) *keysetTable { return &keysetTable{rows: rows} }

func (table *keysetTable) insert(row keysetRow) {
	table.mu.Lock()
	defer table.mu.Unlock()
	table.rows = append(table.rows, row)
}

func (table *keysetTable) requests() []map[string]any {
	table.mu.Lock()
	defer table.mu.Unlock()
	return slices.Clone(table.pagination)
}

func encodeCursor(row keysetRow) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(row.key) + "|" + row.id.String()))
}

func (table *keysetTable) handle(w http.ResponseWriter, body []byte) {
	var request struct {
		Pagination map[string]any `json:"pagination"`
	}
	_ = json.Unmarshal(body, &request)

	table.mu.Lock()
	table.pagination = append(table.pagination, request.Pagination)
	page := len(table.pagination)
	rows := slices.Clone(table.rows)
	table.mu.Unlock()

	slices.SortFunc(rows, func(a, b keysetRow) int {
		if a.key != b.key {
			return b.key - a.key
		}
		return strings.Compare(a.id.String(), b.id.String())
	})

	limit, offset, cursor := 20, 0, ""
	if value, ok := request.Pagination["limit"].(float64); ok {
		limit = int(value)
	}
	if value, ok := request.Pagination["offset"].(float64); ok {
		offset = int(value)
	}
	if value, ok := request.Pagination["cursor"].(string); ok {
		cursor = value
	}

	start := offset
	if cursor != "" {
		after := slices.IndexFunc(rows, func(row keysetRow) bool { return encodeCursor(row) == cursor })
		if offset != 0 || after < 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"code": "INVALID_CURSOR", "message": "the cursor is not valid"}},
			})
			return
		}
		start = after + 1
	}
	start = min(start, len(rows))
	end := min(start+limit, len(rows))

	items := []any{}
	for _, row := range rows[start:end] {
		items = append(items, row.item)
	}
	response := map[string]any{"count": len(items), "total": len(rows), "items": items}
	if end < len(rows) {
		response["next_cursor"] = encodeCursor(rows[end-1])
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)

	if table.afterPage != nil {
		table.afterPage(page, table)
	}
}

// tiedRows makes n rows whose keys fall in groups of `tie` rows sharing one key,
// highest key first, so a page boundary can fall inside a group. The item of a row
// is made by item from its id and position.
func tiedRows(n, tie int, item func(id uuid.UUID, position int) any) []keysetRow {
	rows := make([]keysetRow, 0, n)
	for i := range n {
		id := idFor(strconv.Itoa(1000 + i))
		rows = append(rows, keysetRow{key: 1000 - i/tie, id: id, item: item(id, i)})
	}
	return rows
}

// requireNoOffset fails the test when any request carried a non-zero offset.
func requireNoOffset(t *testing.T, table *keysetTable) {
	t.Helper()
	for i, pagination := range table.requests() {
		if offset, _ := pagination["offset"].(float64); offset != 0 {
			t.Errorf("request %d paged by offset %v, want cursors only", i+1, offset)
		}
	}
}
