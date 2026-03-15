package glase

import (
	"encoding/binary"
	"fmt"
	"log"
)

// TODO Observed, but unknown meaning: 0x0003, 0x0008, ?

const (
	DisableLaser         Command = 0x0002
	EnableLaser                  = 0x0004
	ExecuteList                  = 0x0005
	SetPwmPulseWidth             = 0x0006
	GetVersion                   = 0x0007
	GetSerialNo                  = 0x0009 // not clear this is a simple command
	GetListStatus                = 0x000a
	GetPositionXY                = 0x000c
	GotoXY                       = 0x000d
	LaserSignalOff               = 0x000e
	LaserSignalOn                = 0x000f
	WriteCorLine                 = 0x0010
	ResetList                    = 0x0012
	RestartList                  = 0x0013
	WriteCorTable                = 0x0015
	SetControlMode               = 0x0016
	SetDelayMode                 = 0x0017
	SetMaxPolyDelay              = 0x0018
	SetEndOfList                 = 0x0019
	SetFirstPulseKiller          = 0x001a
	SetLaserMode                 = 0x001b
	SetTiming                    = 0x001c
	SetStandby                   = 0x001d
	SetPwmHalfPeriod             = 0x001e
	StopExecute                  = 0x001f
	StopList                     = 0x0020
	WritePort                    = 0x0021
	WriteAnalogPort1             = 0x0022
	WriteAnalogPort2             = 0x0023
	WriteAnalogPortX             = 0x0024
	ReadPort                     = 0x0025
	SetAxisMotionParam           = 0x0026
	SetAxisOriginParam           = 0x0027
	AxisGoOrigin                 = 0x0028
	MoveAxisTo                   = 0x0029
	GetAxisPos                   = 0x002a
	GetFlyWaitCount              = 0x002b
	GetMarkCount                 = 0x002d
	SetFpkParam2                 = 0x002e
	FiberPulseWidth              = 0x002f
	FiberGetConfigExtend         = 0x0030
	InputPort                    = 0x0031
	SetFlyRes                    = 0x0032
	Fiber_SetMo                  = 0x0033
	Fiber_GetStMO_AP             = 0x0034
	GetUserData                  = 0x0036
	GetFlySpeed                  = 0x0038
	DisableZ                     = 0x0039
	EnableZ                      = 0x003a
	SetZData                     = 0x003b
	SetSPISimmerCurrent          = 0x003c
	Reset                        = 0x0040
	GetMarkTime                  = 0x0041
	SetFpkParam                  = 0x0062

	PollStepping = 0x0069
)

// GetSerial reads the serial number
func (c *Conn) GetSerial() {
	resp, err := c.Query(GetSerialNo)
	if err != nil {
		log.Fatalf("error reading serial: %v", err)
	}
	log.Printf("read serial %02x", resp)
}

// GetVersion reads the FW version of the laser
func (c *Conn) GetVersion() {
	resp, err := c.Query(GetSerialNo)
	if err != nil {
		log.Fatalf("error reading version: %v", err)
	}
	log.Printf("read version %02x", resp)
}

// EnableLaser enables the laser
func (c *Conn) EnableLaser() {
	resp, err := c.Query(EnableLaser)
	if err != nil {
		log.Fatalf("error enabling laser: %v", err)
	}
	log.Printf("enable laser %02x", resp)
}

// SetControlMode sets the laser into the specified mode.
func (c *Conn) SetControlMode(mode uint16) {
	resp, err := c.Query(SetControlMode, mode)
	if err != nil {
		log.Fatalf("set control mode error: %v", err)
	}
	log.Printf("set control mode %02x", resp)
}

// GetYX
func (c *Conn) GetYX() (y, x float64) {
	resp, err := c.Query(GetPositionXY)
	if err != nil {
		log.Fatalf("get XY error: %v", err)
	}
	y = float64(int(resp[1]) - 0x8000)
	x = float64(int(resp[2]) - 0x8000)
	log.Printf("get Y=%.1f X=%.1f", y, x)
	return
}

// GotoYX - conventions for the laser are very backwards.
func (c *Conn) GotoYX(y, x float64) {
	iY := uint16(0x8000 + y)
	iX := uint16(0x8000 + x)
	resp, err := c.Query(GotoXY, iY, iX)
	if err != nil {
		log.Fatalf("goto XY error: %v", err)
	}
	if resp[3] != 0x220 {
		log.Fatalf("unexpected response %02x", resp)
	}
}

// UploadCorrections decodes the "jcz150.cor" content and loads it
// into the device. The data argument is the []byte content of this
// file which needs conversion to be loaded into the laser device.
func (c *Conn) UploadCorrections(data []byte) error {
	// cmd:Reset
	ans, err := c.Query(Reset, 1)
	if err != nil {
		return err
	} else if ans[3] != 0x0220 {
		return fmt.Errorf("unexpected Reset response %04x", ans)
	}
	log.Printf("Reset returned %02x", ans)

	// cmd:WriteCorTable
	ans, err = c.Query(WriteCorTable, 1)
	if err != nil {
		return err
	} else if ans[3] != 0x0220 {
		return fmt.Errorf("unexpected WriteCorTable response %04x", ans)
	}
	log.Printf("WriteCorTable returned %02x", ans)

	//  write all of the correction data (no acknowledgement)
	suffix := uint16(0x000)
	for i := 36; i < len(data)-4; i += 8 {
		var n [2]int32
		if _, err := binary.Decode(data[i:i+8], binary.LittleEndian, n[:2]); err != nil {
			log.Fatalf("failed to decode correction data[%d:%d] %02x: %v", i, i+8, data[i:i+8], err)
		}
		x, y := Int32ToUInt16(n[0]), Int32ToUInt16(n[1])
		if err := c.Send(WriteCorLine, x, y, suffix); err != nil {
			return err
		}
		suffix = 1
	}

	return nil
}
