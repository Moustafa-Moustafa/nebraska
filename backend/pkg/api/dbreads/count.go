package dbreads

import (
	"github.com/doug-martin/goqu/v9"
)

// GetCountQuery executes a SELECT COUNT(*) (or equivalent) goqu query and
// returns the int count. Used by many list-style reads that pair a page query
// with a count query.
func (q *Queries) GetCountQuery(query *goqu.SelectDataset) (int, error) {
	sql, _, err := query.ToSQL()
	if err != nil {
		return 0, err
	}
	count := 0
	if err := q.db.QueryRow(sql).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
