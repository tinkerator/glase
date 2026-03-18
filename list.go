package glase

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
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
	atDuration
	atFrequency
	atInt
	atPower
	atSpeed
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
		args:   []argType{atDisplacement, atDisplacement},
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
		args:   []argType{atDisplacement, atDisplacement},
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
			case atDisplacement:
				x, _ := c.fXY(v, 0)
				s = fmt.Sprintf("%.3f", x)
			case atDuration:
				s = (time.Duration(time.Microsecond) * time.Duration(v)).String()
			case atInt:
				s = fmt.Sprintf("%d", v)
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
	x, y     float64
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

// Space returns the number of entries not yet written.
func (list *List) Space() int {
	list.mu.Lock()
	defer list.mu.Unlock()
	return MaxListCommands - len(list.commands)
}

// ErrFull indicates that the caller has over filled the list.
var ErrFull = errors.New("too full")

// pack packs a command sequence into a list of commands. It returns
// list, which allows (*List).pack()s to be chained. This method is
// called with the list mutex locked.
func (list *List) pack(cmdargs ...uint16) *List {
	var data Assembled
	if len(list.commands) == MaxListCommands {
		list.err = ErrFull
		return list
	}
	binary.Encode(data[:], binary.LittleEndian, cmdargs)
	list.commands = append(list.commands, data)
	return list
}

// JumpXY Jumps the pointer (laser off) to (x,y) mm coordinates.
func (list *List) JumpXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	dx, dy := x-list.x, y-list.y
	id := list.c.iD(dx, dy)
	iX, iY := list.c.iXY(x, y)
	list.x, list.y = x, y
	const angle = 0
	return list.pack(JumpTo, iY, iX, angle, id)
}

// MarkXY moves the laser enabled (laser on) to (x,y) mm coordinates.
func (list *List) MarkXY(x, y float64) *List {
	list.mu.Lock()
	defer list.mu.Unlock()
	dx, dy := x-list.x, y-list.y
	id := list.c.iD(dx, dy)
	iX, iY := list.c.iXY(x, y)
	list.x, list.y = x, y
	const angle = 0
	return list.pack(MarkTo, iY, iX, angle, id)
}
