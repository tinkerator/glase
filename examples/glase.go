// Program glase demonstrates some of the functionality of the glase
// package.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"zappem.net/pub/graphics/aruco"
	"zappem.net/pub/graphics/hershey"
	"zappem.net/pub/graphics/polymark"
	"zappem.net/pub/io/glase"
	"zappem.net/pub/math/linear"
	"zappem.net/pub/math/polygon"
)

var (
	info      = flag.Bool("info", false, "list the discovered laser devices")
	serial    = flag.String("serial", "", "serial numbered device to connect to")
	cor       = flag.String("cor", "./local.fixup.cor", "correction data file (ex. jcz150.cor)")
	scale     = flag.Float64("mm2gal", 0.0, "non-zero overrides number of galvo units to mm")
	gotoX     = flag.Float64("x", 0.0, "x coordinate at end of run")
	gotoY     = flag.Float64("y", 0.0, "y coordinate at end of run")
	radius    = flag.Float64("radius", 5.0, "mm radius of --poly-star")
	circle    = flag.Bool("circle", false, "step out a circle with the laser")
	burn      = flag.Bool("burn", false, "burn the specified object")
	polystar  = flag.Int("poly-star", 0, "draw a 2*n sided star (n>=3) around (--x,--y)")
	decode    = flag.String("decode", "", "decode the hex dump of a command list instruction")
	dis       = flag.String("dis", "", "disassemble a stream file containing command list instructions")
	sim       = flag.Bool("sim", false, "simulate the connection if no device is found")
	bSpeed    = flag.Float64("burn-speed", 200, "burn speed of movement mm/sec")
	burnCu    = flag.Bool("burn-cu", false, "use PCB copper burn settings with --burn")
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
	aCode     = flag.Int("aruco", -1, "render an --aruco <code> centered at --x,--y of --size")
	invert    = flag.Bool("invert", false, "invert which parts of the --aruco code to burn")
	qFreq     = flag.Float64("shmoo-qfreq", 0, "mm squares to shmoo Q-pulse (ns) and Frequency (kHz)")
	box       = flag.String("box", "", "renders a box in mm format --box=dx,dy centered at --x, --y")
	repeat    = flag.Int("repeat", 0, "number of times to repeat a --box or --shmoo-qfreq")
	transform = flag.String("transform", "", "filename of json linear.Affine structure")
	mesh      = flag.Float64("mesh", 0.0, "3x3 tiles surrounded by mesh of width --mesh (with or without --hatch --burn)")
	poly      = flag.String("poly", "", "render polygon.Shapes from json file at (--x,--y)")
)

// jsonShapes outputs to os.Stdout the shapes of interest, p, in json
// format. See the zappem.net/pub/graphics/svgpoly examples/outline.go
// code for something that can render it.
func jsonShapes(p *polygon.Shapes, abort bool) {
	d, err := json.Marshal(p)
	if err != nil {
		log.Fatalf("failed to marshal shapes: %v", err)
	}
	fmt.Println(string(d))
	if abort {
		log.Fatal("aborting")
	}
}

// polysToHatch fills the polygon (avoiding holes) with a up-down and
// left-right hatch pattern.
func polysToHatch(list *glase.List, polys *polygon.Shapes, hatch float64) *glase.List {
	if hatch <= 0 {
		log.Fatalf("--hatch needs to be positive: got=%f", hatch)
	}
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
		lines, err := polys.Hatch(i, *scribe, hatch, *scribe/2, 0, holes...)
		if err != nil {
			log.Fatalf("Failed to x-hatch laser icon %d: %v", i, err)
		}
		for _, line := range lines {
			list = list.JumpXY(line.From.X, line.From.Y).MarkXY(line.To.X, line.To.Y).Sleep(30 * time.Microsecond)
		}
		lines, err = polys.Hatch(i, *scribe, hatch, *scribe/2, math.Pi/2, holes...)
		if err != nil {
			log.Fatalf("Failed to y-hatch laser icon %d: %v", i, err)
		}
		for _, line := range lines {
			list = list.JumpXY(line.From.X, line.From.Y).MarkXY(line.To.X, line.To.Y).Sleep(30 * time.Microsecond)
		}
	}
	return list
}

