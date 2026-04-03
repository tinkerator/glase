package glase

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"zappem.net/pub/math/linear"
)

// mm2cal holds the conversion factor from mm to galvo units.  Roughly
// 0.0025 mm is a single galvo unit. This is the default, but it can
// be overriden via a connection specific scale factor.  Weak attempt
// to calibrate my machine as the default. Corrections come in two
// forms - this scale factor, and offsets of points on a 65 square
// grid. For calibrations we make, we set the scale to 400 and absorb
// all normalization deviation from that into the correction file.
const mm2galvo = 400

// Use this to override the mm2galvo scale factor for the connected
// laser.
func (c *Conn) SetMM2Galvo(mm2galvo float64) (old float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old = c.mm2galvo
	c.mm2galvo = mm2galvo
	return
}

// MM2Galvo indicates the normalization factor in use for converting
// mm to galvo units.
func (c *Conn) MM2Galvo() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mm2galvo
}

// int32ToUInt16 converts a signed integer (held in a int32) into a
// laser correction encoded signed integer (held in a uint16). The
// latter is **not** a twos complement format, but a sign bit followed
// by 15 bits of absolute value. This format is used exclusively for
// calibration data.
func int32ToUInt16(x int32) uint16 {
	if x < 0 {
		return uint16((-x) | 0x8000)
	}
	return uint16(x)
}

// UploadCorrections decodes the "jcz150.cor" content and loads it
// into the device. The data argument is the []byte content of this
// file which needs conversion to be loaded into the laser device.
// The data entries make up a 65x65 table of pairs of int32 little
// endian values {dy, dx}. The laser accepts a specific 16 bit
// translation of that data.
//
// As we interpret the data it enumerates deviations in the grid that
// start in the lower left (minimum x, minimum y) and proceed in +ve y
// direction before proceeding to the next x value with minimal y. The
// deviation is specifically how many galvo units wrong {dy,dx} a
// rendered point will be if it is not corrected. As such, this
// correction delta is subtracted from the intended galvo coordinates
// to correctly place the laser.
func (c *Conn) UploadCorrections(data []byte) error {
	_, err := c.Query(Reset, 1)
	if err != nil {
		return err
	}
	c.mu.Lock()
	status := c.status
	c.mu.Unlock()
	if status != 0x0220 {
		return fmt.Errorf("unexpected Reset response %04x", status)
	}

	// cmd:WriteCorTable
	if _, err = c.Query(WriteCorTable, 1); err != nil {
		return err
	}
	c.mu.Lock()
	status = c.status
	c.mu.Unlock()
	if status != 0x0220 {
		return fmt.Errorf("unexpected WriteCorTable response %04x", status)
	}

	//  write all of the correction data (no acknowledgment)
	suffix := uint16(0x000)
	for i := 36; i < len(data)-4; i += 8 {
		var n [2]int32
		if _, err := binary.Decode(data[i:i+8], binary.LittleEndian, n[:2]); err != nil {
			log.Fatalf("failed to decode correction data[%d:%d] %02x: %v", i, i+8, data[i:i+8], err)
		}
		x, y := int32ToUInt16(n[0]), int32ToUInt16(n[1])
		if err := c.Send(WriteCorLine, x, y, suffix); err != nil {
			return err
		}
		suffix = 1
	}

	return nil
}

type Ixy struct {
	x, y int
}

type Delta struct {
	Dx, Dy float64 // mm
}

