# Sub2API upgrade runbook

This host uses a source-built Sub2API binary with local custom commits. Do not
replace it with the upstream one-click installer unless you intentionally want to
discard local changes.

## Hosts

- Primary: current host, service listens on `0.0.0.0:8181`.
- Standby: `root@10.26.12.89`.
- Primary database and Redis are on the primary private IP `10.29.12.88`.
- Standby `config.yaml` must keep database and Redis pointing to
  `10.29.12.88`, not `127.0.0.1`.

## Pre-upgrade standby sync

Run this before changing the primary, so the standby can take traffic with the
known-good version if the upgrade fails.

1. Check current versions and service state:

   ```bash
   /opt/sub2api/sub2api --version
   systemctl is-active sub2api
   ssh root@10.26.12.89 '/opt/sub2api/sub2api --version; systemctl is-active sub2api'
   ```

2. Verify standby config points to primary database and Redis:

   ```bash
   ssh root@10.26.12.89 "grep -E '^[[:space:]]*(host|port|dbname|db):' /opt/sub2api/config.yaml"
   ```

   Expected database and Redis hosts are `10.29.12.88`.

3. Back up standby binary and source tree, then sync primary source and binary:

   ```bash
   ssh root@10.26.12.89 'ts=$(date +%Y%m%d%H%M%S); \
     cp -a /opt/sub2api/sub2api /opt/sub2api/sub2api.bak.pre-primary-sync-$ts; \
     if [ -e /root/sub2api-src ]; then mv /root/sub2api-src /root/sub2api-src.bak.pre-primary-sync-$ts; fi; \
     mkdir -p /root/sub2api-src'

   rsync -az --delete --exclude 'frontend/node_modules/' \
     /root/sub2api-src/ root@10.26.12.89:/root/sub2api-src/

   rsync -az /opt/sub2api/sub2api root@10.26.12.89:/opt/sub2api/sub2api
   ssh root@10.26.12.89 'chown root:root /opt/sub2api/sub2api; chmod 755 /opt/sub2api/sub2api; systemctl restart sub2api'
   ```

4. Verify standby:

   ```bash
   sha256sum /opt/sub2api/sub2api
   ssh root@10.26.12.89 'sha256sum /opt/sub2api/sub2api; git -C /root/sub2api-src log --oneline -3; curl -sS -I --max-time 8 http://127.0.0.1:8181/login | head'
   ```

## Primary upgrade

1. Fetch upstream and inspect the new release:

   ```bash
   cd /root/sub2api-src
   git fetch origin --tags
   git diff --stat v0.1.136..v0.1.137
   git log --oneline v0.1.136..v0.1.137 --max-count=30
   git merge-tree --write-tree HEAD v0.1.137
   ```

2. Ensure the source tree is clean and create rollback markers:

   ```bash
   git status --short
   git tag local/pre-v0.1.137-$(date +%Y%m%d%H%M%S)
   cp -a /opt/sub2api/sub2api /opt/sub2api/sub2api.bak.pre-v0.1.137-$(date +%Y%m%d%H%M%S)
   ```

3. Merge the upstream tag:

   ```bash
   git merge --no-edit v0.1.137
   ```

   Resolve conflicts in source only. Do not deploy until builds and tests pass.

4. Build the frontend:

   ```bash
   pnpm --dir frontend install --frozen-lockfile
   pnpm --dir frontend run build
   ```

5. Build the backend embedded binary. If upstream forgot to update
   `backend/cmd/server/VERSION`, pass the intended release explicitly:

   ```bash
   make -C backend build VERSION=0.1.137
   /root/sub2api-src/backend/bin/server --version
   ```

6. If the build exposes merge-adaptation errors, fix them locally and commit the
   fix. For v0.1.137, `FilterThinkingBlocksForRetry` started requiring a
   `mappedModel` argument, so local historical-thinking sanitization needed an
   adapter commit:

   ```bash
   git log --oneline -1
   # 9e0861a3 fix: adapt thinking sanitize to v0.1.137
   ```

7. Run focused tests for touched high-risk areas:

   ```bash
   cd /root/sub2api-src/backend
   go test ./internal/service -run 'TestFilterThinkingBlocksForRetry|TestSanitizeHistoricalThinkingBlocks|TestThinking|TestApplyThinking|TestNormalizeChineseLLMThinking'
   ```

   Add broader tests when the release changes shared request, billing, auth, or
   migration behavior.

8. Back up data before deploying releases with migrations:

   ```bash
   PGPASSWORD='<database password>' pg_dump -h 127.0.0.1 -U admin_account -d sub2api -Fc \
     -f /root/sub2api-db-pre-v0.1.137-$(date +%Y%m%d%H%M).dump
   redis-cli -a '<redis password>' -n 3 BGSAVE
   ```

9. Deploy and restart:

   ```bash
   install -o root -g root -m 755 /root/sub2api-src/backend/bin/server /opt/sub2api/sub2api
   systemctl restart sub2api
   sleep 4
   systemctl is-active sub2api
   systemctl show sub2api -p MainPID -p ActiveEnterTimestamp --no-pager
   /opt/sub2api/sub2api --version
   ```

10. Verify service:

    ```bash
    curl -sS -I --max-time 8 http://127.0.0.1:8181/login | head
    journalctl -u sub2api --since '5 minutes ago' --no-pager | grep -Ei 'error|fatal|panic|failed|migration' | tail -80
    ss -ltnp | grep ':8181'
    ```

    A `Failed with result 'exit-code'` line at the exact restart moment can be
    from the old process forced shutdown. Re-check from the new process start
    timestamp before treating it as a new-version failure.

## Rollback

If the primary upgrade fails after deployment:

1. Immediate binary rollback on primary:

   ```bash
   cp -a /opt/sub2api/sub2api.bak.pre-v0.1.137-YYYYMMDDHHMMSS /opt/sub2api/sub2api
   chmod 755 /opt/sub2api/sub2api
   systemctl restart sub2api
   /opt/sub2api/sub2api --version
   curl -sS -I --max-time 8 http://127.0.0.1:8181/login | head
   ```

2. If the primary is unhealthy, switch traffic to standby `10.26.12.89`, which
   should still be running the pre-upgrade version.

3. If database migrations ran and the older binary is incompatible, restore from
   the pre-upgrade PostgreSQL dump after stopping writers. Treat this as a
   planned outage operation.

## Notes from v0.1.137

- Official tag `v0.1.137` still contained `backend/cmd/server/VERSION` as
  `0.1.136`; build with `VERSION=0.1.137` to make `--version` accurate.
- The primary was backed up before deployment:
  `/opt/sub2api/sub2api.bak.pre-v0.1.137-202606171700`.
- PostgreSQL dump before deployment:
  `/root/sub2api-db-pre-v0.1.137-202606171704.dump`.
- Standby binary backup before sync:
  `/opt/sub2api/sub2api.bak.pre-primary-sync-20260617165442`.
