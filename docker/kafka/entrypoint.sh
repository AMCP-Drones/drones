#!/bin/sh
set -e

ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin123}"

USERS_FILE=$(mktemp)

# Collect all COMPONENT_USER_* from environment
env | grep '^COMPONENT_USER_' | sort | while IFS='=' read -r VAR USERNAME; do
  [ -z "$USERNAME" ] && continue
  SUFFIX="${VAR#COMPONENT_USER_}"
  PASSWORD_VAR="COMPONENT_PASSWORD_${SUFFIX}"
  PASSWORD="$(printenv "$PASSWORD_VAR")"
  [ -z "$PASSWORD" ] && continue
  printf '    user_%s="%s"\n' "$USERNAME" "$PASSWORD" >> "$USERS_FILE"
done

# Build the KafkaServer block with admin as the last (semicolon-terminated) line
{
  echo 'KafkaServer {'
  echo '    org.apache.kafka.common.security.plain.PlainLoginModule required'
  echo "    username=\"${ADMIN_USER}\""
  echo "    password=\"${ADMIN_PASSWORD}\""
  # Component users (no semicolons)
  if [ -s "$USERS_FILE" ]; then
    cat "$USERS_FILE"
  fi
  # Admin user line terminates the module options
  printf '    user_%s="%s";\n' "$ADMIN_USER" "$ADMIN_PASSWORD"
  echo '};'
  echo ''
  echo 'KafkaClient {'
  echo '    org.apache.kafka.common.security.plain.PlainLoginModule required'
  echo "    username=\"${ADMIN_USER}\""
  echo "    password=\"${ADMIN_PASSWORD}\";"
  echo '};'
} > /tmp/jaas.conf

rm -f "$USERS_FILE"

echo "[kafka-entrypoint] JAAS config generated with admin + component users"
if [ "$DEBUG" = "1" ]; then cat /tmp/jaas.conf; fi

exec /etc/kafka/docker/run
