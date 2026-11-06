package main

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestBuildID3(t *testing.T) {
	cover := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...)
	tag := BuildID3("Hällo", "My Show", "Jane", cover)

	if !bytes.HasPrefix(tag, []byte{'I', 'D', '3', 3, 0, 0}) {
		t.Fatalf("bad header % x", tag[:6])
	}
	// Synchsafe size must equal everything after the 10-byte header.
	size := int(tag[6])<<21 | int(tag[7])<<14 | int(tag[8])<<7 | int(tag[9])
	if size != len(tag)-10 {
		t.Fatalf("size field %d, actual %d", size, len(tag)-10)
	}
	for _, b := range tag[6:10] {
		if b&0x80 != 0 {
			t.Fatal("size byte has high bit set")
		}
	}

	// Walk the frames.
	frames := map[string][]byte{}
	for rest := tag[10:]; len(rest) > 0; {
		n := int(binary.BigEndian.Uint32(rest[4:8]))
		frames[string(rest[:4])] = rest[10 : 10+n]
		rest = rest[10+n:]
	}
	for _, id := range []string{"TIT2", "TALB", "TPE1", "TCON", "APIC"} {
		if frames[id] == nil {
			t.Errorf("missing frame %s", id)
		}
	}
	// Decode the title: encoding byte, BOM, UTF-16LE.
	body := frames["TIT2"]
	if body[0] != 1 || body[1] != 0xFF || body[2] != 0xFE {
		t.Fatalf("bad text prefix % x", body[:3])
	}
	var u []uint16
	for i := 3; i+1 < len(body); i += 2 {
		u = append(u, uint16(body[i])|uint16(body[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != "Hällo" {
		t.Errorf("title = %q", got)
	}
	if !bytes.Contains(frames["APIC"], []byte("image/png\x00\x03\x00")) {
		t.Errorf("APIC header wrong: % x", frames["APIC"][:20])
	}
}

func TestBuildID3GenreOnly(t *testing.T) {
	// The "Podcast" genre frame is always written, so the tag is never nil.
	if tag := BuildID3("", "", "", nil); tag == nil {
		t.Error("expected genre-only tag")
	}
}
