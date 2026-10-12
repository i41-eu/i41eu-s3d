# Task 003: components and their features (design)

Done 2026-10-11 (data/components.json, the page lists components with their features; apps use features). The design as it was:

## Why

data/parts.json mixes physical parts and what they deliver: the LTR-553 is two entries (ambient
light, proximity) with no link between them, servo feedback is a "with" on the servos, and
nothing says which features cannot run together (sending IR pauses the proximity sensor,
HARDWARE.md line 44; the owner wonders if the IR receiver shares the proximity's detector).

## Data

`data/components.json`: each physical thing once, where it is, and what it delivers.

```json
{
 "components": [
  {
   "id": "ltr553",
   "name": "Light and proximity sensor",
   "chip": "LTR-553ALS-WA (0x23)",
   "kind": "sensor",
   "where": { "part": "core", "offset": [6.5, 23.2, 33.6], "out": [0, 0, 1], "shape": {"kind": "rect", "w": 4.6, "h": 1.9, "r": 0.95}, "how": "near", "note": "...", "source": {} },
   "windows": [
    { "id": "ir-led", "name": "IR LED", "offset": [5.5, 23.2, 33.6], "shape": {"kind": "circle", "r": 0.6} },
    { "id": "detector", "name": "detector", "offset": [7.5, 23.2, 33.6], "shape": {"kind": "circle", "r": 0.6} }
   ],
   "features": [
    { "id": "light", "name": "Ambient light", "on": ["detector"], "data": "light_lux, light_ch0/ch1 (telemetry)", "sources": [] },
    { "id": "proximity", "name": "Proximity", "on": ["ir-led", "detector"], "data": "proximity (telemetry), proximity_near/_far (events); command proximity {on}", "excludes": ["ir-send"], "sources": [] }
   ],
   "sources": []
  }
 ]
}
```

- **component**: one physical part or chip: id, name, chip, kind (sensor, actuator, other),
  where (as today: part, offset, out, shape, how, note, source), optional windows (sub-places,
  each with its own shape: the LTR-553's two dots, the head touch's three zones, the dual mics),
  features, sources.
- **feature**: what software gets from it: id, name, on (which windows; none: the whole
  component), data (telemetry fields, events, commands, binary streams, as Embody Mode names
  them), excludes (feature ids that cannot run at the same time, with a source), sources.
- Feature ids stay unique across all components, so apps keep pointing at one id.

Examples to model: CoreS3 (ESP32-S3: chip temperature, Wi-Fi RSSI, free memory), AXP2101
(battery, charging, USB plugged, power button, power LED), SCS0009 x2 (angle, load,
temperature, voltage; torque; continuous yaw), Si12T (three zones: press with intensity,
release, swipe), ES7210 (two channels; a third input unused), BMI270 + BMM150 (IMU, magnetometer,
the 100 Hz stream), ports A/B/C and microSD (other, no features yet).

## Apps

`data/apps/<app>.json` `uses[].part` becomes `uses[].feature` (a feature id; or a component id
for all of its features). Raw data: one use per feature the dashboard shows.

## Page

- The list: components, each with its features nested (a checkbox each); a component's
  checkbox checks all its features.
- The robot: a component's shape, its windows' shapes inside it when picked; a feature
  highlights only its windows (light: the detector dot).
- Excludes shown in the feature's text ("not with: IR send").

## Tests

- Each feature id unique; each use points at a feature or a component; each "on" names a window
  of its component; excludes name existing features and are mutual.
- pagecheck: every feature of Raw data, picked, shows its component (as today for parts).

## Steps

1. Write components.json from parts.json (one script, kept out of the repo), add the features.
2. main.go types and tests; serve data/components.json; drop parts.json.
3. app.js: list and markers from components; windows; excludes in the text.
4. Apps: part to feature; pagecheck; README.
