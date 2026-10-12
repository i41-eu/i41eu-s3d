// The page: robot.glb turning on its own and by drag, the sensors and actuators from
// data/components.json (each component's places as markers on their parts, its features in the
// list) and an app from data/apps.json highlighting the features it uses. Everything shown comes from those files.
import * as THREE from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { CSS2DRenderer, CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js';

const stage = document.getElementById('stage');
const status = document.getElementById('status');
const panel = document.getElementById('panel');
const appSelect = document.getElementById('app');
const markersBox = document.getElementById('markers');

const css = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();
const deg = Math.PI / 180;

// The scene: the robot in metres (robot.glb's root scales robot3d's millimetres).
const renderer = new THREE.WebGLRenderer({ antialias: true });
renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
// Photo-like: a soft studio round the robot (three.js's RoomEnvironment) lights and reflects in
// every surface, and a filmic tone curve keeps the light shell from burning out.
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 0.45; // the shell's side about #b5b5b9, as on the owner's photos
stage.appendChild(renderer.domElement);
const labels = new CSS2DRenderer();
labels.domElement.style.position = 'absolute';
labels.domElement.style.inset = '0';
labels.domElement.style.pointerEvents = 'none';
labels.domElement.style.zIndex = '1'; // its labels' own z-indexes stay inside it, under the list
stage.appendChild(labels.domElement);

const scene = new THREE.Scene();
scene.background = new THREE.Color(css('--panel'));
scene.environment = new THREE.PMREMGenerator(renderer).fromScene(new RoomEnvironment(), 0.04).texture;
scene.add(new THREE.HemisphereLight(0xffffff, 0xd2d4d8, 0.9)); // light from below too: holes and recesses grey, not black (the owner's photos)
const key = new THREE.DirectionalLight(0xffffff, 1.6);
key.position.set(-0.025, 0.2, 0.045); // high above, a little to the front left (metres); no shadows
scene.add(key);
const fill = new THREE.DirectionalLight(0xffffff, 0.3);
fill.position.set(0.75, 0.25, 0.45);
scene.add(fill);

const camera = new THREE.PerspectiveCamera(32, 1, 0.005, 2);
const target = new THREE.Vector3(0, 0.036, 0);
const turntable = new THREE.Group(); // turns the whole robot
scene.add(turntable);

let distance = 0.26, elevation = 12 * deg;
// A fixed view (?view=front|back|left|right|head|leds|top|photo|seam|holes|topseam|under|bottom|led, for comparing with photos): the robot
// turned to it (yaw, degrees: the robot's side the camera sees), not spinning; a close-up nearer
// (zoom) and aimed at a height (aim, 0 the robot's foot, 1 its top).
const views = {
  front: { yaw: 0 }, back: { yaw: 180 }, left: { yaw: -90 }, right: { yaw: 90 },
  head: { yaw: -30, elevation: 20, zoom: 0.55, aim: 0.72 },
  leds: { yaw: -55, elevation: 38, zoom: 0.45, aim: 0.95 },
  top: { yaw: 0, elevation: 70, zoom: 0.6, aim: 0.95 },
  photo: { yaw: -35, elevation: 22, zoom: 0.75, aim: 0.55 },
  seam: { yaw: -50, elevation: 25, zoom: 0.35, aim: 0.8 },
  holes: { yaw: -90, elevation: 0, zoom: 0.3, aim: 0.85 },
  topseam: { yaw: -20, elevation: 55, zoom: 0.3, aim: 0.98 },
  under: { yaw: -15, elevation: -30, zoom: 0.75, aim: 0.35 },
  bottom: { yaw: 0, elevation: -80, zoom: 0.8, aim: 0.2 },
  led: { yaw: 0, elevation: -25, zoom: 0.45, aim: 0.26 },
};
const view = views[new URLSearchParams(location.search).get('view')] || null;
if (view && view.elevation !== undefined) elevation = view.elevation * deg;
let fitBox = null; // the robot's bounds at rest: {r (about the yaw axis), y0, y1}, metres
function placeCamera() {
  camera.position.set(0, target.y + distance * Math.sin(elevation), distance * Math.cos(elevation));
  camera.lookAt(target);
}

// The LEDs, as robot3d draws them (i41eu-assets robot3d/shade.go: ledBar, barGlow): six
// under each bar (on the left, +X, LED 0 at the front; on the right LED 11 at the front). A lit
// LED's part of its bar shows its colour (0.35 of it, and 0.75 as its own light), an unlit one
// the bar's milky plastic (its colour in robot.glb); on the shell's sides a lit LED adds its light
// round the bar, fading over about a millimetre (0.35 exp(-d / 1.1 mm), out to 4 mm). Only the
// colours change: an app's leds from data (each LED, or one colour for all), else ledDefault.
const ledDefault = '#7fd4f5'; // as on the photos of a real robot (light blue)
const ledUniforms = {
  uLed: { value: Array.from({ length: 12 }, () => new THREE.Color()) },
  uLedOn: { value: new Array(12).fill(0) },
  uBar: { value: new THREE.Vector4() }, // the bars' z0, z1, y0, y1 at rest (mm), from robot.glb
};
// ledColors gives the 12 LEDs' colours ('' or null: off) from an app's leds: each, robot3d's
// -leds form ("#rrggbb*12", or 12 comma-separated, empty for off), or color for all.
function ledColors(leds) {
  if (!leds) return new Array(12).fill(''); // no app: off, as on the owner's photos (the launcher)
  if (!leds.each) return new Array(12).fill(leds.color);
  const all = /^(#[0-9a-f]{6})\*12$/.exec(leds.each);
  return all ? new Array(12).fill(all[1]) : leds.each.split(',');
}
function setLEDs(leds) {
  ledColors(leds).forEach((hex, i) => {
    ledUniforms.uLedOn.value[i] = hex ? 1 : 0;
    if (hex) ledUniforms.uLed.value[i].set(hex);
  });
}
const ledGLSL = `
uniform vec3 uLed[12];
uniform float uLedOn[12];
uniform vec4 uBar;
varying vec3 vRest;
varying vec3 vRestNormal;
// glowAt: the light in a light guide at z, as robot3d draws it: each lit LED strongest over its
// sixth of the guide and fading along it (one LED lights about two thirds of the guide, the
// owner's photo); .a: how lit it is there.
vec4 glowAt(float z, bool left) {
  vec3 sum = vec3(0.0); float w = 0.0;
  for (int k = 0; k < 6; k++) {
    int i = left ? k : 11 - k;
    float zk = uBar.y - (float(k) + 0.5) * (uBar.y - uBar.x) / 6.0;
    float wk = exp(-abs(z - zk) / 6.0) * uLedOn[i];
    sum += uLed[i] * wk; w += wk;
  }
  return vec4(sum / max(w, 1.0), min(w, 1.0));
}
`;
// withLEDs patches a material: the bar's (bar true) or the shell's glow round the bars.
function withLEDs(material, bar) {
  material = material.clone();
  material.onBeforeCompile = (shader) => {
    Object.assign(shader.uniforms, ledUniforms);
    shader.vertexShader = 'varying vec3 vRest;\nvarying vec3 vRestNormal;\n' + shader.vertexShader.replace(
      '#include <begin_vertex>', '#include <begin_vertex>\nvRest = position;\nvRestNormal = normal;');
    const lit = bar ? `
      vec4 g = glowAt(vRest.z, vRest.x < 0.0); // the firmware's left strip (0-5): left as you face the screen, -X
      if (g.a > 0.0) { diffuseColor.rgb = mix(diffuseColor.rgb, g.rgb / max(g.a, 1e-4) * 0.35, g.a); totalEmissiveRadiance += mix(g.rgb, vec3(g.a), 0.4) * 2.2; } // the diffuser glows pale and bright (the owner's photos)` : `
      if (abs(vRestNormal.x) > 0.97 && abs(vRest.x) > 26.0) {
        float dz = max(0.0, max(uBar.x - vRest.z, vRest.z - uBar.y));
        float dy = max(0.0, max(uBar.z - vRest.y, vRest.y - uBar.w));
        float d = length(vec2(dz, dy));
        vec4 g = glowAt(clamp(vRest.z, uBar.x, uBar.y), vRest.x < 0.0);
        if (d < 4.0 && g.a > 0.0) totalEmissiveRadiance += g.rgb * 0.35 * exp(-d / 1.1);
      }`;
    shader.fragmentShader = ledGLSL + shader.fragmentShader.replace('#include <emissivemap_fragment>', '#include <emissivemap_fragment>' + lit);
  };
  material.customProgramCacheKey = () => (bar ? 'led-bar' : 'led-glow');
  return material;
}
// withChipLEDs patches an LED chips' material: each chip its LED's colour when lit, bright.
function withChipLEDs(material) {
  material = material.clone();
  material.onBeforeCompile = (shader) => {
    Object.assign(shader.uniforms, ledUniforms);
    shader.vertexShader = 'varying vec3 vRest;\nvarying vec3 vRestNormal;\n' + shader.vertexShader.replace(
      '#include <begin_vertex>', '#include <begin_vertex>\nvRest = position;\nvRestNormal = normal;');
    shader.fragmentShader = ledGLSL + shader.fragmentShader.replace('#include <emissivemap_fragment>', `#include <emissivemap_fragment>
      int k = int(clamp((uBar.y - vRest.z) / ((uBar.y - uBar.x) / 6.0), 0.0, 5.0));
      int i = vRest.x < 0.0 ? k : 11 - k; // the firmware's left strip (0-5) on -X
      if (uLedOn[i] > 0.5) { diffuseColor.rgb = uLed[i] * 0.5; totalEmissiveRadiance += mix(uLed[i], vec3(1.0), 0.3) * 2.5; }`);
  };
  material.customProgramCacheKey = () => 'led-chips';
  return material;
}
function lightLEDs(robot) {
  const bar = robot.getObjectByName('led-bar-left');
  bar.geometry.computeBoundingBox();
  const { min, max } = bar.geometry.boundingBox; // millimetres at rest, the bar along Z
  ledUniforms.uBar.value.set(min.z, max.z, min.y, max.y);
  for (const name of ['led-bar-left', 'led-bar-right']) {
    const o = robot.getObjectByName(name);
    o.material = withLEDs(o.material, true);
  }
  for (const name of ['led-chips-left', 'led-chips-right']) { // the LEDs inside, under the bars
    const o = robot.getObjectByName(name);
    if (o) o.material = withChipLEDs(o.material);
  }
  const body = robot.getObjectByName('body');
  body.material = withLEDs(body.material, false);
  setLEDs(null);
}

function resize() {
  const w = stage.clientWidth, h = stage.clientHeight;
  renderer.setSize(w, h);
  labels.setSize(w, h);
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
  fit();
  placeCamera();
}
new ResizeObserver(resize).observe(stage);

// Framing: the robot turns, so what must fit is the cylinder round the yaw axis that holds it at
// rest; the camera is as near as lets that fill the view (a little margin), at any width.
function fit() {
  if (!fitBox) { placeCamera(); return; }
  const { r, y0, y1 } = fitBox;
  const v = Math.tan(camera.fov * deg / 2), hz = v * camera.aspect;
  target.set(0, (y0 + y1) / 2, 0);
  const margin = 0.98; // a little closer: the turning robot's corners may touch the edge
  distance = r + margin * Math.max((y1 - y0) / 2 / v, r / hz);
  if (view && view.zoom) {
    distance *= view.zoom;
    target.y = y0 + view.aim * (y1 - y0);
  }
  placeCamera();
}
function measure(robot) {
  robot.updateWorldMatrix(true, true);
  const box = new THREE.Box3().setFromObject(robot);
  let r = 0;
  for (const x of [box.min.x, box.max.x]) for (const z of [box.min.z, box.max.z]) r = Math.max(r, Math.hypot(x, z));
  fitBox = { r, y0: box.min.y, y1: box.max.y };
  fit();
}

// Turning: by itself, side by side (a quarter turn, slow and smooth, then a pause on each side),
// until the button stops it, or an arrow (the next side, left or right), a drag (which turns it,
// and tilts the view with a mouse) or a pick in the list (which turns the picked part to the
// front) does. Up and down tilts the view, with a finger too (the page scrolls beside the robot).
const sideMs = 2600, holdMs = 2400; // a quarter turn by itself, and the pause on each side
let nextSideAt = 0;
const spinButton = document.getElementById('spin');
let autoTurn = false;
function setAutoTurn(on) {
  autoTurn = on;
  spinButton.textContent = on ? '⏸ Stop turning' : '▶ Turn';
  spinButton.setAttribute('aria-pressed', String(on));
}
setAutoTurn(!view);
if (view) turntable.rotation.y = view.yaw * deg;
spinButton.addEventListener('pointerdown', (e) => e.stopPropagation());
spinButton.addEventListener('click', (e) => { e.stopPropagation(); turnTo = null; setAutoTurn(!autoTurn); });
let drag = null, turnTo = null;
stage.addEventListener('pointerdown', (e) => {
  drag = { x: e.clientX, y: e.clientY, id: e.pointerId };
  stage.setPointerCapture(e.pointerId);
  stage.classList.add('dragging');
});
stage.addEventListener('pointermove', (e) => {
  if (!drag || e.pointerId !== drag.id) return;
  const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
  if (dx || dy) { setAutoTurn(false); turnTo = null; }
  turntable.rotation.y += dx * 0.01; // across: turn; up and down: tilt (a finger too)
  elevation = Math.min(70 * deg, Math.max(-20 * deg, elevation + dy * 0.006));
  fit();
  drag.x = e.clientX; drag.y = e.clientY;
});
const endDrag = () => { drag = null; stage.classList.remove('dragging'); };
stage.addEventListener('pointerup', endDrag);
stage.addEventListener('pointercancel', endDrag);

// faceTo turns the robot (and tilts the view) so that a direction at rest faces the camera.
// A part facing straight down (under the CoreS3) is turned by where it is: its side to the front.
function faceTo(out, at) {
  const from = turntable.rotation.y;
  let to = from;
  const dir = Math.hypot(out.x, out.z) > 0.3 ? out : out.y < -0.5 && at && Math.hypot(at.x, at.z) > 1 ? at : null;
  if (dir) {
    const a = -Math.atan2(dir.x, dir.z);
    to = from + ((((a - from) % (2 * Math.PI)) + 3 * Math.PI) % (2 * Math.PI)) - Math.PI;
  }
  const up = out.y / out.length();
  turnTo = { from, to, el0: elevation, el1: up > 0.7 ? 55 * deg : up > 0.2 ? 30 * deg : up < -0.5 ? -30 * deg : 12 * deg, t0: performance.now(), ms: 700 };
  setAutoTurn(false);
}
// turnSide turns the robot to its next side (dir 1 or -1), from where it is.
function turnSide(dir, ms) {
  const from = turntable.rotation.y, q = Math.PI / 2;
  const to = (Math.round(from / q - dir * 0.3) + dir) * q;
  turnTo = { from, to, el0: elevation, el1: view ? elevation : 12 * deg, t0: performance.now(), ms };
}
for (const [id, dir] of [['turn-left', -1], ['turn-right', 1]]) {
  const b = document.getElementById(id);
  b.addEventListener('pointerdown', (e) => e.stopPropagation());
  b.addEventListener('click', (e) => { e.stopPropagation(); setAutoTurn(false); turnSide(dir, 900); });
}
function turning(now) {
  if (autoTurn && !drag && !turnTo && now > nextSideAt) turnSide(1, sideMs);
  if (!turnTo) return;
  const t = Math.min(1, (now - turnTo.t0) / turnTo.ms), s = t * t * (3 - 2 * t);
  turntable.rotation.y = turnTo.from + (turnTo.to - turnTo.from) * s;
  elevation = turnTo.el0 + (turnTo.el1 - turnTo.el0) * s;
  placeCamera();
  if (t >= 1) { turnTo = null; nextSideAt = now + holdMs; }
}

// The head's joints (robot.glb's yaw and head nodes) and a short move to show a servo.
let yawNode = null, headNode = null, move = null;
function showMove(id) {
  if (id === 'yaw-servo') move = { joint: 'yaw', t0: performance.now(), ms: 2400 };
  if (id === 'pitch-servo') move = { joint: 'pitch', t0: performance.now(), ms: 2400 };
}
function pose(now) {
  if (!move || !yawNode) return;
  const t = (now - move.t0) / move.ms;
  if (t >= 1) { yawNode.rotation.y = 0; headNode.rotation.x = 0; move = null; return; }
  const s = Math.sin(t * Math.PI * 2) * Math.sin(t * Math.PI);
  if (move.joint === 'yaw') yawNode.rotation.y = 35 * deg * s;
  else headNode.rotation.x = -20 * deg * Math.abs(s); // +pitch lifts the front: about X by -pitch
}

// Markers: one per place (a component's, or each of its windows), on the robot's surface: where
// a ray from outside along the place's out (data/components.json) meets the robot at rest. Entries at the same place (within near mm, e.g.
// ambient light and proximity, one chip) share one; its label is the name, or a count with the
// list on hover or tap. The screen's entries (area screen) outline the screen. Only the selected
// entries of the list show.
const near = 1.5;
// Each marker its own shade (its outline, its label and its row in the list): sensors blues,
// actuators oranges, so two of a kind in view still differ.
// All 4.5:1 or more with white text (the labels).
const sensorShades = ['#1f6fd0', '#0c8097', '#5a4fcf', '#2b4fa0', '#168275', '#3377be', '#4069b8', '#0f6f8f'];
const actuatorShades = ['#bb571b', '#c23a2b', '#9c6b0e', '#a8461c', '#b05e2d', '#b5521f', '#cb4554', '#9a6a10'];
const otherShades = ['#398346', '#5f6b7a', '#2f7d6b', '#7a6a4a', '#4f7a2f', '#6a5a8a'];
const markers = []; // {ids, items, obj, label, el, colour, out}
let places = [], features = [], apps = [], byId = new Map(), components = [];
let robotRoot = null; // the robot's scene (robot.glb), for the parts a place names to see through
// fromComponents: the markers' places (a component's own, or each of its windows: id component/window)
// and the list's features, each with the places it works through (its "on" windows; none: all of
// its component's places).
function fromComponents(doc) {
  components = doc.components;
  for (const c of components) {
    const own = c.windows ? c.windows.map((w) => ({ id: c.id + '/' + w.id, name: w.name, kind: c.kind, where: w.where, comp: c }))
      : c.where ? [{ id: c.id, name: c.name, kind: c.kind, where: c.where, comp: c }] : [];
    places.push(...own);
    for (const f of c.features) {
      const at = f.on ? f.on.map((w) => c.id + '/' + w) : own.map((p) => p.id);
      features.push({ ...f, chip: c.chip, comp: c, places: at, where: (own.find((p) => p.id === at[0]) || {}).where });
    }
  }
  byId = new Map(features.map((f) => [f.id, f]));
}
let picked = null; // the app shown
const selected = new Set(); // ids checked in the list
let focused = null; // the marker picked last in the list

function outOf(where) {
  if (where.out) return new THREE.Vector3().fromArray(where.out).normalize();
  const o = new THREE.Vector3(where.offset[0], 0, where.offset[2]);
  return o.lengthSq() > 1 ? o.normalize() : new THREE.Vector3(0, 0, 1);
}
// onSurface gives where the ray from outside along out meets the robot, in node's space.
function onSurface(robot, node, offset, out) {
  const p = node.localToWorld(new THREE.Vector3().fromArray(offset));
  const ray = new THREE.Raycaster(p.clone().addScaledVector(out, 0.2), out.clone().negate(), 0, 0.2);
  const hit = ray.intersectObject(robot, true).find((h) => h.object.isMesh);
  return node.worldToLocal((hit ? hit.point : p).clone());
}
// Outlines: a part is shown by its own shape on the robot (where.shape: a circle of radius r, or
// a rect w x h with corners rounded by r, in mm), lying on the surface, facing out (its w across,
// its h up; on top, its h toward the back): a frame and a faint fill. Without a shape, a dot.
const frameT = 0.6; // the frame's width, mm
function outlinePath(sh, grow) {
  const p = new THREE.Shape();
  if (sh.kind === 'circle') { p.absarc(0, 0, sh.r + grow, 0, Math.PI * 2, false); return p; }
  const w = sh.w / 2 + grow, h = sh.h / 2 + grow, r = Math.min((sh.r || 0) + grow, w, h);
  p.moveTo(-w + r, -h).lineTo(w - r, -h).absarc(w - r, -h + r, r, -Math.PI / 2, 0, false)
    .lineTo(w, h - r).absarc(w - r, h - r, r, 0, Math.PI / 2, false)
    .lineTo(-w + r, h).absarc(-w + r, h - r, r, Math.PI / 2, Math.PI, false)
    .lineTo(-w, -h + r).absarc(-w + r, -h + r, r, Math.PI, Math.PI * 1.5, false);
  return p;
}
function outline(sh, colour) {
  const g = new THREE.Group();
  const outer = outlinePath(sh, frameT);
  outer.holes.push(outlinePath(sh, 0));
  const mat = (opacity) => new THREE.MeshBasicMaterial({ color: colour, transparent: true, opacity, depthWrite: false,
    polygonOffset: true, polygonOffsetFactor: -4, polygonOffsetUnits: -4, side: THREE.DoubleSide });
  const frame = new THREE.Mesh(new THREE.ShapeGeometry(outer, 24), mat(1));
  const fill = new THREE.Mesh(new THREE.ShapeGeometry(outlinePath(sh, 0), 24), mat(0.16));
  fill.userData.fill = true;
  g.add(frame, fill);
  return g;
}
// basis: right, up and out (the normal) of a surface facing out.
function basis(out) {
  const hint = Math.abs(out.y) > 0.9 ? new THREE.Vector3(0, 0, -1) : new THREE.Vector3(0, 1, 0);
  const right = hint.clone().cross(out).normalize();
  const up = out.clone().cross(right).normalize();
  return { right, up };
}
function addMarkers(robot, screen) {
  const rank = { exact: 0, joint: 1, near: 2, inside: 3 };
  const groups = [];
  for (const p of places) {
    const at = new THREE.Vector3().fromArray(p.where.offset);
    let g = groups.find((g) => g.where.part === p.where.part && (g.where.area || '') === (p.where.area || '') &&
      g.items.some((q) => at.distanceTo(new THREE.Vector3().fromArray(q.where.offset)) < near));
    if (!g) groups.push(g = { where: p.where, items: [] });
    g.items.push(p);
    if (rank[p.where.how] < rank[g.where.how]) g.where = p.where;
  }
  robot.updateMatrixWorld(true);
  for (const { where, items } of groups) {
    const node = robot.getObjectByName(where.area === 'screen' ? 'screen' : where.part);
    if (!node) { console.warn('no part', where.part); continue; }
    const kind = items.every((p) => p.kind === items[0].kind) ? items[0].kind : 'sensor';
    const shades = kind === 'actuator' ? actuatorShades : kind === 'other' ? otherShades : sensorShades;
    const n = markers.filter((m) => m.shades === shades).length;
    const hex = shades[n % shades.length];
    const colour = new THREE.Color(hex);
    const out = outOf(where);
    let obj, at;
    if (where.area === 'screen' && screen) { // a frame round the screen
      const [cx, cy, cz] = screen.centre, w = screen.width / 2, h = screen.height / 2;
      const shape = new THREE.Shape().moveTo(-w - 1.6, -h - 1.6).lineTo(w + 1.6, -h - 1.6).lineTo(w + 1.6, h + 1.6).lineTo(-w - 1.6, h + 1.6);
      shape.holes.push(new THREE.Path().moveTo(-w - 0.6, -h - 0.6).lineTo(-w - 0.6, h + 0.6).lineTo(w + 0.6, h + 0.6).lineTo(w + 0.6, -h - 0.6));
      obj = new THREE.Mesh(new THREE.ShapeGeometry(shape), new THREE.MeshBasicMaterial({ color: colour, transparent: true }));
      obj.position.set(cx, cy, cz + 0.2);
      at = new THREE.Vector3(cx + w + 1.6, cy + h + 1.6, cz + 0.2);
    } else if (where.shape) { // its own shape on the surface
      const sh = where.shape;
      const centre = sh.free ? new THREE.Vector3().fromArray(where.offset) : onSurface(robot, node, where.offset, out);
      centre.addScaledVector(out, 0.12);
      const { right, up } = basis(out);
      obj = outline(sh, colour);
      obj.matrix.makeBasis(right, up, out).setPosition(centre);
      obj.matrixAutoUpdate = false;
      const hw = (sh.kind === 'circle' ? sh.r : sh.w / 2) + frameT, hh = (sh.kind === 'circle' ? sh.r : sh.h / 2) + frameT;
      at = centre.clone().addScaledVector(right, hw * 0.75).addScaledVector(up, hh * 0.75);
    } else {
      obj = new THREE.Mesh(new THREE.SphereGeometry(1.4, 16, 12),
        new THREE.MeshBasicMaterial({ color: colour, depthTest: false, transparent: true }));
      at = onSurface(robot, node, where.offset, out);
      obj.position.copy(at);
    }
    obj.renderOrder = 10;
    obj.traverse((o) => { o.raycast = () => {}; }); // not in the way of the next markers' rays
    node.add(obj);
    const box = document.createElement('div'); // placed by CSS2DRenderer
    const el = document.createElement('div'); // moved down by spread() when labels overlap
    el.className = 'label';
    box.append(el);
    const label = new CSS2DObject(box);
    label.center.set(0, 0); // placed by spread(), round its shape
    label.position.copy(obj.matrixAutoUpdate ? obj.position : new THREE.Vector3().setFromMatrixPosition(obj.matrix)); // the shape's centre
    node.add(label);
    el.style.setProperty('--c', hex);
    const m = { ids: items.map((p) => p.id), items, obj, label, el, colour, hex, shades, out, at: new THREE.Vector3().fromArray(where.offset), shown: [], see: where.see };
    if (items.some((p) => p.where.how === 'inside')) { // seen through its part when picked: a chip inside
      const part = robot.getObjectByName(where.part);
      const sh = where.shape || { kind: 'rect', w: 5, h: 5 };
      const cw = sh.kind === 'circle' ? sh.r * 2 : sh.w, chh = sh.kind === 'circle' ? sh.r * 2 : sh.h;
      const chip = new THREE.Mesh(new THREE.BoxGeometry(cw * 0.7, chh * 0.7, 1.2),
        new THREE.MeshBasicMaterial({ color: colour, depthTest: false, transparent: true, opacity: 0.9 }));
      const { right, up } = basis(out);
      const at = onSurface(robot, part, where.offset, out).addScaledVector(out, -4); // 4 mm in, under its outline
      chip.matrix.makeBasis(right, up, out).setPosition(at);
      chip.matrixAutoUpdate = false;
      m.inside = { part, dot: chip };
      m.inside.dot.renderOrder = 11;
      m.inside.dot.visible = false;
      m.inside.dot.raycast = () => {};
      part.add(m.inside.dot);
    }
    markers.push(m);
    el.addEventListener('pointerdown', (e) => e.stopPropagation()); // a tap, not a drag
    el.addEventListener('click', (e) => { e.stopPropagation(); if (m.shown.length) focus(m.shown[0], true); });
  }
}

// uses: the picked app's uses; a use of a whole component is one row for the component, shown on
// all of its features' places.
const wholes = new Map(); // component id: its row
function whole(c) {
  if (!wholes.has(c.id)) {
    const fs = c.features.map((f) => byId.get(f.id));
    wholes.set(c.id, { id: c.id, name: c.name, kind: c.kind, chip: c.chip, comp: c, whole: true,
      places: [...new Set(fs.flatMap((f) => f.places))], where: fs.map((f) => f.where).find(Boolean) });
  }
  return wholes.get(c.id);
}
function uses() {
  const out = [];
  for (const u of picked.uses) {
    const c = components.find((c) => c.id === u.feature);
    const f = c ? whole(c) : byId.get(u.feature);
    if (f) out.push({ f, does: u.does, sources: u.sources });
  }
  return out;
}
// listed: the rows the panel lists (the app's, or all features); shownOn: the checked ones on a
// marker, in the components' order.
const listed = () => (picked ? uses().map((u) => u.f) : features);
const order = (f) => components.indexOf(f.comp) * 100 + (f.whole ? 0 : 1 + f.comp.features.findIndex((x) => x.id === f.id));
function shownOn(m) {
  return listed().filter((f) => selected.has(f.id) && f.places.some((id) => m.ids.includes(id))).sort((a, b) => order(a) - order(b));
}
// updateMarkers sets each marker's label and shows the selected ones (on selection changes).
function updateMarkers() {
  for (const m of markers) {
    m.shown = shownOn(m);
    const on = m.shown.length > 0;
    m.on = on; // checked; frameMarkers shows it only while its side faces the camera
    m.obj.visible = on;
    m.label.visible = on;
    if (!on && focused === m) focused = null;
    m.el.textContent = [...new Set(m.shown.map((p) => p.name))].join(', '); // features at one place: their names
    m.el.classList.toggle('used', !!picked);
    m.el.classList.toggle('focus', focused === m);
  }
  for (const li of panel.querySelectorAll('li.component')) { // a component's box: all, some or none of its features
    const fs = components.find((c) => c.id === li.dataset.component).features;
    const k = fs.filter((f) => selected.has(f.id)).length, cb = li.querySelector('.comp-head input');
    cb.checked = k === fs.length; cb.indeterminate = k > 0 && k < fs.length;
  }
  const ids = listed().map((p) => p.id);
  const n = ids.filter((id) => selected.has(id)).length;
  markersBox.checked = n === ids.length;
  markersBox.indeterminate = n > 0 && n < ids.length;
}
// The screen shows the picked app's picture (its screen in data), else the launcher.
let screenMesh = null, screenNow = '';
const screenLoader = new THREE.TextureLoader();
function setScreen(image) {
  if (!screenMesh || image === screenNow) return;
  screenNow = image;
  screenLoader.load('screens/' + image, (t) => {
    if (screenNow !== image) return;
    t.colorSpace = THREE.SRGBColorSpace; t.flipY = false;
    screenMesh.material.map = t; screenMesh.material.needsUpdate = true;
  });
}

// See-through: while a part inside another (how inside: the IMU in the CoreS3) is picked, that
// part (and the screen on it) turns translucent and a chip shows the inside part, under its outline.
let xrayOf = null;
const xrayed = []; // [material, transparent, opacity, depthWrite]
function xray(m) {
  if (m === xrayOf) return;
  for (const [mat, t, o, dw] of xrayed.splice(0)) { mat.transparent = t; mat.opacity = o; mat.depthWrite = dw; mat.needsUpdate = true; }
  if (xrayOf && xrayOf.inside) xrayOf.inside.dot.visible = false;
  xrayOf = m;
  if (!m || !(m.inside || m.see)) return;
  // the parts its place names to see through (the LED chips inside the body), or the part it is in
  const meshes = m.see ? m.see.map((n) => robotRoot.getObjectByName(n)) : [m.inside.part];
  if (m.inside && m.inside.part.name === 'core') meshes.push(m.inside.part.parent.getObjectByName('screen'));
  for (const mesh of meshes.filter(Boolean)) {
    const mat = mesh.material;
    xrayed.push([mat, mat.transparent, mat.opacity, mat.depthWrite]);
    mat.transparent = true; mat.opacity = 0.25; mat.depthWrite = false; mat.needsUpdate = true;
  }
  if (m.inside) m.inside.dot.visible = true;
}

// Each frame: only the markers on the sides facing the camera show (their out, turned with the
// robot, toward the camera); the focused one pulses.
const world = new THREE.Vector3(), toCam = new THREE.Vector3(), out = new THREE.Vector3(), yAxis = new THREE.Vector3(0, 1, 0);
function frameMarkers(now) {
  xray(focused);
  for (const m of markers) {
    if (!m.on) continue;
    const f = focused === m;
    m.obj.getWorldPosition(world);
    out.copy(m.out).applyAxisAngle(yAxis, turntable.rotation.y);
    toCam.copy(camera.position).sub(world).normalize();
    const facing = out.dot(toCam) > 0.2;
    m.obj.visible = m.label.visible = facing;
    if (!facing) continue;
    const behind = false;
    const pulse = 0.5 + 0.5 * Math.sin(now / 180);
    const a = behind ? 0.25 : f ? 0.7 + 0.3 * pulse : 1;
    if (m.obj.isGroup) m.obj.children.forEach((c) => { c.material.opacity = c.userData.fill ? (f ? 0.18 + 0.22 * pulse : 0.16) * (behind ? 0.4 : 1) : a; });
    else {
      if (m.obj.geometry.type === 'SphereGeometry') m.obj.scale.setScalar(f ? 1.6 + 0.6 * pulse : picked ? 1.3 : 1);
      m.obj.material.opacity = a;
    }
    m.el.classList.toggle('behind', behind);
  }
}

// Labels sit next to their shape, not over it or any other: each shape's box on the screen is
// found, then each label (top to bottom) takes the first place round its own shape (right, left,
// above, below, then the corners) clear of every shape and of the labels placed before it; with
// none clear, the right, moved down until clear of the labels.
const corner = new THREE.Vector3(), box3 = new THREE.Box3(), screenPos = new THREE.Vector3();
function screenBox(obj, w, h) {
  box3.setFromObject(obj);
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (let k = 0; k < 8; k++) {
    corner.set(k & 1 ? box3.max.x : box3.min.x, k & 2 ? box3.max.y : box3.min.y, k & 4 ? box3.max.z : box3.min.z).project(camera);
    const x = (corner.x + 1) / 2 * w, y = (1 - corner.y) / 2 * h;
    x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
  }
  return { x0, y0, x1, y1 };
}
const hit = (a, b, gap) => a.x0 < b.x1 + gap && b.x0 < a.x1 + gap && a.y0 < b.y1 + gap && b.y0 < a.y1 + gap;
function spread() {
  const w = stage.clientWidth, h = stage.clientHeight, gap = 3;
  const shown = markers.filter((m) => m.label.visible).map((m) => {
    m.label.getWorldPosition(screenPos).project(camera);
    return { m, ax: (screenPos.x + 1) / 2 * w, ay: (1 - screenPos.y) / 2 * h, wd: m.el.offsetWidth, ht: m.el.offsetHeight, box: screenBox(m.obj, w, h) };
  }).sort((a, b) => a.box.y0 - b.box.y0);
  const sr = stage.getBoundingClientRect(); // the stage's buttons: no label over them
  const buttons = ['turn-left', 'turn-right', 'spin'].map((id) => document.getElementById(id).getBoundingClientRect())
    .map((r) => ({ x0: r.left - sr.left, y0: r.top - sr.top, x1: r.right - sr.left, y1: r.bottom - sr.top }));
  const shapes = shown.map((l) => l.box), placed = [...buttons];
  const inside = (r) => r.x0 >= 4 && r.y0 >= 4 && r.x1 <= w - 4 && r.y1 <= h - 4;
  for (const l of shown) {
    const b = l.box, cx = (b.x0 + b.x1) / 2, cy = (b.y0 + b.y1) / 2, d = 6;
    const spots = [
      [b.x1 + d, cy - l.ht / 2], [b.x0 - d - l.wd, cy - l.ht / 2], [cx - l.wd / 2, b.y0 - d - l.ht], [cx - l.wd / 2, b.y1 + d],
      [b.x1 + d, b.y0 - l.ht], [b.x1 + d, b.y1], [b.x0 - d - l.wd, b.y0 - l.ht], [b.x0 - d - l.wd, b.y1],
    ];
    let r = null;
    for (const [x, y] of spots) {
      const c = { x0: x, y0: y, x1: x + l.wd, y1: y + l.ht };
      if (inside(c) && !shapes.some((s) => hit(c, s, 1)) && !placed.some((p) => hit(c, p, gap))) { r = c; break; }
    }
    if (!r) { // the right, clamped into the view, moved down until clear of the labels
      let x = Math.min(Math.max(4, b.x1 + d), w - 4 - l.wd), y = Math.max(4, cy - l.ht / 2);
      r = { x0: x, y0: y, x1: x + l.wd, y1: y + l.ht };
      for (let moved = true; moved;) {
        moved = false;
        for (const p of placed) if (hit(r, p, gap)) { r = { x0: r.x0, y0: p.y1 + gap, x1: r.x1, y1: p.y1 + gap + l.ht }; moved = true; }
      }
    }
    l.m.el.style.transform = `translate(${Math.round(r.x0 - l.ax)}px, ${Math.round(r.y0 - l.ay)}px)`;
    placed.push(r);
  }
  stage.shapeBoxes = shown.map((l) => ({ text: l.m.el.textContent, ...l.box })); // for pagecheck: labels off the shapes
}

// focus: a part picked in the list (or on its label): checked, its marker turned to the front,
// pulsing, its servo moving; fromStage: its row scrolled into view.
function focus(p, fromStage = false) {
  if (!selected.has(p.id)) { selected.add(p.id); const cb = panel.querySelector(`input[data-id="${p.id}"]`); if (cb) cb.checked = true; }
  const target = p.places[0];
  focused = markers.find((m) => m.ids.includes(target)) || null;
  updateMarkers();
  panel.querySelectorAll('li.active').forEach((x) => x.classList.remove('active'));
  const li = panel.querySelector(`li[data-id="${p.id}"]`);
  if (li) { li.classList.add('active'); if (fromStage) li.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); }
  showMove(p.id);
  if (focused) {
    faceTo(focused.out, focused.at);
    focused.el.classList.remove('flash'); void focused.el.offsetWidth; focused.el.classList.add('flash');
  }
}

// The panel: the apps (each a link, #id), then all parts, or the picked app and its uses; each
// entry with a checkbox (show it on the robot), All checks or clears the list.
const sourceLink = (s) => {
  const a = document.createElement('a');
  a.href = s.url || `https://github.com/i41-eu/${s.repo}/blob/${s.ref}/${s.path}#L${s.line}`;
  a.textContent = s.url ? `M5Stack's docs: "${s.quote}"` : `${s.repo}/${s.path}:${s.line}`;
  a.target = '_blank'; a.rel = 'noopener';
  return a;
};
// sources: folded under a toggle (the file paths are for those who check them).
function sources(list) {
  const d = document.createElement('details');
  d.className = 'src';
  const sum = document.createElement('summary');
  sum.textContent = list.length === 1 ? 'Source' : `Sources (${list.length})`;
  const div = document.createElement('div');
  list.forEach((s, i) => { if (i) div.append(', '); div.append(sourceLink(s)); });
  d.append(sum, div);
  return d;
}
const link = (href, text, external = true) => {
  const a = document.createElement('a');
  a.href = href; a.textContent = text;
  if (external) { a.target = '_blank'; a.rel = 'noopener'; }
  return a;
};
function item(p, does, srcs, nested = false) { // nested: under its component's row
  const li = document.createElement('li');
  li.dataset.id = p.id;
  const mk = markers.find((m) => m.ids.includes(p.places[0]));
  if (mk) { li.style.setProperty('--c', mk.hex); li.classList.add('placed'); } // its marker's colour
  const cb = document.createElement('input');
  cb.type = 'checkbox'; cb.dataset.id = p.id; cb.checked = selected.has(p.id);
  cb.title = 'Show on the robot';
  cb.setAttribute('aria-label', 'Show ' + p.name + ' on the robot');
  cb.addEventListener('click', (e) => e.stopPropagation());
  const drop = () => { selected.delete(p.id); cb.checked = false; li.classList.remove('active'); if (focused && focused.ids.includes(p.id)) focused = null; updateMarkers(); };
  cb.addEventListener('change', () => { if (cb.checked) focus(p); else drop(); }); // as a click on the row
  const body = document.createElement('div');
  const head = document.createElement('div');
  const name = document.createElement('span');
  name.className = 'name'; name.textContent = p.name;
  const kind = document.createElement('span');
  kind.className = 'kind ' + p.kind; kind.textContent = p.kind;
  head.append(name, kind);
  const d = document.createElement('div');
  d.className = 'does'; d.textContent = does;
  body.append(head, d);
  const meta = document.createElement('div');
  meta.className = 'meta';
  const of = !p.whole && (p.comp.features.length > 1 || p.comp.name !== p.name) ? p.comp.name : '';
  const on = p.whole ? 'all of it: ' + p.comp.features.map((f) => f.name).join(', ') : p.on ? 'through its ' + p.on.map((w) => p.comp.windows.find((x) => x.id === w).name).join(' and ') : '';
  const where = p.where ? p.where.note : 'where: not in the docs';
  meta.textContent = (picked ? [of, on, p.where ? '' : where] : nested ? [on, where] : [of, p.chip, on, where]).filter(Boolean).join(' · ');
  if (meta.textContent) body.append(meta);
  if (p.excludes) { // what cannot run at the same time
    const x = document.createElement('div'); x.className = 'meta excludes';
    x.append('Not at the same time as ' + p.excludes.map((e) => byId.get(e.feature).name).join(', ') + ' · ');
    p.excludes.forEach((e, i) => { if (i) x.append(', '); x.append(sourceLink(e.source)); });
    body.append(x);
  }
  body.append(sources(p.where && !picked ? [...srcs, p.where.source] : srcs)); // where it is, too
  if (p.places.length) { // one column (a phone): the model is far above, a link back up to it
    const up = document.createElement('a');
    up.className = 'tomodel'; up.href = '#stage'; up.textContent = '↑ Show on the model';
    up.addEventListener('click', (e) => {
      e.preventDefault(); e.stopPropagation();
      focus(p);
      stage.scrollIntoView({ behavior: 'instant', block: 'start' }); // instant: a smooth scroll can stop short while the robot renders (a slow phone)
    });
    body.append(up);
  }
  li.append(cb, body);
  li.addEventListener('click', (e) => { // a click picks it (checked); a second click on it unchecks it
    if (e.target.closest('a, summary, .src')) return; // a link or the sources' toggle, not a pick
    if (li.classList.contains('active')) drop();
    else focus(p);
  });
  return li;
}
// componentRow: a component of several features (or named apart from its one): its name and
// chip, a checkbox for all of its features, and the features nested under it.
function componentRow(c) {
  const li = document.createElement('li');
  li.className = 'component'; li.dataset.component = c.id;
  const fs = c.features.map((f) => byId.get(f.id));
  const cb = document.createElement('input');
  cb.type = 'checkbox'; cb.title = 'Show all of its features on the robot';
  cb.setAttribute('aria-label', 'Show ' + c.name + ' on the robot');
  cb.addEventListener('change', () => {
    for (const f of fs) if (cb.checked) selected.add(f.id); else selected.delete(f.id);
    li.querySelectorAll('input[data-id]').forEach((x) => { x.checked = selected.has(x.dataset.id); });
    updateMarkers();
    if (cb.checked) focus(fs[0]);
  });
  const head = document.createElement('div'); head.className = 'comp-head';
  const name = document.createElement('span'); name.className = 'name'; name.textContent = c.name;
  const chip = document.createElement('span'); chip.className = 'chip'; chip.textContent = c.chip;
  head.append(cb, name, chip);
  const ul = document.createElement('ul'); ul.className = 'items features';
  for (const f of fs) ul.append(item(f, f.does, f.sources, true));
  li.append(head, ul);
  return li;
}
function appLinks(a) {
  const div = document.createElement('div');
  div.className = 'links';
  div.append(link(a.page, new URL(a.page).host + new URL(a.page).pathname + ' ↗')); // its page on s.i41.eu
  if (a.web) div.append(link(a.web, new URL(a.web).host + ' ↗'));
  div.append(link(`https://github.com/i41-eu/${a.repo}`, a.repo + ' ↗'));
  return div;
}
function render() {
  panel.replaceChildren();
  selected.clear();
  for (const p of listed()) selected.add(p.id);
  focused = null;
  if (picked) {
    const h = document.createElement('h2'); h.textContent = picked.name;
    const about = document.createElement('p'); about.className = 'about'; about.textContent = picked.about;
    panel.append(h, about, appLinks(picked), sources(picked.sources));
    if (picked.leds) {
      const l = document.createElement('p'); l.className = 'about';
      const sw = document.createElement('span'); sw.className = 'swatch'; sw.style.background = picked.leds.color;
      l.append(sw, 'LEDs: ' + picked.leds.does);
      panel.append(l, sources(picked.leds.sources));
    }
    const ul = document.createElement('ul'); ul.className = 'items';
    for (const u of uses()) ul.append(item(u.f, u.does, u.sources));
    panel.append(ul);
  } else {
    const h = document.createElement('h2'); h.textContent = 'Apps';
    const ul = document.createElement('ul'); ul.className = 'apps';
    for (const a of apps) {
      const li = document.createElement('li');
      li.append(link('#' + a.id, a.name, false), ' — ' + a.about + ' ', appLinks(a));
      ul.append(li);
    }
    panel.append(h, ul);
    for (const kind of ['sensor', 'actuator', 'other']) {
      const h = document.createElement('h2'); h.textContent = { sensor: 'Sensors', actuator: 'Actuators', other: 'Other parts (ports, slots, buttons)' }[kind];
      const ul = document.createElement('ul'); ul.className = 'items';
      for (const c of components.filter((c) => c.kind === kind)) {
        if (c.features.length === 1 && c.features[0].name === c.name) { const f = byId.get(c.features[0].id); ul.append(item(f, f.does, f.sources)); continue; }
        ul.append(componentRow(c));
      }
      panel.append(h, ul);
    }
  }
  const unplaced = listed().filter((p) => !p.places.length);
  if (unplaced.length) {
    const p = document.createElement('p'); p.className = 'unplaced';
    p.textContent = 'Not marked on the model (the docs do not say where): ' + unplaced.map((p) => p.name).join(', ') + '.';
    panel.append(p);
  }
  setLEDs(picked ? picked.leds : null);
  setScreen(picked && picked.screen ? picked.screen.image : 'launcher.png');
  updateMarkers();
}

// All: checks or clears every entry of the list.
markersBox.addEventListener('change', () => {
  for (const p of listed()) if (markersBox.checked) selected.add(p.id); else selected.delete(p.id);
  panel.querySelectorAll('input[data-id]').forEach((cb) => { cb.checked = selected.has(cb.dataset.id); });
  updateMarkers();
});
// The app: from the address (#id), so each app has its own link; the picker changes it.
function pickFromHash() {
  const a = apps.find((a) => '#' + a.id === location.hash) || null;
  if (a === picked && panel.childElementCount) return;
  picked = a;
  appSelect.value = a ? a.id : '';
  render();
}
appSelect.addEventListener('change', () => {
  if (appSelect.value) location.hash = appSelect.value;
  else { history.pushState(null, '', location.pathname + location.search); pickFromHash(); }
});
window.addEventListener('hashchange', pickFromHash);

let last = performance.now();
function frame(now) {
  const dt = Math.min(0.1, (now - last) / 1000);
  last = now;
  turning(now);
  pose(now);
  frameMarkers(now);
  renderer.render(scene, camera);
  labels.render(scene, camera);
  spread();
  requestAnimationFrame(frame);
}

async function main() {
  resize();
  requestAnimationFrame(frame);
  const [gltf, compsDoc, appsDoc] = await Promise.all([
    new GLTFLoader().loadAsync('robot.glb'),
    fetch('data/components.json').then((r) => r.json()),
    fetch('data/apps.json').then((r) => r.json()),
  ]);
  fromComponents(compsDoc);
  apps = appsDoc.apps;
  for (const a of apps) appSelect.add(new Option(a.name, a.id));
  const robot = gltf.scene;
  robotRoot = robot;
  robot.traverse((o) => {
    if (o.isMesh && o.name === 'screen') { o.material.toneMapped = false; screenMesh = o; } // the picture as it is
    else if (o.isMesh && o.name === 'base-cover') { // the photo of the bottom carries its own light: shown about as photographed
      o.material.emissive.set(0xffffff);
      o.material.emissiveMap = o.material.map;
      o.material.emissiveIntensity = 1.6;
    }
    else if (o.isMesh && !o.name.startsWith('led-bar')) { // light bounced inside holes and recesses: a little of each part's own colour
      o.material.emissive.set(0x3a3a3a);
      if (o.material.map) o.material.emissiveMap = o.material.map;
    }
  });
  lightLEDs(robot);
  addMarkers(robot, (gltf.parser.json.extras || {}).screen); // at rest, before it turns
  turntable.add(robot);
  yawNode = robot.getObjectByName('yaw');
  headNode = robot.getObjectByName('head');
  measure(robot);
  pickFromHash();
  // ?pick=<feature> or <component>: only that is shown (the rest of the list unchecked, there to
  // check), the first picked at once. A component: its rows in the app's list (its own row, or each
  // of its features the app uses: both LED strips), else all of its features.
  const id = new URLSearchParams(location.search).get('pick');
  const comp = components.find((c) => c.id === id);
  const rows = listed().filter((f) => f.id === id || (comp && f.comp === comp));
  const picks = rows.length ? rows : comp ? comp.features.map((f) => byId.get(f.id)) : [byId.get(id)].filter(Boolean);
  if (picks.length) {
    selected.clear();
    for (const f of picks) selected.add(f.id);
    panel.querySelectorAll('input[data-id]').forEach((cb) => { cb.checked = selected.has(cb.dataset.id); });
    updateMarkers();
    focus(picks[0]);
  }
  status.remove();
}
main().catch((e) => { status.textContent = 'Could not load the robot: ' + e.message; console.error(e); });
