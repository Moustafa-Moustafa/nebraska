package admin

import (
	"crypto/md5"
	"fmt"
	"io"

	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
)

// AddUser registers a new user.
func (s *Service) AddUser(user *api.User) (*api.User, error) {
	query, _, err := goqu.Insert("users").
		Cols("username", "team_id", "secret").
		Vals(goqu.Vals{user.Username, user.TeamID, user.Secret}).
		Returning(goqu.T("users").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowx(query).StructScan(user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateUserPassword updates the password of the provided user.
func (s *Service) UpdateUserPassword(username, newPassword string) error {
	secret, err := generateUserSecret(username, newPassword)
	if err != nil {
		return err
	}
	query, _, err := goqu.Update("users").
		Set(goqu.Record{"secret": secret}).
		Where(goqu.C("username").Eq(username)).
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
		return api.ErrUpdatingPassword
	}
	return nil
}

func generateUserSecret(username, password string) (string, error) {
	h := md5.New()
	if _, err := io.WriteString(h, username+":"+api.Realm+":"+password); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
