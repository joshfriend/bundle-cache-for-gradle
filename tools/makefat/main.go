// makefat combines two or more Mach-O executables into a single universal
// (fat) binary, replicating what Apple's lipo -create does.
//
// Usage: makefat <output> <input>...
package main

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
)

const fatMagic uint32 = 0xcafebabe

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: makefat <output> <input>...")
		os.Exit(1)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "makefat:", err)
		os.Exit(1)
	}
}

type slice struct {
	cpu, subcpu, align uint32
	data               []byte
}

func run(outPath string, inputs []string) error {
	var slices []slice
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		// arm64 executables require 2^14 (16 KB) alignment; everything else 2^12 (4 KB).
		align := uint32(12)
		if f.Cpu == macho.CpuArm64 {
			align = 14
		}
		slices = append(slices, slice{
			cpu: uint32(f.Cpu),
			// Strip the CPU_SUBTYPE_LIB64 capability bits (0xff000000) that
			// the linker sets on 64-bit slices but lipo omits in fat arch entries.
			subcpu: uint32(f.SubCpu) &^ 0xff000000,
			align:  align,
			data:   data,
		})
		f.Close()
	}

	// Compute byte offsets for each slice.
	type entry struct{ cpu, subcpu, offset, size, align uint32 }
	var entries []entry
	offset := uint32(8 + 20*len(slices)) // fat header + n * fat_arch structs
	for _, s := range slices {
		alignment := uint32(1) << s.align
		offset = (offset + alignment - 1) &^ (alignment - 1)
		entries = append(entries, entry{s.cpu, s.subcpu, offset, uint32(len(s.data)), s.align})
		offset += uint32(len(s.data))
	}

	// Assemble the fat binary in memory.
	var buf bytes.Buffer
	w32 := func(v uint32) { _ = binary.Write(&buf, binary.BigEndian, v) }
	w32(fatMagic)
	w32(uint32(len(slices)))
	for _, e := range entries {
		w32(e.cpu)
		w32(e.subcpu)
		w32(e.offset)
		w32(e.size)
		w32(e.align)
	}
	for i, s := range slices {
		for uint32(buf.Len()) < entries[i].offset {
			buf.WriteByte(0) // alignment padding
		}
		buf.Write(s.data)
	}

	return os.WriteFile(outPath, buf.Bytes(), 0o755)
}
