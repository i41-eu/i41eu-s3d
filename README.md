# i41eu-s3d

[s.i41.eu/s3d](https://s.i41.eu/s3d/): the Stackchan robot in 3D, in the browser — turn it round, see
its sensors and actuators (or hide them), pick an app and see what each sensor and actuator does
in it. Everything shown is generated from data: the robot's parts and their places come from
`robot3d` in [i41eu-assets](https://github.com/i41-eu/i41eu-assets), the sensors and
actuators from the firmware and the wire package, each app's use of them from the app's own
description — so a new app is a new description, not new code.

Related projects (all public, cloned beside this one by `setup.sh`):

| Repo | What it gives here |
|---|---|
| [i41eu-assets](https://github.com/i41-eu/i41eu-assets) | `robot3d`: the 3D model (M5Stack's StackChan STLs, MIT, and the CoreS3 drawn), parts and offsets (`robot3d/mesh.go`) |
| [q41eu-raw](https://github.com/i41-eu/q41eu-raw) | the wire package: the sensor and command list |
| [i41eu-StackChan](https://github.com/i41-eu/i41eu-StackChan) | the firmware's sensors and actuators (`firmware/main/apps/app_embody_mode/README.md`) |
| [q41eu-pet](https://github.com/i41-eu/q41eu-pet), [q41eu-focus](https://github.com/i41-eu/q41eu-focus), [q41eu-dev](https://github.com/i41-eu/q41eu-dev) | what each app does with them |
| [i41eu-manager](https://github.com/i41-eu/i41eu-manager) | the app catalog |
| [i41eu-web](https://github.com/i41-eu/i41eu-web) | the sites' pattern: one Go binary, pages embedded |
| [i41eu-arch](https://github.com/i41-eu/i41eu-arch) | the docs |

Work is done in `claude/…` branches (a Claude cloud session may make them); `main` is merged by
hand after review. `tasks/` holds the tasks given to cloud sessions.

## What is here

| Path | What it is |
|---|---|
| `cmd/s3dgen` | writes `web/robot.glb` from robot3d (`robot3d.Parts`, `PitchPivot`, `Screen`): nodes robot (mm → m) → base → yaw → head (at the pitch pivot), each part a named node under its joint, the screen a textured quad |
| `data/components.json` | the robot's components, each physical part or chip once: where it is (a robot3d part and an offset in mm; left out where the docs do not say), or its windows (sub-places: the light sensor's IR LED and detector, the head touch's three zones, the power chip's USB-C, button and LED); and its features, what software gets from it (id, kind sensor/actuator/other, what it does, `on`: the windows it works through, `excludes`: features that cannot run at the same time, on both sides, with a source); every entry with its sources (repo, commit, file, line, a quote from that line) |
| `data/decals.json`, `data/decals/` | pictures painted on the robot where robot3d draws no text (the base's bottom label, the side stickers): crops of the owner's photos, straightened, saved without their metadata; s3dgen paints them into the parts' textures |
| `data/apps/<app>.json` | what an app does with each feature it uses (`uses[].feature`: a feature id, or a component id for all of its features), with sources |
| `web/index.html`, `web/app.js` | the page: three.js r160 (`web/three/`, npm `three@0.160.0`, MIT, served from here: no third party sees a visitor), the robot turning, drag to turn, markers, an app picker |
| `main.go` | the site: one binary, standard library only, everything embedded |
| `cmd/pagecheck` | a headless check of the page in Chrome over the DevTools protocol (standard library only) |

```bash
./setup.sh                 # the related repos beside this one
go run ./cmd/s3dgen        # web/robot.glb (after a change in robot3d)
go vet ./... && go test ./...   # the sources' quotes are checked in the repos beside this one
go run .                   # http://localhost:8080
```

The page check needs Chrome or Chromium installed (found on `PATH`, in the usual places on macOS
and Windows, or in Playwright's download; or give `-chrome` or `$CHROME`). From the
repository's root:

```bash
go run ./cmd/pagecheck     # builds and starts the site on a free port, checks it at 1200 and 420 px
```

It checks that the robot loads and fills most of the view (also after a resize), that the
markers are grouped and a tap on a group opens its list, that no labels overlap while the robot
turns, that nothing overflows the side panel or the page, and that there are no console errors;
it prints each check, the path of a screenshot of each width (in a temporary directory, or
`-o dir`), and exits 1 if a check failed. `-url` checks a running site instead.

**robot3d's details as textures.** `s3dgen` bakes what robot3d's renderer draws on the CoreS3,
the main body and the board through the open back (the glass front and the red ring, the sensors' dots, the vents,
the ports and the power button, the labels) into textures, from `robot3d.Surface`. `go.mod` pins
i41eu-assets at the commit that has it (branch `claude/robot3d-interior` there, with the inside of
the head as on the photos: the servo body's top cover and the black pitch servo); once that is in
its `main`, `go get` the merge.

**The page's light.** No shadows. Photo-like: a soft studio round the robot (three.js's RoomEnvironment) lights and reflects in it, with a filmic tone curve set so the shell shows the light grey of the owner's photos; the screen shows its picture as it is (Embody Mode's launcher, `screens/launcher.png` in i41eu-assets, as on the real robot). Its 12 LEDs light their
bars as robot3d draws them (six under each bar; a lit LED shows its colour and lights the shell
round the bar a little, an unlit one the bar's milky plastic): light blue as on the photos of a
real robot, or what an app's `leds` gives (with its sources): one `color` for all, or `each` LED
in robot3d's `-leds` form, e.g. Focus's 4 + 4 red while focusing.

**Comparing with M5Stack's model.** M5Stack's StackChan app carries its own photo-like model of
the robot (`../StackChan/app/assets/stack_chan_model.glb`, beside this repo after `setup.sh`):
`go run ./cmd/pagecheck -compare ../StackChan/app/assets/stack_chan_model.glb -o dir` draws it
(left) and ours (right) from the same views, both 70.5 mm high (`compare-<view>.png`; not kept
here: the pictures show M5Stack's model).

**Comparing with photos.** `?view=front|back|left|right|head|leds|top|photo|seam|holes|topseam|under|bottom` holds the robot at a fixed
view (add `#app` for an app's LEDs); `go run ./cmd/pagecheck -views all -o dir` saves each.
Before/after pairs of each change to the model's look are in `tests/visual/`. The glTF's test
counts each part's open edges (must not grow) and triangles wound against their normals (none).

**Picking and showing features.** Each app has its own address (`#raw`, `#pet`, `#focus`); the
panel lists the app's features, or all components with their features nested (a component's box
checks all of its features; a feature that cannot run with another says so), with a checkbox each, and All checks or clears them: only the
checked ones are marked on the robot. A pick in the list (or on a label) turns that side of the
robot to the front, stops the turning (the button above the robot starts it again; the arrows ◀ ▶
turn to the next side; a drag stops it too) and makes its marker pulse. By itself the robot turns
side by side, slowly, with a pause on each. Each marker has its own shade (sensors blue, actuators
orange), the same on its outline, its label and its row in the list; a place with two parts
labels both names. Markers sit on the robot's surface, where a ray from outside
along the entry's `out` meets it, drawn as the part's own outline (`shape`: a circle round the camera, a
pill round the light sensor's windows, rounded rects round the ports and LED bars, a ring round the turntable); the screen's entries outline the screen; a feature shows on
its component's places, or only on the windows it works through (ambient light: the detector). The header links the Stackchan sites (s.i41.eu,
m.i41.eu and the apps' own sites).

A new app is a new `data/apps/<app>.json`; a new component or feature a new entry in `data/components.json`.

## Trademark

Stack-chan is a registered trademark of Shinya Ishikawa; this project is independent of it.

## License

Apache License 2.0 (see LICENSE); the StackChan structure files it shows are M5Stack's, MIT.
