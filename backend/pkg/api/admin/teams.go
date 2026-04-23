package admin

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

// UpdateTeam updates an existing team.
func (s *Service) UpdateTeam(team *api.Team) error {
	query, _, err := goqu.Update("team").
		Set(goqu.Record{"name": team.Name}).
		Where(goqu.C("id").Eq(team.ID)).
		ToSQL()
	if err != nil {
		return err
	}
	result, err := s.db.Exec(query)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return api.ErrNoRowsAffected
	}

	return nil
}

// AddTeam registers a team.
func (s *Service) AddTeam(team *api.Team) (*api.Team, error) {
	var query *goqu.InsertDataset
	if team.ID != "" {
		query = goqu.Insert("team").
			Cols("id", "name").
			Vals(goqu.Vals{team.ID, team.Name}).
			Returning(goqu.T("team").All())
	} else {
		query = goqu.Insert("team").
			Cols("name").
			Vals(goqu.Vals{team.Name}).
			Returning(goqu.T("team").All())
	}
	insertQuery, _, err := query.ToSQL()
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowx(insertQuery).StructScan(team)
	if err != nil {
		return nil, err
	}
	return team, err
}
