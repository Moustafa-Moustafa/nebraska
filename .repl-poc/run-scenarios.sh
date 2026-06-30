#!/usr/bin/env bash
# Live PoC test scenarios for the node-local group_local sidecar.
#
# Tests both single-instance and distributed semantics by:
#   - Reading effective policy values via the REST API on each node.
#   - Tripping the brake by directly writing the override on group_local
#     (same SQL the Go disableUpdates() function executes - it's the
#     internal helper that enforceRolloutPolicy/triggerEventConsequences
#     call, exercised end-to-end by the unit tests against a real PG).
#   - Calling the REST API to admin-write the group (UpdateGroup),
#     which writes the admin default AND clears the override in one
#     transaction (verifies the re-enable semantics).

set -euo pipefail

APP_ID="e96281a6-d1af-4bde-9a0a-97b76e56dc57"
GROUP_ID="9a2deb70-37be-4026-853f-bfdd6b347bbe"   # "Stable" group
GROUP_NAME="Stable"

CTRL="http://localhost:9000"
EDGE="http://localhost:9101"

psql_ctrl() { docker exec -e PGPASSWORD=nebraska nebraska-pg-control psql -U postgres -d nebraska -At "$@"; }
psql_edge() { docker exec -e PGPASSWORD=nebraska nebraska-pg-edge    psql -U postgres -d nebraska -At "$@"; }

get_effective() {
  # Returns the effective policy_updates_enabled value as seen by the API.
  local base="$1"
  curl -s "${base}/api/apps/${APP_ID}/groups/${GROUP_ID}" | python3 -c 'import sys,json; g=json.load(sys.stdin); print("enabled=" + str(g["policy_updates_enabled"]).lower() + " safe_mode=" + str(g["policy_safe_mode"]).lower())'
}

raw_state() {
  local label="$1"
  local where_node="$2"
  local default override
  if [[ "${where_node}" = "ctrl" ]]; then
    default=$(psql_ctrl -c "select policy_updates_enabled from groups where id='${GROUP_ID}'")
    override=$(psql_ctrl -c "select coalesce(policy_updates_enabled_override::text,'NULL') from group_local where group_id='${GROUP_ID}'")
  else
    default=$(psql_edge -c "select policy_updates_enabled from groups where id='${GROUP_ID}'")
    override=$(psql_edge -c "select coalesce(policy_updates_enabled_override::text,'NULL') from group_local where group_id='${GROUP_ID}'")
  fi
  printf "    %-10s groups.policy_updates_enabled=%s   group_local.override=%s\n" "${label}" "${default}" "${override}"
}

snapshot() {
  echo "  ${1}:"
  echo "    effective (control API): $(get_effective ${CTRL})"
  echo "    effective (edge    API): $(get_effective ${EDGE})"
  raw_state "control" ctrl
  raw_state "edge   " edge
}

brake_via_sql() {
  # Simulates disableUpdates() on the named node by writing the same SQL.
  local node="$1"
  if [[ "${node}" = "ctrl" ]]; then
    psql_ctrl -c "update group_local set policy_updates_enabled_override = false where group_id = '${GROUP_ID}'" > /dev/null
  else
    psql_edge -c "update group_local set policy_updates_enabled_override = false where group_id = '${GROUP_ID}'" > /dev/null
  fi
}

clear_override_via_sql() {
  local node="$1"
  if [[ "${node}" = "ctrl" ]]; then
    psql_ctrl -c "update group_local set policy_updates_enabled_override = NULL where group_id = '${GROUP_ID}'" > /dev/null
  else
    psql_edge -c "update group_local set policy_updates_enabled_override = NULL where group_id = '${GROUP_ID}'" > /dev/null
  fi
}

