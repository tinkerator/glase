// Program glase demonstrates some of the functionality of the glase
// package.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"zappem.net/pub/graphics/hershey"
	"zappem.net/pub/graphics/polymark"
	"zappem.net/pub/io/glase"
	"zappem.net/pub/math/polygon"
)

var (
	info      = flag.Bool("info", false, "list the discovered laser devices")
	serial    = flag.String("serial", "", "serial numbered device to connect to")
	cor       = flag.String("cor", "./local.fixup.cor", "correction data file (ex. jcz150.cor)")
	scale     = flag.Float64("mm2gal", 0.0, "non-zero overrides number of galvo units to mm")
	gotoX     = flag.Float64("x", 0.0, "x coordinate at end of run")
	gotoY     = flag.Float64("y", 0.0, "y coordinate at end of run")
	radius    = flag.Float64("radius", 5.0, "mm radius of --poly")
	circle    = flag.Bool("circle", false, "step out a circle with the laser")
	burn      = flag.Bool("burn", false, "burn the specified --poly")
	poly      = flag.Int("poly", 0, "draw a 2*n sided star (n>=3) around (--x,--y)")
	decode    = flag.String("decode", "", "decode the hex dump of a command list instruction")
	dis       = flag.String("dis", "", "disassemble a stream file containing command list instructions")
	sim       = flag.Bool("sim", false, "simulate the connection if no device is found")
	bSpeed    = flag.Float64("burn-speed", 200, "burn speed of movement mm/sec")
	grid      = flag.Int("grid", 0, "cm grid edge size, centered at (--x,--y)")
	calibrate = flag.Bool("calibrate", false, "renders a 65x65 pt grid of 1000 galvo unit pitch")
	banner    = flag.String("banner", "", "text to render, centered at (--x,--y) with --font and --size")
	font      = flag.String("font", "rowmand", "hershey font to use for --banner")
	size      = flag.Float64("size", 15.0, "mm size of --banner font to use")
	scribe    = flag.Float64("scribe", 0.02, "mm width of raw laser mark")
	preview   = flag.Duration("preview", 10*time.Second, "time to preview marks")
	hatch     = flag.Float64("hatch", 0.0, "non-zero mm spacing for hatch marks (--burn fill for --banner text)")
	bb        = flag.Bool("bb", false, "just display the bounding box instead of --banner text")
	laser     = flag.Bool("laser", false, "burn a laser icon of --size at (--x,--y)")
	regen     = flag.Bool("regen", false, "write new correction file, named by extending --cor name")
)

// polystoList renders polygon Shapes with a list of laser commands.
func polysToList(list *glase.List, polys *polygon.Shapes) *glase.List {
	ll, tr := polys.BB()
	if ll.X < -80 || ll.Y < -80 || tr.X > 80 || tr.Y > 80 {
		log.Fatalf("Polygons too large to render. Bounding-box: %.2f <-> %.2f", ll, tr)
	} else if ll.X < -75 || ll.Y < -75 || tr.X > 75 || tr.Y > 75 {
		log.Printf("WARNING: coordinates unstable at edges of polygons. Bounding-box: %.2f <-> %.2f", ll, tr)
	}
	polys.Union()
	var err error
	if *burn {
		p := glase.BasicProfile
		p.MarkSpeed = *bSpeed
		list, err = list.Start(p)
	} else {
		list, err = list.Start(glase.PointerProfile)
		if err == nil && *bb {
			repeatFrom := list.Offset()
			list = list.JumpXY(ll.X, ll.Y).JumpXY(ll.X, tr.Y).JumpXY(tr.X, tr.Y).JumpXY(tr.X, ll.Y)
			n, err := list.ReplayFrom(repeatFrom, 0)
			if err != nil {
				log.Fatalf("Failed to repeat from %d: %v", repeatFrom, err)
			}
			log.Printf("Appended %d repetitions", n)
			return list
		}
	}
	if err != nil {
		log.Fatalf("Encountered error: %v", err)
	}
	if *hatch != 0 && *burn {
		var holes []int
		var shapes []int
		for i, p := range polys.P {
			if p.Hole {
				holes = append(holes, i)
			} else {
				shapes = append(shapes, i)
			}
		}
		for _, i := range shapes {
			lines, err := polys.Slice(i, *hatch, holes...)
			if err != nil {
				log.Fatalf("Failed to x-hatch laser icon %d: %v", i, err)
			}
			for _, line := range lines {
				list = list.JumpXY(line.From.X, line.From.Y).MarkXY(line.To.X, line.To.Y).Sleep(30 * time.Microsecond)
			}
			lines, err = polys.VSlice(i, *hatch, holes...)
			if err != nil {
				log.Fatalf("Failed to y-hatch laser icon %d: %v", i, err)
			}
			for _, line := range lines {
				list = list.JumpXY(line.From.X, line.From.Y).MarkXY(line.To.X, line.To.Y).Sleep(30 * time.Microsecond)
			}
		}
	}
	for _, p := range polys.P {
		for i := 0; i <= len(p.PS); i++ {
			var pt polygon.Point
			if i < len(p.PS) {
				pt = p.PS[i]
			} else {
				pt = p.PS[0]
			}
			if i == 0 || !*burn {
				list = list.JumpXY(pt.X, pt.Y)
			} else {
				list = list.MarkXY(pt.X, pt.Y)
			}
		}
		if *burn {
			list = list.Sleep(30 * time.Microsecond)
		}
	}
	return list
}

