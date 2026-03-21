package glase

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// MaxListCommands is the capacity of a command list.
const MaxListCommands = 256

const (
	JumpTo          uint16 = 0x8001
	EndOfList              = 0x8002
	DelayTime              = 0x8004
	MarkTo                 = 0x8005
	JumpSpeed              = 0x8006
	LaserOnDelay           = 0x8007
	LaserOffDelay          = 0x8008
	MarkFrequency          = 0x800a
	MarkPowerRatio         = 0x800b
	MarkSpeed              = 0x800c
	JumpDelay              = 0x800d
	PolygonDelay           = 0x800f
	SetCo2FPK              = 0x801e
	ChangeMarkCount        = 0x8023
	ReadyMark              = 0x8051
)

var encoder = map[string]uint16{
	"JumpTo":          JumpTo,
	"EndOfList":       EndOfList,
	"DelayTime":       DelayTime,
	"MarkTo":          MarkTo,
	"JumpSpeed":       JumpSpeed,
	"LaserOnDelay":    LaserOnDelay,
	"LaserOffDelay":   LaserOffDelay,
	"MarkFrequency":   MarkFrequency,
	"MarkPowerRatio":  MarkPowerRatio,
	"MarkSpeed":       MarkSpeed,
	"JumpDelay":       JumpDelay,
	"PolygonDelay":    PolygonDelay,
	"SetCo2FPK":       SetCo2FPK,
	"ChangeMarkCount": ChangeMarkCount,
	"ReadyMark":       ReadyMark,
}

// argument types
type argType int

const (
	atDisplacement argType = iota
	atDistance
	atDuration
	atFrequency
	atInt
	atPower
	atSpeed
	atAngle
)

// Detail holds a lookup table for decoding an assembled command.
type Detail struct {
	name   string
	desc   string
	opcode uint16
	args   []argType
}

// decoder holds a decoding map for each supported opcode.
var decoder = map[uint16]Detail{
	JumpTo: {
		name:   "JumpTo",
		desc:   "Move the laser to target a specific point without marking.",
		opcode: JumpTo,
		args:   []argType{atDisplacement, atDisplacement, atAngle, atDistance},
	},
	EndOfList: {
		name:   "EndOfList",
		opcode: EndOfList,
		desc:   "Indicates the end of the command list sequence. Typically used to pad the list to 256 entries.",
	},
	DelayTime: {
		name:   "DelayTime",
		desc:   "This is a delay inserted into the command sequence. Equivalent to time.Sleep()",
		opcode: DelayTime,
		args:   []argType{atDuration},
	},
	MarkTo: {
		name:   "MarkTo",
		desc:   "Move the laser to target a specific point, lasing along the way.",
		opcode: MarkTo,
		args:   []argType{atDisplacement, atDisplacement, atAngle, atDistance},
	},
	JumpSpeed: {
		name:   "JumpSpeed",
		desc:   "Configures future jumps to execute at this speed. Units assumed to be galvo units per millisecond.",
		opcode: JumpSpeed,
		args:   []argType{atSpeed},
	},
	LaserOnDelay: {
		name:   "LaserOnDelay",
		desc:   "Relative to start of mirror motion, delay this number of microseconds to activate laser.",
		opcode: LaserOnDelay,
		args:   []argType{atDuration},
	},
	LaserOffDelay: {
		name:   "LaserOffDelay",
		desc:   "Relative to start of completion of marking motion, delay before turning the laser off.",
		opcode: LaserOffDelay,
		args:   []argType{atDuration},
	},
	MarkFrequency: {
		name:   "MarkFrequency",
		desc:   "Frequency of laser pulses. Less frequent pulses deliver more power.",
		opcode: MarkFrequency,
		args:   []argType{atFrequency},
	},
	MarkPowerRatio: {
		name:   "MarkPowerRatio",
		desc:   "Set the fraction of laser power marking will use. Units unknown, assumed higher number is higher power.",
		opcode: MarkPowerRatio,
		args:   []argType{atPower},
	},
	MarkSpeed: {
		name:   "MarkSpeed",
		desc:   "Configures future marking to execute at this speed. Units assumed to be galvo units per millisecond.",
		opcode: MarkSpeed,
		args:   []argType{atSpeed},
	},
	JumpDelay: {
		name:   "JumpDelay",
		desc:   "Microseconds to wait after a jump to let mirrors settle.",
		opcode: JumpDelay,
		args:   []argType{atDuration},
	},
	PolygonDelay: {
		name:   "PolygonDelay",
		desc:   "Microseconds to wait between segments of a set of connected lines. The laser continues to be on through this delay.",
		opcode: PolygonDelay,
		args:   []argType{atDuration},
	},
	SetCo2FPK: {
		name:   "SetCo2FPK",
		desc:   "Microsecond delay to suppress (First-Pulse-Killer) the initial power surge of the laser when starting to mark.",
		opcode: SetCo2FPK,
		args:   []argType{atDuration, atDuration},
	},
	ChangeMarkCount: {
		name:   "ChangeMarkCount",
		desc:   "Change the number of times a subsequent sequence is executed.",
		opcode: ChangeMarkCount,
		args:   []argType{atInt},
	},
	ReadyMark: {
		name:   "ReadyMark",
		desc:   "Wait for an external signal to start rest of program. Likely the foot pedal.",
		opcode: ReadyMark,
	},
}

// Assembled is an assembled line.
type Assembled [12]byte

