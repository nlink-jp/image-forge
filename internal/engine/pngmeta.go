package engine

// PNG text-chunk writer. Go's image/png encoder does not expose tEXt/iTXt
// chunks, and image-forge keeps a zero-extra-dependency posture, so we hand-roll
// the chunk insertion here. This file carries NO build tag — it is pure Go and is
// compiled (and unit-tested) under both the stub and the cgo_sdcpp builds.
//
// A PNG chunk is: 4-byte length (big-endian, of the data only) + 4-byte type +
// data + 4-byte CRC-32 (IEEE, computed over type+data). Text chunks are inserted
// immediately after the IHDR chunk (the first chunk after the 8-byte signature).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
)

// pngSignature is the 8-byte PNG file header.
var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// encodePNGWithText encodes img as a PNG, then splices the given text chunks in
// immediately after the IHDR chunk. When texts is empty the PNG is returned
// unchanged. Each entry becomes a tEXt chunk when its Text is Latin-1-safe
// (every rune <= 0xFF), else an iTXt (UTF-8) chunk so Unicode prompts round-trip.
func encodePNGWithText(img image.Image, texts []PNGText) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	base := buf.Bytes()
	if len(texts) == 0 {
		return base, nil
	}

	// Locate the insertion point right after IHDR: verify the signature, read the
	// IHDR data length (uint32 at offset 8), and skip sig(8) + len(4) + type(4) +
	// data(ihdrLen) + crc(4).
	if len(base) < 8+12 || !bytes.Equal(base[:8], pngSignature) {
		return nil, errors.New("pngmeta: not a PNG (bad signature)")
	}
	ihdrLen := binary.BigEndian.Uint32(base[8:12])
	insertPos := 8 + 12 + int(ihdrLen)
	if insertPos < 0 || insertPos > len(base) {
		return nil, errors.New("pngmeta: truncated IHDR")
	}

	var chunks bytes.Buffer
	for _, t := range texts {
		c, err := encodeTextChunk(t)
		if err != nil {
			return nil, err
		}
		chunks.Write(c)
	}

	out := make([]byte, 0, len(base)+chunks.Len())
	out = append(out, base[:insertPos]...)
	out = append(out, chunks.Bytes()...)
	out = append(out, base[insertPos:]...)
	return out, nil
}

// encodeTextChunk builds a single tEXt or iTXt chunk for one PNGText entry. tEXt
// is used when the text is Latin-1-safe; iTXt (UTF-8) otherwise.
func encodeTextChunk(t PNGText) ([]byte, error) {
	kw := []byte(t.Keyword)
	if len(kw) < 1 || len(kw) > 79 {
		return nil, fmt.Errorf("pngmeta: keyword must be 1-79 bytes, got %d", len(kw))
	}

	if latin1, ok := toLatin1(t.Text); ok {
		// tEXt: keyword \0 latin1(text)
		data := make([]byte, 0, len(kw)+1+len(latin1))
		data = append(data, kw...)
		data = append(data, 0x00)
		data = append(data, latin1...)
		return assembleChunk("tEXt", data), nil
	}

	// iTXt (UTF-8): keyword \0 compFlag(0) compMethod(0) langtag\0 transkw\0 utf8(text)
	// i.e. after keyword\0 come four zero bytes (compFlag, compMethod, empty
	// language-tag terminator, empty translated-keyword terminator) then the text.
	text := []byte(t.Text)
	data := make([]byte, 0, len(kw)+5+len(text))
	data = append(data, kw...)
	data = append(data, 0x00)       // keyword null separator
	data = append(data, 0x00, 0x00) // compression flag, compression method
	data = append(data, 0x00, 0x00) // empty language tag, empty translated keyword
	data = append(data, text...)
	return assembleChunk("iTXt", data), nil
}

// toLatin1 returns s encoded as Latin-1 (one byte per rune) when every rune is
// <= 0xFF; otherwise ok is false.
func toLatin1(s string) (out []byte, ok bool) {
	out = make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, false
		}
		out = append(out, byte(r))
	}
	return out, true
}

// assembleChunk builds one PNG chunk: length(4, big-endian, data only) + type(4)
// + data + CRC-32(4, IEEE over type+data).
func assembleChunk(ctype string, data []byte) []byte {
	typeAndData := make([]byte, 0, 4+len(data))
	typeAndData = append(typeAndData, ctype...)
	typeAndData = append(typeAndData, data...)

	out := make([]byte, 0, 4+len(typeAndData)+4)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(data)))
	out = append(out, u32[:]...)
	out = append(out, typeAndData...)
	binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(typeAndData))
	out = append(out, u32[:]...)
	return out
}