// renderLaser burns a laser warning sign (just the rays in a triangle).
func renderLaser(list *glase.List, size float64) *glase.List {
	pen := &polymark.Pen{
		Scribe:  *scribe,
		Reflect: true,
	}
	// Triangle
	dX := size / 2
	rt3 := math.Sqrt(3.0)
	dY := rt3 * dX
	dYlow := dX / rt3
	border := size / 40
	cX := *gotoX
	cY := *gotoY
	ang := math.Pi / 12
	polys := pen.Line(nil, []polygon.Point{
		{cX - dX, cY - dYlow},
		{cX + dX, cY - dYlow},
		{cX, cY + dY - dYlow},
		{cX - dX, cY - dYlow},
	}, 2*border, true, true)
	list = polysToList(list, polys)

	// circle
	polys = pen.Circle(nil, polygon.Point{cX, cY}, 3*border)
	sR := 6 * border
	lR := 8 * border
	for i := 0; i < 24; i++ {
		// larger rays
		r := lR
		switch i % 2 {
		case 0:
			if i == 0 {
				// rightward ray
				r = dX*2/3 - border
			}
		case 1:
			// smaller rays
			r = sR
		}
		alpha := float64(i) * ang
		rX, rY := r*math.Cos(alpha), r*math.Sin(alpha)
		polys = pen.Line(polys, []polygon.Point{
			{cX, cY},
			{cX + rX, cY + rY},
		}, border*.5, false, false)
	}
	return polysToList(list, polys)
}

// Render text at desired coordinates of desired font and scale.
func renderBanner(list *glase.List, banner string) *glase.List {
	fnt, err := hershey.New(*font)
	if err != nil {
		log.Fatalf("Failed to load font %q: %v", *font, err)
	}
	pen := &polymark.Pen{
		Scribe:  *scribe,
		Reflect: true,
	}
	text := pen.Text(nil, *gotoX, *gotoY, *size, polymark.AlignCenter|polymark.AlignCenter, fnt, banner)
	return polysToList(list, text)
}

