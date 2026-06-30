package api

import (
	"github.com/doug-martin/goqu/v9"

	"github.com/flatcar/nebraska/backend/pkg/api/internal/types"
)

// FlatcarAction is owned by pkg/api/internal/types; re-exported here.
type FlatcarAction = types.FlatcarAction

// AddFlatcarAction registers the provided Omaha Flatcar action.
func (api *API) AddFlatcarAction(action *FlatcarAction) (*FlatcarAction, error) {
	query, _, err := goqu.Insert("flatcar_action").
		Cols("event", "chromeos_version", "sha256", "needs_admin", "is_delta", "disable_payload_backoff", "metadata_signature_rsa", "metadata_size", "deadline", "package_id").
		Vals(goqu.Vals{
			action.Event,
			action.ChromeOSVersion,
			action.Sha256,
			action.NeedsAdmin,
			action.IsDelta,
			action.DisablePayloadBackoff,
			action.MetadataSignatureRsa,
			action.MetadataSize,
			action.Deadline,
			action.PackageID,
		}).
		Returning(goqu.T("flatcar_action").All()).
		ToSQL()
	if err != nil {
		return nil, err
	}
	err = api.db.QueryRowx(query).StructScan(action)
	if err != nil {
		return nil, err
	}
	return action, err
}

// GetFlatcarAction forwards to the embedded reads. The actual SQL lives in
// pkg/api/dbreads/actions.go.
func (api *API) GetFlatcarAction(packageID string) (*FlatcarAction, error) {
	return api.queries.GetFlatcarAction(packageID)
}
