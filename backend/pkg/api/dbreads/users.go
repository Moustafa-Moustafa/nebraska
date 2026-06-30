package dbreads

import (
	"crypto/md5"
	"fmt"
	"io"

	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// realm is used for basic authentication when generating user secrets.
// Mirrors pkg/api.Realm to keep the secret format identical without forcing
// dbreads to import pkg/api.
const realm = "nebraska"

// GetUser returns the user identified by the username provided.
func (q *Queries) GetUser(username string) (*types.User, error) {
	var user types.User
	query, _, err := goqu.From("users").
		Where(goqu.C("username").Eq(username)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	if err := q.db.QueryRowx(query).StructScan(&user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUsersInTeam returns every user that belongs to the given team id.
func (q *Queries) GetUsersInTeam(teamID string) ([]*types.User, error) {
	var users []*types.User
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
		var user types.User
		if err := rows.StructScan(&user); err != nil {
			return nil, err
		}
		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// GenerateUserSecret generates the md5 hash that the user table stores as
// the `secret` column (md5(username:realm:password)). The hash depends on no
// DB state and could live anywhere, but Nebraska treats it as part of the
// user-read surface so it stays here alongside GetUser.
func (q *Queries) GenerateUserSecret(username, password string) (string, error) {
	h := md5.New()
	if _, err := io.WriteString(h, username+":"+realm+":"+password); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
