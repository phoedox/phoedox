# Phoedox add-on

Phoedox documents your home: rooms, devices, network, manuals, warranties and an
emergency folder, connected in a knowledge graph. This add-on runs Phoedox next
to Home Assistant. Its data (SQLite database and files) lives in the add-on and
is part of Home Assistant backups.

## Setup

1. Set **Home Assistant address** to how your browser reaches Home Assistant,
   e.g. `http://homeassistant.local:8123`.
2. Set **Phoedox address** to how your browser reaches the add-on,
   e.g. `http://homeassistant.local:8080`.
3. Start the add-on and open the web interface.
4. Click **Sign in with Home Assistant**. Home Assistant asks you to confirm the
   sign-in. The Home Assistant owner becomes a Phoedox administrator; other
   Home Assistant users get normal accounts.

Phoedox keeps no access to Home Assistant from the sign-in: it only asks who
signed in and revokes the token right away.

## Connect Home Assistant devices

To import areas and devices, switch devices and record their values:

1. In Home Assistant, create a long-lived access token (your profile →
   Security).
2. In Phoedox, open a project → Project settings → Plugins → Home Assistant,
   and set `url` to `http://homeassistant:8123` and `token` to the token.

Owners can then share single devices with other people and allow AI clients to
switch a device without confirmation (both on the device's page).

## Notes

- The web interface runs on its own port; Home Assistant's sidebar panel
  (ingress) is not supported yet.
- For access from outside your network, put the add-on behind your reverse
  proxy and set **Phoedox address** to the https address.
- Emergency commands are available in the add-on's terminal as `phx admin …`
  with `PHOEDOX_DATA_DIR=/data`.
