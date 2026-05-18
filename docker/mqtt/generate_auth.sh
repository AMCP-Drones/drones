#!/bin/sh
# Generates Mosquitto passwd + acl for one BAS instance (legacy topic scheme).
set -eu

PASSWD_FILE="${PASSWD_FILE:-/mosquitto/config/passwd}"
ACL_FILE="${ACL_FILE:-/mosquitto/config/acl}"
CRED_FILE="${CRED_FILE:-/mosquitto/config/credentials.env}"

TOPIC_VERSION="${TOPIC_VERSION:-v1}"
SYSTEM_NAME="${SYSTEM_NAME:-deliverydron}"
INSTANCE_ID="${INSTANCE_ID:-Delivery001}"
TOPIC_PREFIX="${TOPIC_PREFIX:-${TOPIC_VERSION}.${SYSTEM_NAME}.${INSTANCE_ID}}"

COMPONENTS="${MQTT_COMPONENTS:-security_monitor journal navigation mission_handler autopilot limiter emergency motors cargo telemetry}"
EXTERNAL_USERS="${MQTT_EXTERNAL_USERS:-}"

slug() {
  echo "$1" | tr -c 'A-Za-z0-9' '_'
}

broker_user() {
  comp="$1"
  env_key=$(echo "$comp" | tr '[:lower:]' '[:upper:]' | tr '-' '_')
  eval "u=\${${env_key}_BROKER_USER:-}"
  if [ -n "$u" ]; then
    echo "$u"
    return
  fi
  echo "dd_$(slug "$INSTANCE_ID")_$(slug "$comp")"
}

broker_pass() {
  comp="$1"
  env_key=$(echo "$comp" | tr '[:lower:]' '[:upper:]' | tr '-' '_')
  eval "p=\${${env_key}_BROKER_PASSWORD:-}"
  if [ -n "$p" ]; then
    echo "$p"
    return
  fi
  eval "p=\${BROKER_PASSWORD:-}"
  echo "$p"
}

topic_for() {
  echo "${TOPIC_PREFIX}.$1"
}

mkdir -p "$(dirname "$PASSWD_FILE")" "$(dirname "$ACL_FILE")"
: >"$PASSWD_FILE"
: >"$ACL_FILE"

if [ -f "$CRED_FILE" ]; then
  # shellcheck disable=SC1090
  . "$CRED_FILE"
fi

{
  echo "# Auto-generated Mosquitto ACL"
  echo "# Instance: ${INSTANCE_ID}"
  echo ""
} >"$ACL_FILE"

sm_topic=$(topic_for security_monitor)
sm_user=$(broker_user security_monitor)

{
  echo "user ${sm_user}"
  echo "topic read ${sm_topic}"
  echo "topic write ${sm_topic}"
} >>"$ACL_FILE"

for comp in $COMPONENTS; do
  [ "$comp" = "security_monitor" ] && continue
  ct=$(topic_for "$comp")
  rt=$(topic_for "replies.$comp")
  echo "topic read ${ct}" >>"$ACL_FILE"
  echo "topic write ${ct}" >>"$ACL_FILE"
  echo "topic read ${rt}" >>"$ACL_FILE"
  echo "topic write ${rt}" >>"$ACL_FILE"
done
echo "" >>"$ACL_FILE"

for comp in $COMPONENTS; do
  user=$(broker_user "$comp")
  pass=$(broker_pass "$comp")
  if [ -z "$pass" ]; then
    echo "WARN: empty password for ${comp} (${user}), set ${comp}_BROKER_PASSWORD" >&2
    pass="changeme_${comp}"
  fi
  mosquitto_passwd -b "$PASSWD_FILE" "$user" "$pass"

  [ "$comp" = "security_monitor" ] && continue
  ct=$(topic_for "$comp")
  rt=$(topic_for "replies.$comp")
  {
    echo "user ${user}"
    echo "topic read ${ct}"
    echo "topic read ${rt}"
    echo "topic write ${sm_topic}"
    echo ""
  } >>"$ACL_FILE"
done

if [ -n "$EXTERNAL_USERS" ]; then
  OLDIFS=$IFS
  IFS=,
  for entry in $EXTERNAL_USERS; do
    IFS=:
    set -- $entry
    ext_user=$(echo "$1" | tr -d ' ')
    ext_comp=$(echo "$2" | tr -d ' ')
    IFS=$OLDIFS
    [ -z "$ext_user" ] || [ -z "$ext_comp" ] && continue
    ext_var=$(echo "$ext_user" | tr '[:lower:]' '[:upper:]')_PASSWORD
    eval "ext_pass=\${${ext_var}:-}"
    [ -z "$ext_pass" ] && ext_pass="changeme_${ext_user}"
    mosquitto_passwd -b "$PASSWD_FILE" "$ext_user" "$ext_pass"
    {
      echo "user ${ext_user}"
      echo "topic write $(topic_for "$ext_comp")"
      echo "topic read $(topic_for "replies.$ext_comp")"
      echo ""
    } >>"$ACL_FILE"
  done
  IFS=$OLDIFS
fi

echo "Generated ${PASSWD_FILE} and ${ACL_FILE} for prefix ${TOPIC_PREFIX}"
