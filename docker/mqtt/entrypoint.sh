#!/bin/sh
set -e

ADMIN_USER="${ADMIN_USER:-admin}"
if [ -z "$ADMIN_PASSWORD" ]; then
  ADMIN_PASSWORD="$(head -c 16 /dev/urandom | base64 | tr -d '=+/' | head -c 24)"
  echo "[mqtt-entrypoint] WARNING: ADMIN_PASSWORD not set, using auto-generated password" >&2
fi

rm -f /mosquitto/data/passwd /mosquitto/data/acl

# Create admin user
mosquitto_passwd -b -c /mosquitto/data/passwd "$ADMIN_USER" "$ADMIN_PASSWORD"
printf "user %s\ntopic readwrite #\n" "$ADMIN_USER" > /mosquitto/data/acl

# Add all COMPONENT_USER_* from env
env | grep '^COMPONENT_USER_' | sort | while IFS='=' read -r VAR USERNAME; do
  [ -z "$USERNAME" ] && continue
  SUFFIX="${VAR#COMPONENT_USER_}"
  PASSWORD_VAR="COMPONENT_PASSWORD_${SUFFIX}"
  PASSWORD="$(printenv "$PASSWORD_VAR")"
  if [ -z "$PASSWORD" ]; then
    echo "[mqtt-entrypoint] WARNING: $PASSWORD_VAR is empty, skipping user $USERNAME" >&2
    continue
  fi
  mosquitto_passwd -b /mosquitto/data/passwd "$USERNAME" "$PASSWORD"
  printf "\nuser %s\ntopic readwrite #\n" "$USERNAME" >> /mosquitto/data/acl
done

chown mosquitto:mosquitto /mosquitto/data/passwd /mosquitto/data/acl
chmod 700 /mosquitto/data/acl

exec mosquitto -c /mosquitto/config/mosquitto.conf