# Helper to build a GroupConfig JSON body with policy_updates_enabled set
# to a given boolean. Keeps every other field at the existing value so we
# don't accidentally change the channel/track/etc.
admin_set_enabled() {
  local base="$1"
  local enabled="$2"  # 'true' or 'false' (lowercase, as JSON literals)
  local py_bool
  if [[ "${enabled}" = "true" ]]; then py_bool="True"; else py_bool="False"; fi
  local body
  body=$(curl -s "${base}/api/apps/${APP_ID}/groups/${GROUP_ID}" | python3 -c "
import sys,json
g = json.load(sys.stdin)
body = {
    'name': g['name'],
    'description': g['description'],
    'channel_id': g['channel_id'],
    'policy_updates_enabled': ${py_bool},
    'policy_safe_mode': g['policy_safe_mode'],
    'policy_office_hours': g['policy_office_hours'],
    'policy_timezone': g['policy_timezone'],
    'policy_period_interval': g['policy_period_interval'],
    'policy_max_updates_per_period': g['policy_max_updates_per_period'],
    'policy_update_timeout': g['policy_update_timeout'],
    'track': g['track'],
}
print(json.dumps(body))")
  curl -s -X PUT -H 'Content-Type: application/json' -d "${body}" "${base}/api/apps/${APP_ID}/groups/${GROUP_ID}" > /dev/null
}

wait_for_replication() {
  # Poll until the edge node sees the latest groups.policy_updates_enabled
  # value matching the control node, or timeout. Logical replication is
  # usually sub-second on localhost.
  local expect
  expect=$(psql_ctrl -c "select policy_updates_enabled from groups where id='${GROUP_ID}'")
  for _ in $(seq 1 20); do
    local got
    got=$(psql_edge -c "select policy_updates_enabled from groups where id='${GROUP_ID}'")
    if [[ "${got}" = "${expect}" ]]; then return 0; fi
    sleep 0.25
  done
  echo "    (warning: edge replication slow; expected ${expect})"
}

heading() { printf "\n================================================================\n%s\n================================================================\n" "$1"; }

# Ensure clean starting state on both nodes.
clear_override_via_sql ctrl
clear_override_via_sql edge
admin_set_enabled "${CTRL}" true
wait_for_replication

heading "Starting state: clean defaults on both nodes"
snapshot "initial"

# ---------------------------------------------------------------------------
heading "Scenario 1 (single-instance, control only): brake trips -> effective=false"
brake_via_sql ctrl
snapshot "after brake on control"

heading "Scenario 2 (single-instance): admin re-enable -> override cleared, effective=true"
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "after admin re-enable on control"

heading "Scenario 3 (single-instance): admin explicit disable (default=false) -> effective=false"
admin_set_enabled "${CTRL}" false
wait_for_replication
snapshot "after admin disable on control"

heading "Scenario 4 (single-instance): admin re-enable (default=true) -> effective=true"
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "after admin re-enable on control"

# ---------------------------------------------------------------------------
heading "Distributed scenarios: control + edge"

heading "Scenario 5: admin write on control replicates default to edge"
admin_set_enabled "${CTRL}" false
wait_for_replication
snapshot "after admin disable on control (should be false on both)"
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "after admin re-enable on control (should be true on both)"

heading "Scenario 6: brake on edge only -> edge false, control true (no leak via replication)"
brake_via_sql edge
sleep 0.5
snapshot "after brake on edge"

heading "Scenario 7: brake on control too -> control false, edge still false (independent overrides)"
brake_via_sql ctrl
sleep 0.5
snapshot "after brake on control too"

heading "Scenario 8: admin re-enable on control -> control override cleared; edge override stays (preserves edge autonomy)"
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "after admin re-enable on control"

heading "Scenario 9: admin write through edge node -> edge override cleared"
admin_set_enabled "${EDGE}" true
sleep 0.5
snapshot "after admin re-enable on edge"

heading "Scenario 10: edge brake survives an unrelated control admin write (description change)"
brake_via_sql edge
sleep 0.3
# Change description only, leave policy_updates_enabled as the current effective value.
body=$(curl -s "${CTRL}/api/apps/${APP_ID}/groups/${GROUP_ID}" | python3 -c "
import sys,json
g = json.load(sys.stdin)
g['description'] = g['description'].rstrip(' (edited)') + ' (edited)'
body = {k:g[k] for k in ['name','description','channel_id','policy_updates_enabled','policy_safe_mode','policy_office_hours','policy_timezone','policy_period_interval','policy_max_updates_per_period','policy_update_timeout','track']}
print(json.dumps(body))")
curl -s -X PUT -H 'Content-Type: application/json' -d "${body}" "${CTRL}/api/apps/${APP_ID}/groups/${GROUP_ID}" > /dev/null
wait_for_replication
snapshot "after control description edit (UpdateGroup also clears control's override; edge override stays)"

heading "Scenario 11: admin disable does NOT clear an existing brake; only enable clears it"
# Start clean: defaults true, no overrides on either node.
clear_override_via_sql ctrl
clear_override_via_sql edge
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "11a: clean start"

# Trip the local brake on control.
brake_via_sql ctrl
sleep 0.3
snapshot "11b: brake tripped on control (override=false, default=true, effective=false)"

# Admin writes policy_updates_enabled=false (the new behavior: must NOT clear override).
admin_set_enabled "${CTRL}" false
wait_for_replication
snapshot "11c: admin set policy_updates_enabled=false (override MUST still be false, not NULL)"

# Admin writes policy_updates_enabled=true (this should clear the override).
admin_set_enabled "${CTRL}" true
wait_for_replication
snapshot "11d: admin set policy_updates_enabled=true (override MUST be NULL)"

# Trip again, then send an UpdateGroup with policy_updates_enabled=false plus an unrelated edit (description change).
brake_via_sql ctrl
sleep 0.3
body=$(curl -s "${CTRL}/api/apps/${APP_ID}/groups/${GROUP_ID}" | python3 -c "
import sys,json
g = json.load(sys.stdin)
body = {k:g[k] for k in ['name','description','channel_id','policy_safe_mode','policy_office_hours','policy_timezone','policy_period_interval','policy_max_updates_per_period','policy_update_timeout','track']}
body['policy_updates_enabled'] = False
body['description'] = (g['description'].rstrip(' (s11)')) + ' (s11)'
print(json.dumps(body))")
curl -s -X PUT -H 'Content-Type: application/json' -d "${body}" "${CTRL}/api/apps/${APP_ID}/groups/${GROUP_ID}" > /dev/null
wait_for_replication
snapshot "11e: unrelated edit + policy_updates_enabled=false (override MUST still be false)"

heading "Cleanup: restore initial clean state"
admin_set_enabled "${EDGE}" true
admin_set_enabled "${CTRL}" true
wait_for_replication
clear_override_via_sql ctrl
clear_override_via_sql edge
snapshot "final"

echo
echo "URLs still up for browser testing:"
echo "  control admin UI: ${CTRL}"
echo "  edge    admin UI: ${EDGE}"
