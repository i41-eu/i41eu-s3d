// Command s3dgen writes the robot for the page: robot3d's parts at rest, each a named node under
// its joint, as a glTF binary (web/robot.glb).
//
// The nodes: robot (scale 0.001: glTF is in metres, robot3d in millimetres) → base (the plate)
// → yaw (turns about Y: the servo) → head (at robot3d.PitchPivot(), turns about X: the main
// body, the CoreS3, the LED bars, the screen). A part under head is translated by -pivot, so in
// its node every vertex is where robot3d has it at rest. The screen is a textured quad at
// robot3d.Screen() (its picture is drawn in robot3d's shader, not modelled).
//
//	go run ./cmd/s3dgen [-o web/robot.glb] [-screen ../i41eu-assets/screens/launcher.png]
package main

import (
	"flag"
	"log"
	"os"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

// defaultScreen is the screen's picture: the robot's launcher, Embody Mode (as on the owner's
// photos of the robot), from the assets cloned beside this repo.
const defaultScreen = "../i41eu-assets/screens/launcher.png"

func main() {
	out := flag.String("o", "web/robot.glb", "the glTF binary to write")
	screen := flag.String("screen", defaultScreen, "a PNG for the screen (320x240); empty: a dark screen")
	decalsFile := flag.String("decals", "data/decals.json", "pictures painted on the robot (data/decals.json); empty: none")
	flag.Parse()

	var png []byte
	if *screen != "" {
		b, err := os.ReadFile(*screen)
		if err != nil {
			log.Fatal(err)
		}
		png = b
	}
	centre, w, h := robot3d.Screen()
	surface := surfaceFunc(robot3d.Surface)
	if *decalsFile != "" {
		decals, err := loadDecals(*decalsFile)
		if err != nil {
			log.Fatal(err)
		}
		surface = withDecals(surface, decals)
	}
	glb, err := build(robot3d.Parts(), robot3d.PitchPivot(), centre, w, h, png, surface)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, glb, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: %d bytes", *out, len(glb))
}
