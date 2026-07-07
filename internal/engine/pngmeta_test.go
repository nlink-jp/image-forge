package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// tinyImage builds a small non-uniform RGBA image for round-trip tests.
func tinyImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 80), B: 100, A: 255})
		}
	}
	return img
}

// parsedChunk is one decoded PNG chunk.
type parsedChunk struct {
	typ  string
	data []byte
}

// parseChunks walks a PNG byte stream (after the 8-byte signature) into its
// chunks, verifying each chunk's CRC-32 as it goes.
func parseChunks(t *testing.T, b []byte) []parsedChunk {
	t.Helper()
	if len(b) < 8 || !bytes.Equal(b[:8], pngSignature) {
		t.Fatalf("bad PNG signature")
	}
	var out []parsedChunk
	off := 8
	for off+12 <= len(b) {
		length := int(binary.BigEndian.Uint32(b[off : off+4]))
		typ := string(b[off+4 : off+8])
		dataStart := off + 8
		dataEnd := dataStart + length
		if dataEnd+4 > len(b) {
			t.Fatalf("chunk %q overruns buffer", typ)
		}
		data := b[dataStart:dataEnd]
		gotCRC := binary.BigEndian.Uint32(b[dataEnd : dataEnd+4])
		wantCRC := crc32.ChecksumIEEE(b[off+4 : dataEnd]) // over type+data
		if gotCRC != wantCRC {
			t.Fatalf("chunk %q CRC mismatch: got %08x want %08x", typ, gotCRC, wantCRC)
		}
		out = append(out, parsedChunk{typ: typ, data: data})
		off = dataEnd + 4
		if typ == "IEND" {
			break
		}
	}
	return out
}

func findChunk(chunks []parsedChunk, typ string) (parsedChunk, bool) {
	for _, c := range chunks {
		if c.typ == typ {
			return c, true
		}
	}
	return parsedChunk{}, false
}

func TestEncodePNGWithText_EmptyUnchanged(t *testing.T) {
	img := tinyImage()
	var plain bytes.Buffer
	if err := png.Encode(&plain, img); err != nil {
		t.Fatal(err)
	}
	got, err := encodePNGWithText(img, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain.Bytes()) {
		t.Fatalf("empty texts should return the PNG unchanged (%d vs %d bytes)", len(got), len(plain.Bytes()))
	}
}

func TestEncodePNGWithText_TEXtAndITXt(t *testing.T) {
	img := tinyImage()
	asciiText := "a cat\nSteps: 26, Seed: 42"
	jpText := "猫、詳細な背景" // non-Latin-1 => iTXt

	out, err := encodePNGWithText(img, []PNGText{
		{Keyword: "parameters", Text: asciiText},
		{Keyword: "image-forge", Text: jpText},
	})
	if err != nil {
		t.Fatal(err)
	}

	// (a) still decodes as a valid PNG with the same bounds.
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("png.Decode after splicing: %v", err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Fatalf("bounds changed: %v vs %v", decoded.Bounds(), img.Bounds())
	}

	// The spliced chunks must come immediately after IHDR (parse + CRC-check all).
	chunks := parseChunks(t, out)
	if len(chunks) < 3 || chunks[0].typ != "IHDR" {
		t.Fatalf("first chunk should be IHDR, got %+v", chunks)
	}
	if chunks[1].typ != "tEXt" || chunks[2].typ != "iTXt" {
		t.Fatalf("expected tEXt then iTXt after IHDR, got %q then %q", chunks[1].typ, chunks[2].typ)
	}

	// (b) tEXt: keyword \0 latin1(text).
	tExt, ok := findChunk(chunks, "tEXt")
	if !ok {
		t.Fatal("tEXt chunk missing")
	}
	kw, txt, found := bytes.Cut(tExt.data, []byte{0x00})
	if !found {
		t.Fatal("tEXt has no keyword separator")
	}
	if string(kw) != "parameters" {
		t.Errorf("tEXt keyword = %q, want parameters", kw)
	}
	if string(txt) != asciiText {
		t.Errorf("tEXt text = %q, want %q", txt, asciiText)
	}

	// (b) iTXt: keyword \0 00 00 00 00 utf8(text).
	iTxt, ok := findChunk(chunks, "iTXt")
	if !ok {
		t.Fatal("iTXt chunk missing")
	}
	ikw, rest, found := bytes.Cut(iTxt.data, []byte{0x00})
	if !found {
		t.Fatal("iTXt has no keyword separator")
	}
	if string(ikw) != "image-forge" {
		t.Errorf("iTXt keyword = %q, want image-forge", ikw)
	}
	if len(rest) < 4 {
		t.Fatalf("iTXt too short after keyword: %v", rest)
	}
	// compFlag, compMethod, empty langtag terminator, empty transkw terminator.
	if !bytes.Equal(rest[:4], []byte{0, 0, 0, 0}) {
		t.Errorf("iTXt header bytes = %v, want 0,0,0,0", rest[:4])
	}
	if string(rest[4:]) != jpText {
		t.Errorf("iTXt text = %q, want %q", rest[4:], jpText)
	}
}

func TestEncodeTextChunk_SelectsTEXtForLatin1(t *testing.T) {
	// A Latin-1 rune above ASCII (é = U+00E9) still fits tEXt.
	c, err := encodeTextChunk(PNGText{Keyword: "k", Text: "café"})
	if err != nil {
		t.Fatal(err)
	}
	if typ := string(c[4:8]); typ != "tEXt" {
		t.Errorf("latin-1 text should be tEXt, got %q", typ)
	}
	// A rune above 0xFF (猫) forces iTXt.
	c2, err := encodeTextChunk(PNGText{Keyword: "k", Text: "猫"})
	if err != nil {
		t.Fatal(err)
	}
	if typ := string(c2[4:8]); typ != "iTXt" {
		t.Errorf("unicode text should be iTXt, got %q", typ)
	}
}

func TestEncodeTextChunk_RejectsBadKeyword(t *testing.T) {
	if _, err := encodeTextChunk(PNGText{Keyword: "", Text: "x"}); err == nil {
		t.Error("empty keyword should error")
	}
	long := make([]byte, 80)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := encodeTextChunk(PNGText{Keyword: string(long), Text: "x"}); err == nil {
		t.Error("80-byte keyword should error")
	}
}
