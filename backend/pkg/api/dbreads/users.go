package dbreads

import (
	"crypto/md5"
	"fmt"
	"io"

	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

func (q *Queries) GetUser(username string) (*api.User, error) {
	var user api.User
	query, _, err := goqu.From("users").
		Where(goqu.C("username").Eq(username)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (q *Queries) GetUsersInTeam(teamID string) ([]*api.User, error) {
	var users []*api.User
	query, _, err := goqu.From("users").
		Where(goqu.C("team_id").Eq(teamID)).
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
		var user api.User
		err := rows.StructScan(&user)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (q *Queries) GenerateUserSecret(username, password string) (string, error) {
	h := md5.New()
	if _, err := io.WriteString(h, username+":"+api.Realm+":"+password); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
