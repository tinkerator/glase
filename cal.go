package glase

import (
	"encoding/binary"
	"fmt"
	"log"
)

// mm2cal holds the conversion factor from mm to galvo units.
// Roughly 0.0025 mm is a single galvo unit. This is the default,
// but it can be overriden via a connection specific scale factor.
// Weak attempt to calibrate my machine as the default.
const mm2galvo = 406.51

// Use this to override the mm2galvo scale factor for the connected
// laser.
func (c *Conn) SetMM2Galvo(mm2galvo float64) (old float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old = c.mm2galvo
	c.mm2galvo = mm2galvo
	return
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
// The data entries make up a 65x65 table.
func (c *Conn) UploadCorrections(data []byte) error {
	// cmd:Reset
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
