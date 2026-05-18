#!/bin/sh
set -eu

PASSWD_FILE="/mosquitto/config/passwd"
ACL_FILE="/mosquitto/config/acl"
CONFIG_DIR="/mosquitto/config"

mkdir -p "${CONFIG_DIR}" /mosquitto/data /mosquitto/log

if [ -x /mosquitto/config/generate_auth.sh ]; then
  PASSWD_FILE="${PASSWD_FILE}" ACL_FILE="${ACL_FILE}" /mosquitto/config/generate_auth.sh
elif [ -f /docker-mqtt/generate_auth.sh ]; then
  PASSWD_FILE="${PASSWD_FILE}" ACL_FILE="${ACL_FILE}" /docker-mqtt/generate_auth.sh
else
  ADMIN_USER="${ADMIN_USER:-admin}"
  ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin_secret_123}"
  touch "${PASSWD_FILE}"
  mosquitto_passwd -b "${PASSWD_FILE}" "${ADMIN_USER}" "${ADMIN_PASSWORD}"
fi

exec /usr/sbin/mosquitto -c /mosquitto/config/mosquitto.conf
