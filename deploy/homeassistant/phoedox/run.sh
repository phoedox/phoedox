#!/usr/bin/with-contenv bashio
# Starts Phoedox with its data in the add-on's /data (part of HA backups).
set -e
export PHOEDOX_DATA_DIR=/data
export PHOEDOX_LISTEN=":8080"

if bashio::config.has_value 'public_url'; then
  export PHOEDOX_PUBLIC_URL="$(bashio::config 'public_url')"
fi

# Sign-in with Home Assistant: browsers go to ha_url, Phoedox itself talks
# to Home Assistant inside the add-on network.
if bashio::config.true 'sign_in_with_home_assistant' && bashio::config.has_value 'ha_url'; then
  export PHOEDOX_HA_URL="$(bashio::config 'ha_url')"
  export PHOEDOX_HA_INTERNAL_URL="http://homeassistant:8123"
fi

bashio::log.info "Starting Phoedox on port 8080"
exec /usr/local/bin/phoedox