// polystoList renders polygon Shapes with a list of laser commands.
func polysToList(list *glase.List, polys *polygon.Shapes, hatch float64) *glase.List {
	ll, tr := polys.BB()
	if ll.X < -80 || ll.Y < -80 || tr.X > 80 || tr.Y > 80 {
		log.Fatalf("Polygons too large to render. Bounding-box: %.2f <-> %.2f", ll, tr)
	} else if ll.X < -75 || ll.Y < -75 || tr.X > 75 || tr.Y > 75 {
		log.Printf("WARNING: coordinates unstable at edges of polygons. Bounding-box: %.2f <-> %.2f", ll, tr)
	}
	var err error
	if *burn {
		p := glase.BasicProfile
		if *burnCu {
			p = glase.AblateCuProfile
		} else {
			p.MarkSpeed = *bSpeed
		}
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
	if hatch > 0 && *burn {
		list = polysToHatch(list, polys, hatch)
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

func polyAdjust(aff linear.Affine, p *polygon.Shapes) *polygon.Shapes {
	if p == nil {
		return nil
	}
	var sh *polygon.Shapes
	for _, v := range p.P {
		var pts []polygon.Point
		for _, pt := range v.PS {
			tX, tY := Transform.Apply(pt.X, pt.Y)
			pts = append(pts, polygon.Point{tX, tY})
		}
		sh = sh.Builder(pts...)
	}
	return sh
}

var Transform = linear.Affine{
	Axx: 1,
	Ayy: 1,
}

// This is the equivalent of (*polygon.Shapes).Transform() but using
// the linear.Affine parameterized form to capture the transformation.
func polyTransform(p *polygon.Shapes) *polygon.Shapes {
	return polyAdjust(Transform, p)
}

// renderBox renders a box around a central point (cX,cY) with width dX and height dY.
func renderBox(list *glase.List, cX, cY, dX, dY, hatch float64) *glase.List {
	prof := glase.PointerProfile
	if *burn {
		prof = glase.BasicProfile
		prof.MarkSpeed = *bSpeed
	}
	var err error
	if list, err = list.Start(prof); err != nil {
		log.Fatalf("unable to start box: %v", err)
	}
	hX, hY := dX/2, dY/2
	var box *polygon.Shapes
	box, err = box.Append([]polygon.Point{
		{cX - hX, cY - hY},
		{cX + hX, cY - hY},
		{cX + hX, cY + hY},
		{cX - hX, cY + hY},
	}...)
	if err != nil {
		log.Fatalf("unable to build box polygon: %v")
	}
	box = polyTransform(box)
	for i := 0; i <= *repeat; i++ {
		list = polysToList(list, box, 0)
	}
	if hatch != 0 && *burn {
		list = polysToHatch(list, box, hatch)
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
	polys.Union()
	polys = polyTransform(polys)
	list = polysToList(list, polys, *hatch)

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
	polys.Union()
	polys = polyTransform(polys)
	return polysToList(list, polys, *hatch)
}

// renderText text at desired coordinates with desired font, size and
// alignment. It applies the Transform.
func renderText(list *glase.List, fnt *hershey.Font, x, y, size float64, banner string, align polymark.Alignment) *glase.List {
	pen := &polymark.Pen{
		Scribe:  *scribe,
		Reflect: true,
	}
	text := polyTransform(pen.Text(nil, x, y, size, align, fnt, banner))
	text.Union()
	return polysToList(list, text, *hatch)
}

// renderQFShmoo renders a 2D array of Q-pulse (ns) and Frequency
// (kHz) squares of size edge, with axis labels.
func renderQFShmoo(list *glase.List, edge float64) *glase.List {
	step := edge + 1
	margin := 3 * step

	//width := 11*step + margin
	//height := 16*step + step

	fnt, err := hershey.New(*font)
	if err != nil {
		log.Fatalf("Failed to load font %q: %v", font, err)
	}
	_, xL, xR := fnt.Text("M")
	scale := edge / (float64(xR-xL) * *scribe) * 0.75
	for r := 0; r <= *repeat; r++ {
		for i := 1; i <= 10; i++ {
			q := time.Duration(i) * time.Nanosecond
			x := *gotoX + margin + step*(float64(i)-0.5)
			if r == 0 {
				list = renderText(list, fnt, x, *gotoY+step-1, scale, fmt.Sprint(i), polymark.AlignCenter|polymark.AlignAbove)
				log.Printf("q-pulse center top @ (%.1f,%.1f): %v", x, *gotoY+step-1, q)
			}
			for j := 1; j <= 15; j++ {
				f := 10.0 * float64(j)
				y := *gotoY + step*(float64(j)+0.5)
				if r == 0 && i == 1 {
					list = renderText(list, fnt, *gotoX+margin-1, y, scale, fmt.Sprintf("%.0f", f), polymark.AlignRight|polymark.AlignMiddle)
					log.Printf("freq (kHz) right middle @ (%.1f,%.1f): %.0f", *gotoX+margin-1, y, f)
				}
				x0, y0 := x-.5*edge, y-.5*edge
				x1, y1 := x0+edge, y0+edge
				var sq *polygon.Shapes
				sq, err = sq.Append([]polygon.Point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}...)
				if err != nil {
					log.Fatalf("unable to build square %f,%f <- %f,%f", x0, y0, x1, y1)
				}
				prof := glase.BasicProfile
				prof.MarkSpeed = *bSpeed
				prof.MarkFrequency = f
				prof.MarkPowerRatio = q

				list, err = list.Start(prof)
				if err != nil {
					log.Fatalf("failed to start hatch profile %v: %v", prof, err)
				}
				sq = polyTransform(sq)
				list = polysToHatch(list, sq, *hatch)

				prof = glase.BasicProfile
				prof.MarkSpeed = *bSpeed
				list, err = list.Start(prof)
				if err != nil {
					log.Fatalf("failed to restore profile %v: %v", prof, err)
				}
			}
		}
	}
	return list
}

func renderAruco(list *glase.List, x, y, size float64, code int) *glase.List {
	a, err := aruco.Encode(code)
	if err != nil {
		log.Fatalf("Unable to encode %d: %v", code, err)
	}
	delta := size / 10
	margin := (size - 7*delta) / 2
	half := size / 2
	var squares *polygon.Shapes
	for i := 0; i < len(a); i++ {
		px := x - half + margin + delta*float64(i)
		for j, light := range a[i] {
			py := y + half - (margin + delta*float64(j))
			if light != *invert {
				continue
			}
			// minor extension of the upper right to
			// assist with union merge.
			squares = squares.Builder([]polygon.Point{
				{px, py},
				{px + delta + *scribe/10, py},
				{px + delta + *scribe/10, py + delta + *scribe/10},
				{px, py + delta + *scribe/10},
			}...)
		}
	}
	squares.Union()
	squares = polyTransform(squares)
	return polysToList(list, squares, *hatch)
}

// renderMesh renders the negative of a 3x3 collection of 3mm squares centered at x,y
func renderMesh(list *glase.List, x, y, gap float64) *glase.List {
	var squares *polygon.Shapes
	const width = 3
	step := gap + 3
	from := -(gap+width*step)/2 + gap
	for i := 0; i < width; i++ {
		x0 := x + from + float64(i)*step
		for j := 0; j < width; j++ {
			y0 := y + from + float64(j)*step
			squares = squares.Builder([]polygon.Point{
				{x0, y0},
				{x0 + width, y0},
				{x0 + width, y0 + width},
				{x0, y0 + width},
			}...)
		}
	}
	squares.Union()
	neg, err := squares.Negative(gap)
	if err != nil {
		log.Fatalf("failed to generate negative: %v", err)
	}
	if len(neg.P) != 10 {
		log.Fatalf("expecting 10 polygons, got %d", len(neg.P))
	}
	neg = polyTransform(neg)
	return polysToList(list, neg, *hatch)
}

// Directly render some polygon.Shapes.
func renderPoly(list *glase.List, path string) *glase.List {
	d, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read %q: %v", path, err)
	}
	var p polygon.Shapes
	if err := json.Unmarshal(d, &p); err != nil {
		log.Fatalf("unable to parse %q: %v", path, err)
	}
	shapes := &p
	shapes = polyAdjust(linear.Affine{Axx: 1, Ayy: 1, Dx: *gotoX, Dy: *gotoY}, shapes)
	shapes = polyTransform(shapes)
	return polysToList(list, shapes, *hatch)
}

func main() {
	flag.Parse()

	var conn *glase.Conn
	var err error

	// Write this at the end so we can get a sense of how long
	// things took from the log message timestamps.
	defer log.Print("Done.")

	ctx := context.Background()

	if *transform != "" {
		d, err := os.ReadFile(*transform)
		if err != nil {
			log.Fatalf("failed to read %q: %v", *transform, err)
		}
		if err := json.Unmarshal(d, &Transform); err != nil {
			log.Fatalf("failed to decode %q: %v", *transform, err)
		}
	}

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
		// Note --transform is ignored for this pattern.
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed * 2
			list, err = list.Start(p)
			if err != nil {
				log.Fatalf("failed to start profile %v: %v", p, err)
			}
		} else {
			list, err = list.Start(glase.PointerProfile)
			if err != nil {
				log.Fatalf("failed to start pointer profile: %v", err)
			}
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

		x0, y0 := Transform.Apply(fromX, fromY)
		x1, y1 := Transform.Apply(fromX+float64(size), fromY+float64(size))

		for i := 0; i <= size; i++ {
			x2, y2 := Transform.Apply(fromX+float64(i), fromY+float64(i))
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
			if err != nil {
				log.Fatalf("unable to burn cm grid thicker: %v", err)
			}
		}

		for i := 0; i <= size; i += 10 {
			x2, y2 := Transform.Apply(fromX+float64(i), fromY+float64(i))
			if *burn {
				list = list.JumpXY(x0, y2).MarkXY(x1, y2).Sleep(30 * time.Microsecond)
				list = list.JumpXY(x2, y0).MarkXY(x2, y1).Sleep(30 * time.Microsecond)
			} else {
				list = list.JumpXY(x0, y2).JumpXY(x1, y2)
				list = list.JumpXY(x2, y0).JumpXY(x2, y1)
			}
		}
	} else if *banner != "" {
		fnt, err := hershey.New(*font)
		if err != nil {
			log.Fatalf("Failed to load font %q: %v", font, err)
		}
		list = renderText(list, fnt, *gotoX, *gotoY, *size, *banner, polymark.AlignCenter|polymark.AlignCenter)
	} else if *box != "" {
		fields := strings.Split(strings.TrimSpace(*box), ",")
		if len(fields) != 2 {
			log.Fatalf("--box=<dx>,<dy>, got --box=%q", *box)
		}
		dx, err1 := strconv.ParseFloat(fields[0], 64)
		if err1 != nil {
			log.Fatalf("--box dx=%q?: %v", fields[0], err1)
		}
		dy, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil {
			log.Fatalf("--box dy=%q?: %v", fields[1], err2)
		}
		list = renderBox(list, *gotoX, *gotoY, dx, dy, *hatch)
	} else if *laser {
		list = renderLaser(list, *size)
	} else if *qFreq != 0 {
		list = renderQFShmoo(list, *qFreq)
	} else if *mesh > 0.0 {
		if *mesh < *hatch && *burn {
			log.Fatalf("--burn --hatch of --mesh needs to be --hatch < --mesh: but reverse: %.3f >= %.3f", *hatch, *mesh)
		}
		list = renderMesh(list, *gotoX, *gotoY, *mesh)
	} else if *poly != "" {
		list = renderPoly(list, *poly)
	} else if *polystar != 0 {
		if *polystar < 3 {
			log.Fatalf("Need --poly value >= 3, got %d", *poly)
		}
		theta := math.Pi / float64(*polystar)
		if *burn {
			p := glase.BasicProfile
			p.MarkSpeed = *bSpeed
			list, err = list.Start(p)
		} else {
			list, err = list.Start(glase.PointerProfile)
		}
		repeatFrom := list.Offset()
		for i := 0; i <= *polystar*2; i++ {
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
	} else if *aCode != -1 {
		list = renderAruco(list, *gotoX, *gotoY, *size, *aCode)
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
	} else if *circle {
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
	tX, tY := Transform.Apply(*gotoX, *gotoY)
	conn.GotoXY(tX, tY)
	time.Sleep(500 * time.Millisecond)
	x, y, err = conn.GetXY()
	if err != nil {
		log.Fatalf("Failed to read XY: %v", err)
	}
	log.Printf("Final effective XY=(%f, %f)", x, y)
}
