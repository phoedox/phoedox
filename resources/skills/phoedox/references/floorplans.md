# Floor plans

All values in millimetres. Create the room and device entries first. Read the plan
with `get_document` when changing it and keep elements you do not change exactly
(including ids). Rooms are polygons pointing to room entries; devices are points;
walls are segments with a thickness; openings (doors, windows) sit on a wall; cables
are point routes with a kind and optional from/to entries. `save_floorplan` sends the
whole plan; Phoedox links devices `located_in` their room.

A complete plan of a 6 × 4 m flat with two rooms, one wall with a door and a router.
Ids are your own short names (letters, digits, `-`, `_`); room points run clockwise;
`entity` fields are slugs of existing entries. Pass `document` (the slug) instead of
`title` to replace an existing plan.

```json
{
  "project": "home",
  "title": "Ground floor",
  "floorplan": {
    "floor": "ground-floor",
    "width": 6000,
    "height": 4000,
    "rooms": [
      { "id": "r1", "name": "Living room", "entity": "living-room", "color": "blue",
        "points": [[0, 0], [4000, 0], [4000, 4000], [0, 4000]] },
      { "id": "r2", "name": "Office", "entity": "office", "color": "green",
        "points": [[4000, 0], [6000, 0], [6000, 4000], [4000, 4000]] }
    ],
    "walls": [
      { "id": "w1", "a": [4000, 0], "b": [4000, 4000], "thickness": 115 }
    ],
    "openings": [
      { "id": "o1", "wall": "w1", "kind": "door", "offset": 2000, "width": 900 }
    ],
    "devices": [
      { "id": "d1", "entity": "router", "x": 4800, "y": 600 }
    ],
    "cables": [
      { "id": "c1", "kind": "network", "points": [[4800, 600, 300], [1000, 600, 300]],
        "from": "router", "to": "tv" }
    ]
  }
}
```

Colors: gray, red, orange, yellow, green, blue, purple, pink. Cable points are
`[x, y, height above the floor]`; kinds are network, power, coax and other.

Read a plan with `get_document` before changing it. Phoedox links devices
`located_in` their room and shows the plan's rooms and devices in the knowledge
graph (see `knowledge-graph.md`).
