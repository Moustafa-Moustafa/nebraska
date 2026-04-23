package dbreads

import (
	"database/sql"

	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api"
	"github.com/flatcar/nebraska/backend/pkg/api/internal/shared"
)

func (q *Queries) GetChannel(channelID string) (*api.Channel, error) {
	var channel api.Channel
	query, _, err := goqu.From("channel").
		Where(goqu.C("id").Eq(channelID)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = q.db.QueryRowx(query).StructScan(&channel)
	if err != nil {
		return nil, err
	}
	packageEntity, err := q.getPackage(channel.PackageID)
	switch err {
	case nil:
		channel.Package = packageEntity
	case sql.ErrNoRows:
		channel.Package = nil
	default:
		return nil, err
	}
	return &channel, nil
}

func (q *Queries) GetChannelsCount(appID string) (int, error) {
	query := goqu.From("channel").Where(goqu.C("application_id").Eq(appID)).Select(goqu.L("count(*)"))
	return q.GetCountQuery(query)
}

func (q *Queries) GetChannels(appID string, page, perPage uint64) ([]*api.Channel, error) {
	page, perPage = shared.ValidatePaginationParams(page, perPage)
	limit, offset := shared.SQLPaginate(page, perPage)
	query, _, err := q.channelsQuery().
		Where(goqu.C("application_id").Eq(appID)).
		Limit(limit).
		Offset(offset).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getChannelsFromQuery(query)
}

func (q *Queries) GetChannelsForApp(appID string) ([]*api.Channel, error) {
	query, _, err := q.channelsQuery().
		Where(goqu.C("application_id").Eq(appID)).
		ToSQL()
	if err != nil {
		return nil, err
	}
	return q.getChannelsFromQuery(query)
}

func (q *Queries) getChannelsFromQuery(query string) ([]*api.Channel, error) {
	var channels []*api.Channel
	rows, err := q.db.Queryx(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		channel := api.Channel{}
		if err := rows.StructScan(&channel); err != nil {
			return nil, err
		}
		packageEntity, err := q.getPackage(channel.PackageID)
		switch err {
		case nil:
			channel.Package = packageEntity
		case sql.ErrNoRows:
			channel.Package = nil
		default:
			return nil, err
		}
		channels = append(channels, &channel)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return channels, nil
}

func (q *Queries) channelsQuery() *goqu.SelectDataset {
	return goqu.From("channel").Order(goqu.I("name").Asc())
}

func (q *Queries) ValidateChannel(channelID, appID string) error {
	channel, err := q.GetChannel(channelID)
	if err != nil {
		return err
	}
	if channel.ApplicationID != appID {
		return api.ErrInvalidChannel
	}
	return nil
}
