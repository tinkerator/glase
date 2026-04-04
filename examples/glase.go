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

	"zappem.net/pub/io/glase"
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
)

func main() {
	flag.Parse()

	var conn *glase.Conn
	var err error

	defer log.Print("done.")

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
			log.Fatalf("unable to parse %q", *decode)
		}
		s := conn.Disassemble(a)
		log.Printf("%q => %s", *decode, s)
		return
	}

	if *dis != "" {
		d, err := os.ReadFile(*dis)
		if err != nil {
			log.Fatalf("failed to read %q: %v", *dis, err)
		}
		for i, line := range strings.Split(string(d), "\n") {
			if line == "" {
				continue
			}
			a, err := conn.ParseListCommand(line)
			if err != nil {
				log.Fatalf("unable to parse (line %d): %q", i+1, line)
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
	} else if *poly != 0 {
		if *poly < 3 {
			log.Fatalf("need --poly value >= 3, got %d", *poly)
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
		}
		if !*burn {
			n, err := list.ReplayFrom(repeatFrom, 0)
			if err != nil {
				log.Fatalf("failed to repeat from %d: %v", repeatFrom, err)
			}
			log.Printf("appended %d repetitions", n)
		}
	}

	var data []byte
	if strings.HasSuffix(*cor, ".fixup") {
		log.Print("Loading data is ASCII fixup data, lines of: xi yi mmx mmy")
		_, err := conn.DeriveCorrections(*cor)
		log.Fatal("not able to continue yet: ", err)
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
			time.Sleep(10 * time.Second)
			cancel()
			log.Print("canceled, wait to exit")
		}
		wg.Wait()
		if err != nil {
			log.Fatalf("failure to run (--burn=%v): %v", *burn, err)
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
		log.Fatalf("failed to read XY: %v", err)
	}
	log.Printf("current XY=(%f, %f)", x, y)
	conn.GotoXY(*gotoX, *gotoY)
	time.Sleep(500 * time.Millisecond)
	x, y, err = conn.GetXY()
	if err != nil {
		log.Fatalf("failed to read XY: %v", err)
	}
	log.Printf("final XY=(%f, %f)", x, y)
}
