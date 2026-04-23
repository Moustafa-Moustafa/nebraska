package dbreads

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func (q *Queries) GetTeams() ([]*api.Team, error) {
	var teams []*api.Team
	query, _, err := goqu.From("team").
		Select("id", "name", "created_ts").
		Order(goqu.C("name").Asc()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var team api.Team
		err := rows.StructScan(&team)
		if err != nil {
			return nil, err
		}
		teams = append(teams, &team)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return teams, nil
}

func (q *Queries) GetTeam() (*api.Team, error) {
	var team = &api.Team{}
	query, _, err := goqu.From("team").
		Select("id", "name", "created_ts").
		Limit(1).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(team)
	if err != nil {
		return nil, err
	}
	return team, nil
}