// Disassemble generates a string representation what is known about the assembled command.
func (c *Conn) Disassemble(a Assembled) string {
	cmdargs := make([]uint16, 6)
	binary.Decode(a[:], binary.LittleEndian, cmdargs)
	d, ok := decoder[cmdargs[0]]
	if !ok {
		return fmt.Sprintf("unknown:%v", cmdargs)
	}
	var parts []string
	lastZero := 6
	for i := 5; i > 0; i-- {
		if cmdargs[i] != 0 {
			break
		}
		lastZero = i - 1
	}
	for i := 0; i < 5; i++ {
		if i >= lastZero && i >= len(d.args) {
			break
		}
		v := cmdargs[1+i]
		if i < len(d.args) {
			var s string
			switch d.args[i] {
			case atAngle:
				if v == 0x8000 {
					s = ">= 90 deg"
				} else {
					s = fmt.Sprintf("%.2f deg", float64(v)*90.0/float64(0x8000))
				}
			case atDisplacement:
				x, _ := c.fXY(v, 0)
				s = fmt.Sprintf("%.3f", x)
			case atDistance:
				x := float64(v) / c.mm2galvo
				s = fmt.Sprintf("%.3f", x)
			case atDuration:
				s = (time.Duration(time.Microsecond) * time.Duration(v)).String()
			case atInt:
				s = fmt.Sprintf("%d", v)
			case atPower:
				s = fmt.Sprintf("%d ?", v)
			case atSpeed:
				x := float64(v) / c.mm2galvo * 1000
				s = fmt.Sprintf("%.0f mm/s", x)
			default:
				s = fmt.Sprintf("?%d", v)
			}
			parts = append(parts, s)
		} else {
			parts = append(parts, fmt.Sprintf("?0x%x", v))
		}
	}
	return fmt.Sprint(d.name, "\t", strings.Join(parts, ", "))
}

// ParseListCommand parses a string and generates an Assembled command.
func (c *Conn) ParseListCommand(asm string) (a Assembled, err error) {
	if d, err2 := hex.DecodeString(asm); err2 == nil {
		if len(d) != 12 {
			err = fmt.Errorf("%q is not 12 bytes long", asm)
			return
		}
		copy(a[:], d)
		return
	}
	fs := strings.Fields(asm)
	if len(fs) < 1 {
		err = fmt.Errorf("%q has no fields", asm)
		return
	}
	opcode, ok := encoder[fs[0]]
	if !ok {
		err = fmt.Errorf("%q is not recognized in %q", fs[0], asm)
	}

	err = fmt.Errorf("TODO not done yet %04x with %q", opcode, asm)
	return
}

type List struct {
	c        *Conn
	mu       sync.Mutex
	err      error
	pts      int
	x0, y0   float64
	x1, y1   float64
	commands []Assembled
}

// NewList defines a command list associated with the connection. The
// connection, as needed, provides detailed parameters specific to the
// connected device.
func (c *Conn) NewList() *List {
	return &List{
		c: c,
	}
}

// Stream sends a stream of assembled commands over the returned
// channel. This will mutex Lock the list while streaming.
func (list *List) Stream() <-chan Assembled {
	as := make(chan Assembled)
	go func() {
		defer close(as)
		list.mu.Lock()
		defer list.mu.Unlock()
		for _, a := range list.commands {
			as <- a
		}
	}()
	return as
}

// pack packs a command sequence into a list of commands. It returns
// list, which allows (*List).pack()s to be chained. This method is
// called with the list mutex locked. There is no limit when building
// a list, although rendering the list to the laser packages lists
// with 256 entry sub-lists, padding with an end-of-list command.
func (list *List) pack(cmdargs ...uint16) *List {
	var data Assembled
	binary.Encode(data[:], binary.LittleEndian, cmdargs)
	list.commands = append(list.commands, data)
	return list
}

// JumpXY Jumps the pointer (laser off) to (x,y) mm coordinates.
func (list *List) JumpXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	dx, dy := x-list.x1, y-list.y1
	id := list.c.iD(dx, dy)
	iX, iY := list.c.iXY(x, y)
	angle := uint16(0)

	list.pts = 1
	list.x0, list.y0 = list.x1, list.y1
	list.x1, list.y1 = x, y

	return list.pack(JumpTo, iY, iX, angle, id)
}

// lineXY marks a line recursively not drawing any segment greater
// than 10mm in length. This function is called locked.
func (list *List) lineXY(x, y float64) *List {
	var angle uint16
	dx1, dy1 := x-list.x1, y-list.y1

	if dx1*dx1+dy1*dy1 > 100.0 {
		midX, midY := 0.5*(list.x1+x), 0.5*(list.y1+y)
		return list.lineXY(midX, midY).lineXY(x, y)
	}

	if list.pts > 1 {
		// Enough history to compute angle, which is
		// proportional to any acute subtended angle between
		// two successive line segments (reset by any jump).
		dx0, dy0 := list.x1-list.x0, list.y1-list.y0
		d02xd12 := (dx0*dx0 + dy0*dy0) * (dx1*dx1 + dy1*dy1)
		if d02xd12 > 0 {
			dot := (dx0*dx1 + dy0*dy1)
			if theta := math.Acos(dot/math.Sqrt(d02xd12)) / math.Pi * 180.0; theta < 90 {
				angle = uint16(math.Round(theta / 90 * 32768))
			} else {
				angle = 0x8000
			}

		} // else angle = 0
	}
	id := list.c.iD(dx1, dy1)
	iX, iY := list.c.iXY(x, y)

	list.pts++
	list.x0, list.y0 = list.x1, list.y1
	list.x1, list.y1 = x, y

	return list.pack(MarkTo, iY, iX, angle, id)
}

// MarkXY moves the laser enabled (laser on) to (x,y) mm coordinates.
func (list *List) MarkXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	return list.lineXY(x, y)
}
