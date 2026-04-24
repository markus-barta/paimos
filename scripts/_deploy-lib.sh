#!/usr/bin/env bash
# Shared deploy flow. Sourced by scripts/deploy.sh.
#
# Expects the per-instance conf to have exported:
#   SSH_TARGET            host (ssh-config alias or user@ip)
#   SSH_AUTH              "key" | "password"
#   SSH_USER              (password auth only) remote username
#   SSH_PASS_FILE         (password auth only) path to an env file that
#                         `source`s into a variable expect reads
#   COMPOSE_DIR           remote compose working directory
#   SERVICE               docker-compose service name
#   CONTAINER             docker container name
#   STORAGE               "bind" | "volume"
#   DATA_PATH             (bind)   absolute host path of the bind mount
#   VOLUME_NAME           (volume) named volume backing /app/data
#   DB_FILENAME           sqlite filename inside the data dir
#   BACKUP_ROOT           remote dir under which timestamped backups go
#   INSTANCE_URL          public URL for the external smoke test

# Run a shell command on the remote host. Secrets are never printed.
# The command is base64-encoded locally and decoded into `bash` on the
# remote so it always runs under bash, even when the remote user's login
# shell is something else (e.g. mba's login shell on csb1 is fish).
deploy::ssh() {
  local cmd="$1"
  local encoded
  encoded=$(printf '%s' "$cmd" | base64 | tr -d '\n')
  local remote="printf %s '$encoded' | base64 -d | bash"
  case "${SSH_AUTH:-}" in
    key)
      ssh -o BatchMode=yes -o ConnectTimeout=15 "$SSH_TARGET" "$remote"
      ;;
    password)
      if [[ -z "${SSH_PASS_FILE:-}" || -z "${SSH_USER:-}" || -z "${SSH_PASS_VAR:-}" ]]; then
        echo "error: password auth requires SSH_USER + SSH_PASS_FILE + SSH_PASS_VAR" >&2
        return 1
      fi
      (
        # shellcheck disable=SC1090
        source "${SSH_PASS_FILE/#\~/$HOME}"
        SSH_PASSWORD="${!SSH_PASS_VAR:-}"
        if [[ -z "$SSH_PASSWORD" ]]; then
          echo "error: $SSH_PASS_VAR not set after sourcing $SSH_PASS_FILE" >&2
          exit 1
        fi
        export SSH_PASSWORD
        # expect uses a PTY, so ssh output gets \r\n line endings. After
        # the password is sent, the first character in the log stream is
        # the post-password newline echoed by the server. Normalize both
        # so downstream grep/parsing works the same as key-auth mode.
        "$ROOT/scripts/_ssh-pass.exp" "$SSH_USER" "$SSH_TARGET" "$remote" \
          | tr -d '\r' \
          | sed '/./,$!d'
      )
      ;;
    *)
      echo "error: unknown SSH_AUTH='${SSH_AUTH:-}'" >&2
      return 1
      ;;
  esac
}

