# ```floorplan – floor plan excerpts

`plan:` the floor plan page, optionally `room:` a room by its name or entry to cut the view
to it; `devices: no` hides devices. Walls, doors, windows and the devices placed on the
plan are shown with their live status. Floor plans themselves are documents of type
floorplan (`../floorplans.md`).

````markdown
```floorplan
plan: ground-floor
room: Kitchen
```
````

It only names entries and documents and always shows the current state with live
status; `validate_blocks` warns when a named entry, floor plan or room does not exist.
Published pages draw floor plans that are part of the publication.
