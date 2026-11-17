# ```map – an OpenStreetMap map

Lines:

| Line | Means |
|---|---|
| `marker 52.52, 13.40 Label` | a marker at latitude, longitude |
| `marker @entry` | a marker at the entry's `lat`/`lng` attributes |
| `track Name: lat,lng lat,lng …` | a line through points |
| `gpx <file id> Name` | a GPX track from a project file |
| `view lat,lng zoom 12` | the starting view |

Map tiles load only after the reader agrees (third-party content). Published pages list
the markers and tracks.