func main() {
	flag.Parse()

	var conn *glase.Conn
	var err error

	// Write this at the end so we can get a sense of how long
	// things took.
	defer log.Print("Done.")

	ctx := context.Background()

	if *sim {
		conn = glase.OpenSim()
	} else {
		conn, err = glase.OpenOmni1()
		if err != nil {
			log.Fatalf("Failed to detect Omni 1 Laser: %v", err)
		} else {
			defer conn.Close()
		}
	}
	if *scale != 0 {
		conn.SetMM2Galvo(*scale)
	}

	if !*sim {
		if *info {
			list, err := conn.ListDevices()
			if err != nil {
				log.Fatalf("Unable to list devices: %v", err)
			}
			for _, s := range list {
				log.Print(s)
			}
			return
		}
		if *serial != "" {
			if err := conn.DeviceBySerial(*serial); err != nil {
				log.Fatalf("No device found with serial number, %q: %v", *serial, err)
			}
		} else {
			if err := conn.DeviceByIndex(0); err != nil {
				log.Fatalf("No 0-device found: %v", err)
			}
		}
		log.Printf("Connected to: %v", conn.String())
		log.Printf("Serial number: %q", conn.GetSerial())
		log.Printf("Version: %q", conn.GetVersion())
	}

	if *decode != "" {
		a, err := conn.ParseListCommand(*decode)
		if err != nil {
			log.Fatalf("Unable to parse %q", *decode)
		}
		s := conn.Disassemble(a)
		log.Printf("%q => %s", *decode, s)
		return
	}

	if *dis != "" {
		d, err := os.ReadFile(*dis)
		if err != nil {
			log.Fatalf("Failed to read %q: %v", *dis, err)
		}
		for i, line := range strings.Split(string(d), "\n") {
			if line == "" {
				continue
			}
			a, err := conn.ParseListCommand(line)
			if err != nil {
				log.Fatalf("Unable to parse (line %d): %q", i+1, line)
			}
			s := conn.Disassemble(a)
			fmt.Printf("%5d: %q => %s\n", i, line, s)
		}
		return
	}
	list := conn.NewList()
	if *calibrate {
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed * 2
			list, err = list.Start(p)
		} else {
			list, err = list.Start(glase.PointerProfile)
		}

		// To operate in galvo units, we convert back to mm
		// values with the current normalization.
		mm2Galvo := conn.MM2Galvo()
		minD := -32.0 * 1000 / mm2Galvo
		maxD := -minD
		for e := -32; e <= 32; e++ {
			d := float64(e*1000) / mm2Galvo
			if *burn {
				list = list.JumpXY(minD, d).MarkXY(maxD, d).Sleep(30 * time.Microsecond)
				list = list.JumpXY(d, minD).MarkXY(d, maxD).Sleep(30 * time.Microsecond)
			} else {
				list = list.JumpXY(minD, d).JumpXY(maxD, d)
				list = list.JumpXY(d, minD).JumpXY(d, maxD)
			}

		}
	} else if *grid != 0 {
		// Start mm grid
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed * 2
			list, err = list.Start(p)
		} else {
			list, err = list.Start(glase.PointerProfile)
		}

		// Mark out a mm grid with CM spaced lines at full --burn-speed
		// and mm innards at twice --burn-speed.
		size := 10 * *grid
		half := float64(5 * *grid)
		fromX := *gotoX - half
		fromY := *gotoY - half
		x0, x1 := fromX, fromX+float64(size)
		y0, y1 := fromY, fromY+float64(size)
		for i := 0; i <= size; i++ {
			x2 := x0 + float64(i)
			y2 := y0 + float64(i)
			if *burn {
				list = list.JumpXY(x0, y2).MarkXY(x1, y2).Sleep(30 * time.Microsecond)
				list = list.JumpXY(x2, y0).MarkXY(x2, y1).Sleep(30 * time.Microsecond)
			} else {
				list = list.JumpXY(x0, y2).JumpXY(x1, y2)
				list = list.JumpXY(x2, y0).JumpXY(x2, y1)
			}
		}

		// Next burn cm grid
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed
			list, err = list.Start(p)
		}

		for i := 0; i <= size; i += 10 {
			x2 := x0 + float64(i)
			y2 := y0 + float64(i)
			if *burn {
				list = list.JumpXY(x0, y2).MarkXY(x1, y2).Sleep(30 * time.Microsecond)
				list = list.JumpXY(x2, y0).MarkXY(x2, y1).Sleep(30 * time.Microsecond)
			} else {
				list = list.JumpXY(x0, y2).JumpXY(x1, y2)
				list = list.JumpXY(x2, y0).JumpXY(x2, y1)
			}
		}
	} else if *banner != "" {
		list = renderBanner(list, *banner)
	} else if *laser {
		list = renderLaser(list, *size)
	} else if *poly != 0 {
		if *poly < 3 {
			log.Fatalf("Need --poly value >= 3, got %d", *poly)
		}
		theta := math.Pi / float64(*poly)
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed
			list, err = list.Start(p)
		} else {
			list, err = list.Start(glase.PointerProfile)
		}
		repeatFrom := list.Offset()
		for i := 0; i <= *poly*2; i++ {
			at := *radius
			if i&1 == 0 {
				at *= 0.5
			}
			ang := theta * float64(i)
			if i == 0 || !*burn {
				list = list.JumpXY(*gotoX+at*math.Cos(ang), *gotoY+at*math.Sin(ang))
			} else {
				list = list.MarkXY(*gotoX+at*math.Cos(ang), *gotoY+at*math.Sin(ang))
			}
		}
		if *burn {
			list = list.Sleep(30 * time.Microsecond)
			list = list.DelayJumps(glase.BasicProfile.JumpDelay)
		} else {
			n, err := list.ReplayFrom(repeatFrom, 0)
			if err != nil {
				log.Fatalf("Failed to repeat from %d: %v", repeatFrom, err)
			}
			log.Printf("Appended %d repetitions", n)
		}
	}

	var data []byte
	if strings.HasSuffix(*cor, ".fixup") {
		log.Print("Loading data is ASCII fixup data, lines of: xi yi mmx mmy")
		data, err = conn.DeriveCorrections(*cor)
		if err != nil {
			log.Fatalf("Failed to generate correction data from %q: %v", *cor, err)
		}
		if *regen {
			dest := fmt.Sprint(*cor, ".cor")
			if err := os.WriteFile(dest, data, 0644); err != nil {
				log.Fatalf("Failed to write %q: %v", dest, err)
			}
			log.Printf("New correction data written to %q", dest)
			return
		}
	} else if *cor != "" {
		data, err = os.ReadFile(*cor)
		if err != nil {
			log.Fatalf("Failed to read %q: %v", *cor, err)
		}
		log.Printf("Correction data loaded from %q", *cor)
	} else {
		log.Print("WARNING: using all zeros correction file")
		data = make([]byte, 36+65*65*8+4)
	}

	if *sim {
		i := 0
		for a := range list.Stream() {
			s := conn.Disassemble(a)
			i++
			fmt.Printf("%5d: %q => %s\n", i, hex.EncodeToString([]byte(a[:])), s)
		}
		return
	}

	if err := conn.UploadCorrections(data); err != nil {
		log.Fatalf("Failed to upload corrections %q: %v", *cor, err)
	}

	if err := conn.EnableLaser(); err != nil {
		log.Fatalf("Failed to enable laser: %v", err)
	}
	if err := conn.SetControlMode(0); err != nil {
		log.Fatalf("Failed to set control mode: %v", err)
	}

	if list.Offset() != 0 {
		ctx2, cancel := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = list.Run(ctx2, !*burn)
		}()
		if !*burn {
			log.Printf("10 seconds of preview %v", ctx2)
			time.Sleep(*preview)
			cancel()
			log.Print("Canceled, wait to exit")
		}
		wg.Wait()
		if err != nil {
			log.Fatalf("Failure to run (--burn=%v): %v", *burn, err)
		}
		return
	}

	if *circle {
		const steps = 100
		const ang = 2 * math.Pi / steps
		const r = 40.0 // mm
		for i := 0; i < steps; i++ {
			a := ang * float64(i)
			x, y := r*math.Cos(a), r*math.Sin(a)
			conn.GotoXY(x, y)
			time.Sleep(200 * time.Millisecond)
		}
	}

	x, y, err := conn.GetXY()
	if err != nil {
		log.Fatalf("Failed to read XY: %v", err)
	}
	log.Printf("Current XY=(%f, %f)", x, y)
	conn.GotoXY(*gotoX, *gotoY)
	time.Sleep(500 * time.Millisecond)
	x, y, err = conn.GetXY()
	if err != nil {
		log.Fatalf("Failed to read XY: %v", err)
	}
	log.Printf("Final XY=(%f, %f)", x, y)
}