deploy::run() {
  local instance="$1" tag="$2" image="$3"
  local stamp backup pre_image pre_digest

  stamp=$(date -u +%Y-%m-%dT%H-%M-%SZ)
  backup="$BACKUP_ROOT/$stamp"

  echo "--- [1/8] pre-flight"
  pre_image=$(deploy::ssh "docker inspect $CONTAINER --format '{{.Config.Image}}' 2>/dev/null || echo no-container")
  pre_digest=$(deploy::ssh "docker inspect $CONTAINER --format '{{.Image}}' 2>/dev/null || echo unknown")
  echo "    current: $pre_image"
  echo "    target:  $image"
  if [[ "$pre_image" == "$image" ]]; then
    echo "note: already on target image — nothing to do"
    return 0
  fi

  echo "--- [2/8] stop $CONTAINER"
  deploy::ssh "cd $COMPOSE_DIR && docker compose stop $SERVICE"

  echo "--- [3/8] backup ($STORAGE)"
  deploy::ssh "mkdir -p $backup && cp $COMPOSE_DIR/docker-compose.yml $backup/docker-compose.yml.pre"
  case "$STORAGE" in
    bind)
      deploy::ssh "tar -czf $backup/data.tar.gz -C $(dirname "$DATA_PATH") $(basename "$DATA_PATH")"
      ;;
    volume)
      # Throwaway alpine tar against a read-only volume mount — avoids sudo
      # and works whether the host has tar/sqlite or not.
      deploy::ssh "docker run --rm -v $VOLUME_NAME:/src:ro -v $backup:/dst alpine sh -c 'cd /src && tar -czf /dst/data.tar.gz .'"
      ;;
    *)
      echo "error: unknown STORAGE='$STORAGE'" >&2
      return 1
      ;;
  esac

  echo "--- [4/8] validate backup"
  local probe
  probe=$(deploy::ssh "
    set -e
    gzip -t $backup/data.tar.gz
    entries=\$(tar -tzf $backup/data.tar.gz | wc -l | tr -d ' ')
    db_count=\$(tar -tzf $backup/data.tar.gz | grep -c '$DB_FILENAME\$' || true)
    wal_count=\$(tar -tzf $backup/data.tar.gz | grep -c '$DB_FILENAME-wal\$' || true)
    size=\$(stat -c %s $backup/data.tar.gz 2>/dev/null || stat -f %z $backup/data.tar.gz)
    printf 'entries=%s\ndb_count=%s\nwal_count=%s\nsize=%s\n' \
      \"\$entries\" \"\$db_count\" \"\$wal_count\" \"\$size\"
  ")
  # Parse key=value lines into locals. Robust against leading blanks,
  # trailing whitespace, or incidental log output from the ssh transport.
  local entries=0 db_count=0 wal_count=0 size=0
  while IFS='=' read -r k v; do
    case "$k" in
      entries)   entries="$v" ;;
      db_count)  db_count="$v" ;;
      wal_count) wal_count="$v" ;;
      size)      size="$v" ;;
    esac
  done < <(printf '%s\n' "$probe" | grep -E '^[a-z_]+=[0-9]+$')
  printf '    entries=%s db_count=%s wal_count=%s size=%s bytes\n' \
    "$entries" "$db_count" "$wal_count" "$size"
  if [[ "$db_count" != "1" ]]; then
    echo "error: backup does not contain $DB_FILENAME — aborting before deploy" >&2
    echo "raw probe output:" >&2
    printf '%s\n' "$probe" | sed 's/^/    /' >&2
    return 1
  fi

  echo "--- [5/8] manifest"
  # printf with newlines is safer over ssh than heredoc.
  deploy::ssh "printf '%s\n' \
    'instance: $instance' \
    'timestamp: $stamp' \
    'pre_deploy_image: $pre_image' \
    'pre_deploy_image_id: $pre_digest' \
    'target_image: $image' \
    'compose_dir: $COMPOSE_DIR' \
    'service: $SERVICE' \
    'container: $CONTAINER' \
    'storage: $STORAGE' \
    'db_filename: $DB_FILENAME' \
    > $backup/manifest.yaml"

  echo "--- [6/8] pin compose → $image, pull, up -d"
  # Keep a .bak alongside the live compose for extra safety.
  deploy::ssh "
    set -e
    cd $COMPOSE_DIR
    cp docker-compose.yml docker-compose.yml.bak.$stamp
    sed -i 's|image: ghcr.io/markus-barta/paimos:[^ ]*|image: $image|' docker-compose.yml
    diff docker-compose.yml.bak.$stamp docker-compose.yml || true
    docker compose pull $SERVICE
    docker compose up -d $SERVICE
  "

  echo "--- [7/8] tail logs (5s warm-up)"
  sleep 5
  deploy::ssh "cd $COMPOSE_DIR && docker compose logs --tail=40 $SERVICE" || true

  echo "--- [8/8] external smoke test: $INSTANCE_URL/api/health"
  local ok=0
  for i in $(seq 1 12); do
    if curl -sfS -A "paimos-deploy/1.0" --max-time 5 "$INSTANCE_URL/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
      ok=1
      break
    fi
    sleep 2
  done
  if [[ $ok -ne 1 ]]; then
    echo "✗ smoke test failed after 24s" >&2
    deploy::print_rollback "$backup" "$pre_image"
    return 1
  fi
  echo "    ✔ /api/health ok"

  echo
  echo "✔ $instance is live on $image"
  echo "  backup: $backup"
  echo
  deploy::print_rollback "$backup" "$pre_image" "INFO"
}

deploy::print_rollback() {
  local backup="$1" pre_image="$2" mode="${3:-FAIL}"
  local header
  if [[ "$mode" == "INFO" ]]; then
    header="Rollback command (for the record — do NOT run unless needed):"
  else
    header="Rollback (run on the host to revert):"
  fi
  echo "$header"
  echo "  cd $COMPOSE_DIR"
  echo "  docker compose stop $SERVICE"
  case "$STORAGE" in
    bind)
      # Use alpine+tar (running as root via docker) because bind-mounted
      # data files are owned by root — plain `tar -x` as the ssh user
      # hits permission-denied on overwrite. --strip-components=1 drops
      # the leading "data/" dir from the archive so files land inside
      # the mount rather than creating a nested data/data/.
      echo "  docker run --rm -v $DATA_PATH:/dst -v $backup:/src:ro alpine sh -c 'cd /dst && rm -rf ./* && tar -xzf /src/data.tar.gz --strip-components=1'"
      ;;
    volume)
      echo "  docker run --rm -v $VOLUME_NAME:/dst -v $backup:/src:ro alpine sh -c 'cd /dst && rm -rf ./* && tar -xzf /src/data.tar.gz'"
      ;;
  esac
  echo "  sed -i 's|image: ghcr.io/markus-barta/paimos:[^ ]*|image: $pre_image|' docker-compose.yml"
  echo "  docker compose up -d $SERVICE"
}
