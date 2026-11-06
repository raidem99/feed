package main

import (
	"encoding/binary"
	"net/http"
	"unicode/utf16"
)

// BuildID3 returns an ID3v2.3 tag (header + frames) to prepend to an MP3 file.
// Empty fields are omitted; cover (JPEG/PNG bytes) becomes an attached picture.
// It returns nil if there is nothing to write.
func BuildID3(title, album, artist string, cover []byte) []byte {
	var frames []byte
	add := func(id, val string) {
		if val != "" {
			frames = append(frames, id3Frame(id, id3Text(val))...)
		}
	}
	add("TIT2", title)  // title
	add("TALB", album)  // album = show name
	add("TPE1", artist) // artist = show author
	add("TCON", "Podcast")
	if len(cover) > 0 {
		frames = append(frames, id3Frame("APIC", id3Picture(cover))...)
	}
	if len(frames) == 0 {
		return nil
	}
	tag := []byte{'I', 'D', '3', 3, 0, 0} // version 2.3.0, no flags
	tag = append(tag, synchsafe(len(frames))...)
	return append(tag, frames...)
}

// id3Frame wraps a body in a v2.3 frame header: ID, 4-byte size, 2 flag bytes.
func id3Frame(id string, body []byte) []byte {
	f := make([]byte, 10, 10+len(body))
	copy(f, id)
	binary.BigEndian.PutUint32(f[4:], uint32(len(body))) // plain big-endian in v2.3
	return append(f, body...)
}

// id3Text encodes a text frame body as UTF-16 with BOM (encoding byte 0x01).
func id3Text(s string) []byte {
	b := []byte{0x01, 0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8)) // little endian, matches the BOM
	}
	return b
}

// id3Picture builds an APIC body: front cover, empty description.
func id3Picture(img []byte) []byte {
	mime := http.DetectContentType(img)
	if mime != "image/png" {
		mime = "image/jpeg"
	}
	b := []byte{0x00} // text encoding ISO-8859-1 (for mime + description)
	b = append(b, mime...)
	b = append(b, 0x00) // end of mime
	b = append(b, 0x03) // picture type: front cover
	b = append(b, 0x00) // empty description, terminated
	return append(b, img...)
}

// synchsafe encodes n in 4 bytes of 7 bits each (top bit always 0).
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
}
