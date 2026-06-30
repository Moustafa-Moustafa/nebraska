#!/usr/bin/env bash
# Configure PostgreSQL logical replication for the admin tables from
# postgres-control -> postgres-edge. Uses docker exec into the containers
# (no host-side psql client required).
set -euo pipefail

NEB_CTRL_URL="http://localhost:9000"
NEB_EDGE_URL="http://localhost:9101"

ADMIN_TABLES=(
  team users application package channel groups flatcar_action
  package_channel_blacklist channel_package_floors event_type instance_status
)
TABLES_CSV=$(IFS=,; echo "${ADMIN_TABLES[*]}")

psql_ctrl() { docker exec -e PGPASSWORD=nebraska nebraska-pg-control psql -U postgres -d nebraska "$@"; }
psql_edge() { docker exec -e PGPASSWORD=nebraska nebraska-pg-edge    psql -U postgres -d nebraska "$@"; }

wait_health() {
  local url="$1" label="$2"
  printf "Waiting for %-50s ... " "${label}"
  for _ in $(seq 1 60); do
    if curl -sf "${url}/health" > /dev/null 2>&1; then echo "ready"; return 0; fi
    sleep 2
  done
  echo "TIMEOUT"; exit 1
}

echo "==> 1) Waiting for both Nebraska instances to be healthy"
wait_health "${NEB_CTRL_URL}" "nebraska-control"
wait_health "${NEB_EDGE_URL}" "nebraska-edge"

echo
echo "==> 2) Schema check"
psql_ctrl -At -c "select 'control has group_local with ' || count(*) || ' columns' from information_schema.columns where table_name='group_local';"
psql_edge -At -c "select 'edge    has group_local with ' || count(*) || ' columns' from information_schema.columns where table_name='group_local';"

echo
echo "==> 3) Creating publication on control"
psql_ctrl -v ON_ERROR_STOP=1 <<EOF
DROP PUBLICATION IF EXISTS admin_pub;
CREATE PUBLICATION admin_pub FOR TABLE ${TABLES_CSV};
SELECT pubname FROM pg_publication;
EOF

echo
echo "==> 4) On edge: drop prior subscription, truncate admin tables, create subscription"
psql_edge -v ON_ERROR_STOP=1 <<EOF
DO \$\$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_subscription WHERE subname = 'admin_sub') THEN
    EXECUTE 'ALTER SUBSCRIPTION admin_sub DISABLE';
    EXECUTE 'ALTER SUBSCRIPTION admin_sub SET (slot_name = NONE)';
    EXECUTE 'DROP SUBSCRIPTION admin_sub';
  END IF;
END\$\$;
TRUNCATE ${TABLES_CSV} RESTART IDENTITY CASCADE;
EOF

psql_ctrl -v ON_ERROR_STOP=1 -c "SELECT pg_drop_replication_slot('admin_sub') WHERE EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='admin_sub');"

psql_edge -v ON_ERROR_STOP=1 -c "CREATE SUBSCRIPTION admin_sub CONNECTION 'host=postgres-control port=5432 dbname=nebraska user=postgres password=nebraska' PUBLICATION admin_pub WITH (copy_data = true);"

echo
echo "==> 5) Waiting up to 30s for initial COPY to finish"
for _ in $(seq 1 30); do
  STATE=$(psql_edge -At -c "SELECT string_agg(srsubstate, ',') FROM pg_subscription_rel JOIN pg_subscription ON oid = srsubid WHERE subname='admin_sub';")
  if [[ -n "${STATE}" && "${STATE}" =~ ^[rs,]+$ ]]; then echo "all relations in steady state: ${STATE}"; break; fi
  echo "  ... current relation states: ${STATE}"
  sleep 1
done

echo
echo "==> 6) Per-table row counts:"
for t in "${ADMIN_TABLES[@]}"; do
  C=$(psql_ctrl -At -c "select count(*) from ${t}" 2>/dev/null || echo "?")
  E=$(psql_edge -At -c "select count(*) from ${t}" 2>/dev/null || echo "?")
  printf "  %-30s control=%-6s edge=%-6s\n" "${t}" "${C}" "${E}"
done

echo
echo "==> 7) Verify AFTER INSERT trigger fired on edge during COPY"
psql_ctrl -At -c "select 'control group_local rows: ' || count(*) from group_local;"
psql_edge -At -c "select 'edge    group_local rows: ' || count(*) from group_local;"

echo
echo "URLs: control=${NEB_CTRL_URL}   edge=${NEB_EDGE_URL}"
