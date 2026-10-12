# Task 002: the view polished

For the s3d cloud session. Paste the part below the line as its message.

---

Bring the repo up to date first: `git fetch origin && git checkout -B claude/view-polish origin/main`
(001 is merged: main is 21ab47e). Same rules: public repo — nothing private; push only
`claude/view-polish`, never `main`.

The review of 001 (the stackchan project's session, in headless Chrome at 1200 and 420 px) asks:

1. **Overlapping labels**: at the screen and the CoreS3's centre the labels of the touch screen,
   display, IMU, light and proximity sensors sit on top of each other. Group the parts at one
   place into one marker (its label a count, the list on hover/tap), or fan them out with leader
   lines — your choice, say which; it must stay readable at 420 px.
2. **Fit the robot to the view**: it fills about a third of it now; frame it (its bounding box) to
   fill most of the view at any width, also after a resize.
3. **Long source links wrap** in the side panel (they overflow now).
4. Optional: the vents, ports and the red ring on the shell, as robot3d's shader draws them
   (`../i41eu-assets/robot3d`: as textures or decals from the same data, not hand-drawn).
5. Optional: a default screen picture larger than the tiny face (from the assets' screens, if one
   fits; say which).

Tests: what can be tested in Go stays tested (the data, the glTF); for the page, a headless check
if the environment has a browser (say if not). `go vet ./...`, `go test ./...` green. Push and end
with a short summary: what changed, what was optional and done or left.
