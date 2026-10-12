# Task 001: the robot in 3D, turning, with its sensors and actuators

For a Claude cloud session in `mj41/i41eu-s3d`. Paste the part below the line as the session's
first message.

---

You work in `mj41/i41eu-s3d`, a public repo (Apache-2.0): nothing private in it — no tokens, no
hosts other than the public sites, no personal data. Push only a branch named
`claude/rotating-robot`, never `main`. The setup script (`setup.sh`) has cloned the related public
repos beside this one (`../i41eu-assets`, `../q41eu-raw`, `../i41eu-StackChan`, `../q41eu-pet`, `../q41eu-focus`, `../i41eu-manager`, `../i41eu-web`,
`../i41eu-arch`); read the README first.

The goal of the project: s.i41.eu/s3d shows the Stackchan robot in 3D in the browser; it turns (by
itself, and by drag), its sensors and actuators are marked on it (a toggle hides them), and an app
picked shows what each sensor and actuator does in that app. Everything shown is generated from
data, so a new app is a new description, not new code.

This task builds the first working version:

1. **The model from robot3d.** `../i41eu-assets/robot3d` is a Go software renderer of the
   robot (M5Stack's StackChan STLs plus the drawn CoreS3; parts and offsets in `mesh.go`). Do not
   rewrite it: write a Go command here (`cmd/s3dgen`, importing the public module
   `github.com/i41-eu/i41eu-assets/robot3d`, or with a `replace` to `../i41eu-assets` if a
   change there is needed — then say what change, as a separate patch) that exports the robot's
   geometry, each part named and placed as robot3d places it, to a glTF binary (`web/robot.glb`).
   robot3d already exports what this needs (i41eu-assets 6b1e7cf) — use it, do not write your
   own: `robot3d.Parts() []Part` (the model at rest in mm, Y up, facing +Z, the yaw axis is Y;
   each Part has Name — plate, servo, servo-cover, body, core, led-bar-left,
   led-bar-right — Joint "base"/"yaw"/"head", Color, Positions and Normals, three per triangle),
   `robot3d.PitchPivot()` (a point on the head's pitch axis, along X; +pitch lifts the front, +yaw
   turns to the robot's left, +X) and `robot3d.Screen()` (the screen's centre on the CoreS3's front,
   its width and height in mm; 320×240 px). The screen's picture, ports and labels are drawn in
   robot3d's shader, not modelled: the glTF gets a textured quad at `Screen()` for the screen. Keep
   the joints as glTF nodes (base, yaw, head with the pitch pivot) so the page can turn the head.
2. **The parts as data.** `data/parts.json`: each sensor and actuator — id, name, kind (sensor or
   actuator), what it measures or does, where on the robot (a part and an offset, from robot3d's
   geometry), its source (the file and line in the firmware's or the wire package's docs where it
   is described). Generated or written from those docs, every entry with its source; nothing
   invented — what the docs do not say is left out, and listed in the summary.
3. **The apps as data.** `data/apps/<app>.json` for the apps the manager's catalog lists that have
   their own repo here (pet, focus): for each sensor and actuator the app uses, what it does with
   it, each with its source (file and line). An app not described there is not added.
4. **The page.** `web/index.html` and one script: WebGL2 (or three.js, pinned version, loaded from
   cdnjs/jsdelivr, if it is clearly simpler — say which and why), the robot from `robot.glb`,
   turning slowly by itself, drag to turn, the sensors and actuators as markers with their names
   (a toggle: on by default), an app picker that highlights the parts that app uses and lists what
   each does. Works on a phone (touch drag, one column) and a desktop.
5. **The site.** A Go binary in the style of `../i41eu-web` (one binary, the pages embedded, no
   dependencies beyond the standard library), `go run .` serves it on :8080; a Dockerfile like
   i41eu-web's.
6. **Tests.** Go tests: the glTF exported has every part robot3d has, with the placement robot3d
   uses (a few points checked); `parts.json` and `apps/*.json` load, every app's parts exist in
   `parts.json`, every entry has a source. `go vet ./...` and `go test ./...` green.

When done: push `claude/rotating-robot`, and end with a short summary — what was built, the tests
and their results, what the docs did not say (left out), and any patch for i41eu-assets.