func (c *Conn) DeriveCorrections(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)

	pts := make(map[Ixy]Delta)

	for sc.Scan() {
		line := sc.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue // skip empty lines
		}
		nums := strings.Fields(line)
		if len(nums) != 4 {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%d fields in %q", len(nums), line)
		}
		ix, err := strconv.Atoi(nums[0])
		if err != nil {
			return nil, err
		}
		iy, err := strconv.Atoi(nums[1])
		if err != nil {
			return nil, err
		}
		dx, err := strconv.ParseFloat(nums[2], 64)
		if err != nil {
			return nil, err
		}
		dy, err := strconv.ParseFloat(nums[3], 64)
		if err != nil {
			return nil, err
		}
		delta := Delta{dx, dy}
		ixy := Ixy{ix, iy}
		pts[ixy] = delta
	}

	refPts := len(pts)

	// Two passes of the pts: one to derive fit data from
	// reference points; one to fit fit data.
	for k := 0; k < 2; k++ {
		for i := -32; i <= 32; i++ {
			var lineX []int
			var lineY []int
			for j := -32; j <= 32; j++ {
				// Consider line ix=i
				pt := Ixy{i, j}
				if _, ok := pts[pt]; ok {
					lineX = append(lineX, j)
				}
				// Consider line iy=i
				pt = Ixy{j, i}
				if _, ok := pts[pt]; ok {
					lineY = append(lineY, j)
				}
			}
			if len(lineX) >= 3 && len(lineX) < 65 {
				var ptsX, ptsY []linear.Point
				for _, j := range lineX {
					pt := Ixy{i, j}
					delta := pts[pt]
					ptsX = append(ptsX, linear.Point{X: float64(j), Y: delta.Dx})
					ptsY = append(ptsY, linear.Point{X: float64(j), Y: delta.Dy})
				}
				fitX, err := linear.FitPoly(2, ptsX)
				if err != nil {
					log.Fatalf("failed to fit X for %v: %v", ptsY, err)
				}
				fitY, err := linear.FitPoly(2, ptsY)
				if err != nil {
					log.Fatalf("failed to fit Y for %v: %v", ptsY, err)
				}
				for j := -32; j <= 32; j++ {
					pt := Ixy{i, j}
					if _, present := pts[pt]; !present {
						dx := fitX.Expand(float64(j))
						dy := fitY.Expand(float64(j))
						pts[pt] = Delta{dx, dy}
					}
				}
			}
			if len(lineY) >= 3 && len(lineY) < 65 {
				var ptsX, ptsY []linear.Point
				for _, j := range lineY {
					pt := Ixy{j, i}
					delta := pts[pt]
					ptsX = append(ptsX, linear.Point{X: float64(j), Y: delta.Dx})
					ptsY = append(ptsY, linear.Point{X: float64(j), Y: delta.Dy})
				}
				fitX, err := linear.FitPoly(2, ptsX)
				if err != nil {
					log.Fatalf("failed to fit X for %v: %v", ptsY, err)
				}
				fitY, err := linear.FitPoly(2, ptsY)
				if err != nil {
					log.Fatalf("failed to fit Y for %v: %v", ptsY, err)
				}
				for j := -32; j <= 32; j++ {
					pt := Ixy{j, i}
					if _, present := pts[pt]; !present {
						dx := fitX.Expand(float64(j))
						dy := fitY.Expand(float64(j))
						pts[pt] = Delta{dx, dy}
					}
				}
			}
		}
	}
	if len(pts) != 65*65 {
		return nil, fmt.Errorf("interpolation of %d reference corrections does not yield grid of 65x65, got=%d points", refPts, len(pts))
	}
	buf := new(bytes.Buffer)
	buf.Write([]byte("JCZ_COR_2_1"))
	buf.Write(make([]byte, 17))
	binary.Write(buf, binary.BigEndian, []uint16{0xc001, 0xcafe, 0xbeef, 0xf00d})
	for i := -32; i <= 32; i++ {
		for j := -32; j <= 32; j++ {
			pt := Ixy{i, j}
			delta := pts[pt]
			dY := int32(c.mm2galvo * delta.Dy)
			dX := int32(c.mm2galvo * delta.Dx)
			binary.Write(buf, binary.LittleEndian, dY)
			binary.Write(buf, binary.LittleEndian, dX)
		}
	}
	binary.Write(buf, binary.LittleEndian, uint32(0x7))
	dest := path + ".cor"
	if err := os.WriteFile(dest, buf.Bytes(), 0644); err != nil {
		return nil, err
	}
	log.Printf("new correction data written to %q", dest)
	return nil, nil
}
