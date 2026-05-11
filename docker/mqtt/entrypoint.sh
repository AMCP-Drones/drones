#!/bin/sh
set -eu

ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin_secret_123}"
PASSWD_FILE="/mosquitto/config/passwd"

mkdir -p /mosquitto/config /mosquitto/data /mosquitto/log

if [ ! -f "${PASSWD_FILE}" ]; then
  touch "${PASSWD_FILE}"
fi

mosquitto_passwd -b "${PASSWD_FILE}" "${ADMIN_USER}" "${ADMIN_PASSWORD}"

if [ -n "${COMPONENT_USER_A:-}" ] && [ -n "${COMPONENT_PASSWORD_A:-}" ]; then
  mosquitto_passwd -b "${PASSWD_FILE}" "${COMPONENT_USER_A}" "${COMPONENT_PASSWORD_A}"
fi

if [ -n "${COMPONENT_USER_B:-}" ] && [ -n "${COMPONENT_PASSWORD_B:-}" ]; then
  mosquitto_passwd -b "${PASSWD_FILE}" "${COMPONENT_USER_B}" "${COMPONENT_PASSWORD_B}"
fi

exec /usr/sbin/mosquitto -c /mosquitto/config/mosquitto.conf
