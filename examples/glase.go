package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"zappem.net/pub/io/glase"
)

var (
	info   = flag.Bool("info", false, "list the discovered laser devices")
	serial = flag.String("serial", "", "serial numbered device to connect to")
	cor    = flag.String("cor", "./jcz150.cor", "correction data file")
	scale  = flag.Float64("mm2gal", 0.0, "non-zero overrides number of galvo units to mm")
	gotoX  = flag.Float64("x", 0.0, "x coordinate at end of run")
	gotoY  = flag.Float64("y", 0.0, "y coordinate at end of run")
	radius = flag.Float64("radius", 5.0, "mm radius of --poly")
	circle = flag.Bool("circle", false, "step out a circle with the laser")
	poly   = flag.Int("poly", 0, "draw a 2*n sided star (n>=3) around (--x,--y)")
	decode = flag.String("decode", "", "decode the hex dump of a command list instruction")
	dis    = flag.String("dis", "", "disassemble a stream file containing command list instructions")
	sim    = flag.Bool("sim", false, "simulate the connection if no device is found")
)

func main() {
	flag.Parse()

	var conn *glase.Conn
	var err error

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
	if *poly != 0 {
		if *poly < 3 {
			log.Fatalf("need --poly value >= 3, got %d", *poly)
		}
		theta := math.Pi / float64(*poly)
		list := conn.NewList()
		for i := 0; i <= *poly*2; i++ {
			at := *radius
			if i&1 == 0 {
				at *= 0.5
			}
			ang := theta * float64(i)
			if i == 0 {
				list = list.JumpXY(*gotoX+at*math.Cos(ang), *gotoY+at*math.Sin(ang))
			} else {
				list = list.MarkXY(*gotoX+at*math.Cos(ang), *gotoY+at*math.Sin(ang))
			}
		}
		i := 0
		for a := range list.Stream() {
			s := conn.Disassemble(a)
			i++
			fmt.Printf("%5d: %q => %s\n", i, hex.EncodeToString([]byte(a[:])), s)
		}
		return
	}
	if *sim {
		log.Print("simulation has given up because of a lack of features")
	}

	data, err := os.ReadFile(*cor)
	if err != nil {
		log.Fatalf("Failed to read %q: %v", *cor, err)
	}
	if err := conn.UploadCorrections(data); err != nil {
		log.Fatalf("Failed to upload corrections %q: %v", *cor, err)
	}
	log.Printf("Correction data loaded from %q", *cor)

	if err := conn.EnableLaser(); err != nil {
		log.Fatalf("Failed to enable laser: %v", err)
	}
	if err := conn.SetControlMode(0); err != nil {
		log.Fatalf("Failed to set control mode: %v", err)
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
